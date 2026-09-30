# Design

## Context

`media.ErrorAggregator`（commit `4305604 feat(observability)`）已存在并具备 `Record/Sweep/Close` API，聚合窗口默认 60s，summary 行带 `component=internal/adapter/media` 标签（参见 `internal/adapter/media/error_aggregator.go`）。`PSPacketizer` 与 `RTPizer` 的构造点在 `cmd/gb28181-simulator/main.go:353-354` 工厂闭包；现有调用点还包括 `cmd/genps/main.go`（独立 CLI）。

`PSPacketizer.Packetize` 与 `RTPizer.Packetize` 均为高频热循环，错误路径仅 `return ... , err`，无任何聚合。本 change 在错误路径旁加一条 `Record` 调用，对正常路径零开销。

## Goals / Non-Goals

**Goals:**
- 在不改返回语义的前提下，让 PS / RTP 打包错误进入聚合通道
- 主二进制进程级共享一个 aggregator 单例，便于 Sweep / Flush 集中管理
- CLI 工具不强制改造（向后兼容 nil 参数）
- 不破坏既有测试（`ps_packetizer` / `rtpizer` 单元测试零修改）

**Non-Goals:**
- 不在本 change 内接入更细粒度的 signature（MP4 demux / HLS source 等保留为后续 candidate）
- 不调整聚合窗口默认值
- 不暴露 aggregator 状态到 HTTP API
- 不改 ErrorAggregator 本身（其能力已满足需求）

## Decisions

### Decision 1: aggregator 作为可选参数注入构造器

`NewPSPacketizer(agg *ErrorAggregator) *PSPacketizer` 与 `NewRTPizer(ssrc uint32, mtu int, agg *ErrorAggregator) *RTPizer`。

- **Rationale**: nil-safe（`Packetize` 内 `if agg != nil { agg.Record(...) }`），CLI 工具直接传 nil，零行为变化。构造期绑定避免每次 Packetize 调用重复查表/原子读。
- **Alternative considered**: 包级单例（package-level `var defaultAggregator`）。否决：测试隔离困难；多实例并存（如多 CLI 子进程或未来 sidecar 嵌入）时无法灵活配置。

### Decision 2: 主二进制聚合实例由 main 持有并以闭包注入

`cmd/gb28181-simulator/main.go` 创建一个 `media.NewErrorAggregatorWithWindow(60s)`，并在构造 `PSPacketizerFactory` / `RTPizerFactory` 闭包时通过变量捕获传入。

- **Rationale**: 与现有"依赖注入工厂"模式一致（`MediaSourceFactory` / `PSPacketizerFactory` / `RTPizerFactory` 均为闭包）；main 已经持有 `ctx` 与 `*slog.Logger`，新建变量不引入新生命周期。
- **Alternative considered**: 由 `MediaService` 持有并下传。否决：MediaService 已经通过工厂模式拿 packetizer，多一层间接无收益。

### Decision 3: Sweep 由 MediaService 启动的 ticker 驱动

`MediaService` 已经为每条活跃会话启动 goroutine。增加一条**进程级** ticker（`time.NewTicker(60s)`），由 main 在初始化 MediaService 后启动；`defer agg.Flush()` 在 main 退出时执行。

- **Rationale**: MediaService 不知道"何时全部会话结束"——聚合摘要的窗口边界与生命周期应与进程一致而非会话一致。main 持有 ctx，可以随 ctx cancel 退出 ticker。
- **Alternative considered**: 在每个 Packetize 调用内判断是否需要 Sweep。否决：把窗口推进与单包处理耦合；无法保证空闲时不输出过期摘要。

### Decision 4: signature 字符串选择

`ps-mux` 与 `rtp-send` 两个固定字符串。

- **Rationale**: 与 ErrorAggregator 的 docstring 推荐用例一致（`"rtp-send" or "ps-mux"`）；运维可通过 `log.modules: { "internal/adapter/media": "debug" }` 一次性覆盖所有聚合摘要。
- **Alternative considered**: 动态 signature（如 `fmt.Sprintf("ps-mux:%d", frame.Kind)`）。否决：错误粒度不足——同一类失败应聚到一起，过细的 signature 等于不聚合。

## Risks / Trade-offs

- **风险**: hot loop 引入一次额外的 nil 比较与可能的原子加锁（`Record` 内部拿 `mu`）。缓解：`Record` 的 mu 仅在第一次 Record(signature) 时写、后续读多写少；预算热循环额外开销 < 50ns。
- **风险**: 进程退出未 `Flush()` 会丢失最后一窗口的摘要。缓解：main 必须 `defer agg.Flush()`；任务清单显式校验此 defer 存在。
- **Trade-off**: 不在本 change 内接入 demuxer / HLS source 的错误聚合——属于 scope 控制；后续可单独 follow-up。
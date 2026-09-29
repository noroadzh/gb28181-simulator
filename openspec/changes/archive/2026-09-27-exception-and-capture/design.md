# 设计

## 背景

见 `proposal.md - 背景`。Change 2 构建了 wire audit 流（`siptransport` 在每次发送/接收时调用 `audit.Global().Emit`）。Change 13 引入该流的第一个真实消费者，并在 Change 6+ 已实现的 acceptor/keeper 行为之上叠加一层可选故障注入。模拟器已经能回答所有 SIP 方法；本变更让它可以“答错、答慢、不答”，同时让运维能抓取对端随后的行为。

**来自项目上下文的关键约束：**
- 纯 Go，无 CGO。
- `internal/adapter` 可以引用兄弟 adapter；`app` 只能引用 `domain` 与 `port`。
- 默认关闭的增量行为：未显式启用的 fixture 和测试必须看到字节级一致的输出。

## 目标 / 非目标

**目标**
- 按节点的可选故障配置，在 acceptor/keeper 边界生效。
- 按节点的可选 SIP 抓包环形缓冲，支持查询、实时扇出和 pcap 下载端点。
- 两个特性均关闭时零开销。

**非目标**
- 媒体面抓包（RTP/PS 转 pcap）— 仅信令面。
- 抓包脱敏（完整原始 payload 本就是目的）。
- Web 抓包 UI（hexdump 面板）— Change 14。
- YAML 驱动的场景脚本 — Change 15；它将驱动这里构建的 REST 端点。

## 决策

### D1. `audit.WireEvent` 新增 `NodeID`

`audit.WireEvent` 新增单个增量字段 `NodeID string`（默认 `""`）。当传输层以 `siptransport.WithNodeID(...)` 构造时，`Send`/`Receive` 都会附带该字段；否则保持为空。既保留既有 `Emitter` / `NopEmitter` 语义，又为 capture 消费者提供所需的 tag。

- **理由** — `port.AuditSink` 已经适配 wire events，但 domain `model.WireEvent` 只带 256 字节预览。capture 需要完整原始字节与所属节点 ID；直接在 adapter 事件上加 `NodeID` 是侵入最小的路径，且不改变任何 port 接口。
- **备选** — 在 UDP 源端口或线程本地变量里传节点 ID，或要求 transport 直接安装 `port.CaptureSink` — 均被否决。三者要么破坏“transport 不感知节点”的既有契约，要么要求 per-transport 单例。

### D2. Transport factory 签名增加 `NodeID`

`nodereg.TransportFactory` 变为 `func(addr string, nodeID model.NodeID) (port.SIPTransport, error)`。`main.go` 的 `bindTransport` 更新为调用 `siptransport.New(addr, siptransport.WithNodeID(nodeID.String()))`。直接构造 transport 而不传节点身份的既有 adapter 继续可用（默认 `nodeID == ""`）。

- **理由** — factory 由 `Lifecycle.Start(id)` 与 `Lifecycle.Transport(id)` 调用，因此调用点总能拿到节点 ID。经由 factory 传递可保持 transport 对 registry 无状态，并让 lifecycle 测试容易更新。
- **备选** — 在 registry 里维护 `nodeID -> transport` 反向映射 — 否决。会重复所有权且需要额外锁，而 transport 地址本就由 registry 管理。

### D3. 故障存储位于 app 层，通过 `port.FaultStore` 暴露

```go
type FaultStore interface {
    Install(ctx context.Context, nodeID model.NodeID, profile FaultProfile) error
    Clear(ctx context.Context, nodeID model.NodeID) error
    Get(ctx context.Context, nodeID model.NodeID) (FaultProfile, bool)
}
```

`model.FaultProfile` 包含 `Canned map[string]int`、`Delay struct{Base, Jitter time.Duration}`、`Drop float64`、`Blackhole []string`、`UnsupportedMethod int`。存储为以 `NodeID` 为 key 的 `map + sync.RWMutex`。acceptor 在每个入站请求上读取；keeper 只隐式读取（blackhole MESSAGE）。

- **理由** — 故障是动态运行时状态，而非持久化配置。放在 app 层可让 HTTP API 直接操作而无需触碰 YAML loader，也保持 domain 不感知传输细节。
- **备选** — 把 fault 塞进 `model.NodeProfile` — 否决。需要改动 `platformconfig`，且会让临时注入看起来像永久配置。

### D4. capture 存储通过 `port.CaptureStore` 暴露

```go
type CaptureEvent struct {
    NodeID    string
    Direction string
    Local     string
    Remote    string
    Transport string
    Bytes     []byte
    At        time.Time
}

type CaptureStore interface {
    Append(nodeID string, evt CaptureEvent)
    Query(nodeID string, limit int) []CaptureEvent
    Subscribe(nodeID string) (<-chan CaptureEvent, func())
    PCAP(nodeID string) ([]byte, error)
}
```

由 `internal/adapter/capture.Ring` 实现。每个节点一个 ring；总容量可配置，默认 2048 条。`Append` 尽力而为：满了会覆盖最旧槽位。`Subscribe` 返回扇出通道与 cancel 函数；订阅通道满时事件会被丢弃而不是阻塞 capture。

- **理由** — `audit.Global().Emit` 已经是 fire-and-forget。capture adapter 作为全局 emitter 注入可保持 transport 层不变。按节点分 ring 意味着每个节点有独立的有界历史；全局 emitter 是唯一写点，不会出现锁爆炸。
- **备选** — 使用一个按 node ID 键控的进程级共享 ring — 否决。单结构会变成全局锁瓶颈。

### D5. 通过纯 Go `pcapgo` 导出 pcap

`Ring.PCAP` 打开 `pcapgo.Writer`，把每个缓冲事件写成合成的 Ethernet/IPv4/UDP 帧：SIP payload 作为 UDP 数据，本地与远端端点作为 IP/端口，时间戳保留。链路类型使用 `LINKTYPE_ETHERNET`（1），以便 Wireshark/tcpdump 开箱即识别 SIP。文件头 snaplen 为 65536 字节。

- **理由** — `gopacket` 的 `pcapgo` 写入器是纯 Go，无需 libpcap/CGO。合成方式避免真实绑定网卡，因此模拟器无需提权。Ethernet 头清零，因为对端地址已经在 IP 层。
- **备选** — 写不含 Ethernet 的裸 pcap — 否决。需要 `LINKTYPE_RAW`/`LINKTYPE_NULL`，一些老工具解析有误。

### D6. acceptor/keeper 故障编排在 handler 之前统一设卡

单个 `a.fault.Apply(req.Method())` 调用在正常 `handleXxx` 链前判定结果。`Apply` 返回三态：
- `nil` — 正常继续（同时在此处通过 `time.Sleep` 应用延迟）。
- `*model.Response` — 预制回复，跳过 handler。
- `skip` — 静默丢弃 / 黑洞（同时计数）。

该 gate 位于 `acceptor.go` 的主循环，以及 `keeper.go` 内的 keepalive 响应路径（两者共用同一个 `port.FaultStore`）。

- **理由** — 单一 gate 可确保未安装故障配置时每个 handler 与变更前字节级一致，并保证 `UnsupportedMethod` 被统一处理。
- **备选** — 在每个 `handleXxx` 内注入故障 — 否决。会重复 blackhole/drop/canned 逻辑，且容易遗漏。

### D7. HTTP API 扩展现有 `NodeView`

通过给 `NodeView` 增加 `InstallFault`、`ClearFault`、`FaultDetail`、`CaptureQuery`、`CapturePCAP` 来新增 `POST /v1/nodes/:id/faults`、`DELETE /v1/nodes/:id/faults`、`GET /v1/nodes/:id/capture`、`GET /v1/nodes/:id/capture.pcap`。既有的动作端点（`trigger-alarm`、`position`）已经在此；新增五个方法保持 `interface` 包作为唯一路由 handler 面。

- **理由** — `NodeView` 已经是 HTTP 层依赖的窄 app 契约。小幅扩展比引入两个新接口、新的构造参数和测试 fake 变体更快。
- **备选** — 引入 `FaultView`/`CaptureView` 并改 Server 构造器 — 否决。唯一消费者是已经持有 `NodeView` 的 HTTP handler；为了四条新路由引入两个新接口成本过高。

### D8. 故障计数与异常事件日志

每次注入的故障都会递增节点（或 fault store 本身）上的 `faultCounters` map，并打印 `level=info, node_id=..., peer=..., method=..., fault=canned_response|delay|drop|blackhole|unsupported_method`。计数通过 `GET /v1/nodes/:id` 与正常节点状态一起返回。

- **理由** — 运维需要不翻 pcap 就能得到的测试报告；log + counters 提供“坏了几次”的数字，供 Change 15 场景引擎断言。
- **备选** — 只通过 pcap 输出计数 — 否决。需要扫描整个文件；模拟器能直接给出确定性数字。

## 迁移计划

1. 在 `go.mod` 加入 `google/gopacket` 并 vendor。验证 `CGO_ENABLED=0 go build ./...` 退出码 0。
2. 给 `audit.WireEvent` 加 `NodeID`，新增 `siptransport.WithNodeID`。
3. 更新 `nodereg.TransportFactory` 与 `main.go` 的 bindTransport，把节点 ID 传递下去。
4. 新增 `model.FaultProfile`、`port.FaultStore`、`port.CaptureStore`、`port.CaptureEvent`。
5. 实现 `internal/adapter/capture.Ring`，并在 `main.go` 接为全局 audit emitter。
6. 在 `internal/app/faults.go` 实现 fault store，并把 gate 织入 `Acceptor.handleRequest` 与 `Keeper.keepalive` 响应路径。
7. 扩展 `NodeView` 5 个方法，在 `interface/http` 注册 4 条新路由。
8. 在 adapter、app、interface 层补单测；运行 `CGO_ENABLED=0 go test -race -count=1 ./...`。

## 风险 / 权衡

| 风险 | 缓解 |
|------|------|
| pcap 合成出的帧某些解码器不识别 | 使用规范 Ethernet/IPv4/UDP；设计评审时已用 `tcpdump -n -r` 和 Wireshark 校验 |
| 容量过大时 capture 内存无限增长 | 默认每节点 2048 条；后续变更可通过 YAML 配置 |
| 故障注入影响不期望丢包的既有测试 | 故障配置默认关闭；既有 fixture 从不安装，行为不变 |
| `NodeView` 接口过大 | 可接受：仅新增 5 个方法且都与路由相关；下一变更(14)可能加 UI 读方法但不动协议语义 |
| 长时间抓包导致 pcap 文件很大 | 消费者可删文件；场景引擎(#15)会限制抓包时长 |

## 待决问题

无。

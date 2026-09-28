# 设计

## 背景

见 `proposal.md - 背景`。Change 8 交付了四种媒体源统一抽象，其中本地文件源（`file_source.go`）直接打开文件返回 `io.ReadCloser`，由 `StreamESReader` 以 `00 00 00 01` 起始码切割 NAL；实际 gb28181 测试场景（RecordInfo 返回路径 `/records/.../20260925T000000.mp4`）已暗示 mp4 是预期文件格式，但当前实现不支持容器。Change 10 交付了报警/移动位置的运行时触发与 NOTIFY 解析，但平台侧作为订阅方发起 SUBSCRIBE 时，非 catalog 事件一律 489，使平台主动订阅报警/移动位置变为不可能；这与 GB/T 28181-2016 §7.3 的 SUBSCRIBE/NOTIFY 机制不符。Change 11 引入 2022 扩展协商与 `INFO` 载体，但 acceptor 方法分发缺少 `INFO` 分支，且 default 分支沿用 Change 13 的"静默丢弃"行为——与 RFC 3261 冲突。

**来自项目上下文的关键约束：**
- 纯 Go，无 CGO。
- `app` 只依赖 `domain` 与 `port`；`internal/adapter` 之间可相互引用。
- 默认关闭的增量行为：未显式启用时字节级行为不变（此处表现为"配置文件写 `file` 时 mp4 容器自动透明处理，不改变接口"）。
- 涉及 SIP/SDP/PS 字节格式的任务必须包含 golden test（字节级断言）。

## 目标 / 非目标

**目标**
- 本地文件源通过魔数嗅探自动支持 mp4 容器，对上层零感知：`MediaConfig{Kind: "file", Path: "a.mp4"}` 不新增 kind，工厂不变。
- mp4 demux 产出 Annex-B NALU 帧，容器 PTS 经 90 kHz 换算后挂到每帧，消费者（PS Packetizer）无需关心容器格式。
- 入站 SUBSCRIBE 接受 `alarm` / `mobileposition` / `catalog` 三事件，事件类型携带在订阅者结构里；NOTIFY 按事件类型分发（`sendNotify(event, body)`）。
- 未知方法默认回 `405 + Allow`；`INFO` 被区分处理，MANSRTSP 请求路由到新 adapter。
- `PlaybackPort` 扩展 Scale/Seek；2016 风格 `PlaybackControl` MESSAGE 被接通到 port（修复现状缺口）。

**非目标**
- mkv / ts / flv 容器。
- 出站 SUBSCRIBE（设备向上级订阅）。
- MANSRTSP 的 `TEARDOWN`、绝对时间 `clock=` Range、多能力协商。
- 报警/位置订阅的 dialog 内 re-SUBSCRIBE 刷新状态机（沿用现有 per-Call-ID 定时器模型）。
- mp4 的加密轨道、B 帧乱序重排（PTS/DTS 分离输出）、多视频轨选择。

## 决策

### D1. mp4 依赖：`github.com/abema/go-mp4`

新增 `github.com/abema/go-mp4`（纯 Go，无 CGO，活跃维护）。它负责 box 的流式读取：`ftyp` 魔数嗅探 → `moov` → `trak` → `stbl` → `stts/stsc/stsz/stco/stsd`，最终逐 sample 产出原始字节与 PTS。不引入 CGO，与现有 `modernc.org/sqlite`、`gmsm` 同类。

- **理由**：go-mp4 的 box 模型足够支撑我们需要的 track/sample 级元数据提取；原生 Go；API 友好（通过 `mp4.NewFile` 流式解析）。
- **备选**：手写 box 解析器 — 否决；测试成本高，仅 mp4 无复用价值。

### D2. 魔数嗅探 + 透明接入，不新增 SourceKind

`FileSource.Open` 对打开的文件读取前 12 字节：offset 4 处若等于 ASCII `ftyp`，则视为 mp4 容器，后续交给 `MP4Demuxer`；否则保持 `io.ReadCloser` 返回裸文件流（即变化前行为）。`MediaConfig` 不新增 `SourceKindMP4`。

- **理由**：用户确认"只做 mp4，其余后续再说"，且 RecordInfo 测试中 `.mp4` 路径已暗示文件容器格式。同一个 `file` kind 自动适配，配置和工厂都不需要改动，改动面最小。
- **备选**：新增 `SourceKindMP4` + 工厂分支 — 否决；用户路径系统已用 `.mp4` 扩展名，配置引入新 kind 反而造成两套语义重复。

### D3. MP4Demuxer 输出 Annex-B，利用 StreamESReader 的 Annex-B 切割

`MP4Demuxer`（返回的 `io.ReadCloser`）的 `Read` 方法从容器逐 sample 产出 H.264/H.265 Annex-B NALU（AVCC 4 字节长度前缀 → `00 00 00 01` start code）或 AAC ADTS 帧；音频与视频 sample 严格按容器时间戳顺序交织输出，通过 `model.ESFrameWithPTS` 携带 90 kHz PTS。

关键：`MediaService.OpenSource` 在 wrap `StreamESReader` 前对 `src.Open` 返回的 reader 做类型断言：

```go
type ESFrameReader interface {
    Read(ctx context.Context) (model.ESFrame, error)
}
```

如果 reader 实现了该接口（即 mp4 demuxer 有逐帧 PTS 且直接产出 ESFrame），`OpenSource` 直接将其作为 `ESReader` 返回，不套 `StreamESReader`；否则走既有 FPS 合成 PTS 路径。`port` 包定义 `ESFrameReader`，`app` 层仅做类型断言——不感知 mp4 细节。

- **理由**：容器真实 PTS 比 FPS 合成 PTS 更准确（尤其关键帧对齐），且 Annex-B 格式与现有 RTP/PS 流水线直接兼容，无需新增 adapter。
- **备选**：demuxer 输出裸 Annex-B 由 StreamESReader 切割 — 否决；会丢失容器时间戳，PTS 需要回插到已切分的帧上，复杂度高且容易错位。

### D4. SUBSCRIBE 事件扩展：event 字段下沉到 subscriber 结构

将 `platform.subscribers[callID]` 的类型从 `catalogSub` 泛化为 `sub`（含 `event` 字段）。`SubscribePort.Subscribe` 签名扩展为 `Subscribe(ctx context.Context, deviceID, channelID, event string)`；stub adapter 同步更新。入站 SUBSCRIBE 白名单：`{catalog, alarm, mobileposition}`；非白名单事件回 489。

NOTIFY 分发统一为 `sendNotify(ctx, p, callID, peer, fromValue, event, body)`；`notifyCatalogChange` / 报警触发 / 位置变更调用点分别传入对应 event。alarm 初始 NOTIFY 为空体；mobileposition 初始 NOTIFY 携带节点 Profile 中 `Position` 字段（如果已配置）。

- **理由**：同一设备对同一上级可能同时订阅目录与报警；事件类型作为订阅的语义维度，下沉到 subscriber 比单独建 `alarmSub`/`mobilePositionSub` 结构体更干净。
- **备选**：用单独的 map[event]map[callID] — 否决；订阅生命周期（Expires 超时）已在 `recordSubscriber` 中按 Call-ID 集中管理，拆分结构会重复定时器逻辑。

### D5. 默认 405：修改 exception-injection 的"不支持方法"需求

Change 13 归档的 `exception-injection` spec 在 `openspec/specs/exception-injection/spec.md` 中规定"默认静默丢弃 + debug 日志"。本次将其修改为默认回 `405 Method Not Allowed` + `Allow` 头，而 `UnsupportedMethod` 的 fault 配置仍然覆盖默认行为（按 fault 配置回 501）。

具体 `Allow` 内容由 acceptor 中实际 handler 决定：维护一个 `allowedMethods` slice（含 `REGISTER, MESSAGE, INVITE, ACK, BYE, OPTIONS, SUBSCRIBE, INFO`），`default` 分支和所有 200 OK 响应统一使用该 slice 生成头。INVITE 的 200 OK 已有 Allow 头，需同步扩展。

- **理由**：RFC 3261 §8.2.2 明确 405 对未知方法是规范要求；保持 faultUnsupported 钩子优先意味着 Change 13 的功能集不变（fault 用户仍可见 501/自定义状态码），仅默认行为标准化。

### D6. INFO 分发：Content-Type 路由 + MANSRTSP 解析器

新增 `internal/adapter/mansrtsp` 包，负责解析 INFO body 为 `PlayCommand` 结构体（`Command`/`Scale`/`Seek`/`Transport` 等）。acceptor 的 `case "INFO"`：

- `Content-Type: Application/MANSRTSP` 或 body 以 `PLAY`/`PAUSE` 开头 → 路由到 mansrtsp adapter → 映射为 `PlaybackPort` 调用（`Play`/`Stop` + `Scale`/`Seek`）。
- `Content-Type: Application/MANSCDP+XML` 且 `CmdType=MediaStatus` → 复用 `handleMediaStatus`。
- 其余 → 200 OK（宽容），debug 日志记录未知 body 形状。

同时修复现状：`handlePlaybackControl`（MESSAGE 路径）在 playback port 存在时，将 `PlaybackControl` 命令 `Play/Stop/Pause` 路由到 `PlaybackPort`（接口需加 `Stop` 以外的 playback control 语义；当前 `Play`/`Stop`/`Query` 已够覆盖，`Scale`/`Seek` 为 MANSRTSP 专用）。

- **理由**：2022 标准将 MediaStatus 通知从 MESSAGE 迁移到 INFO（MANSRTSP body），而回放控制（播放倍速/seek）仅通过 INFO 承载。统一在 INFO 方法中路由可消除对端的歧义。

### D7. `PlaybackPort` 扩展：Scale / Seek 语义

```go
type PlaybackPort interface {
    Play(ctx context.Context, deviceID, channelID, startTime, endTime string, scale float64) (string, error)
    Stop(ctx context.Context, sessionID string) error
    Query(ctx context.Context, sessionID string) (PlaybackState, error)
}

type PlaybackState struct {
    SessionID string
    Playing   bool
    Scale     float64
    Position  time.Time
}
```

`Play` 新增 `scale` 参数（1.0 为正常倍速，负数表示倒放，0 为暂停）；stub/adapter 对非法 scale 回 `ErrPlaybackUnsupported`。`PlaybackState` 新增 `Scale` 字段。

- **理由**：Scale 是 2022 回放控制的核心能力（8 倍速快进/0.5 倍速慢放/倒放），与 MANSRTSP `Scale` 头直接对应。保持现有 `PlaybackState` 向后兼容，新增字段零开销。

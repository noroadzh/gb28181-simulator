# Design

## Context

#41 建立了平台侧实时会话：每个 (节点, 通道) 一条 PS 帧总线（`internal/app/live_session.go`），FLV 网关通过 `LiveSourceProvider` 消费它。本 change 在这条总线上再接一个消费者（录制器），并让平台在 RecordInfo / 回放 INVITE 上从录制结果作答。

现有可复用部分：

- 实时会话总线已经做了慢消费者丢帧，录制器可以直接订阅；
- `mediaSink`（#40）已经覆盖 UDP 与附录 D TCP 的 RTP 发送；
- 设备侧回放（`answerDevicePlaybackInvite`）给出了 200 + `a=sendonly` 的应答形状；
- FLV 管线 `runPipeline` 只依赖 `<-chan model.PSFrame`，录像文件读出来就是 PS 帧。

## Goals / Non-Goals

**Goals:**

- 拉流即录制，分段落盘，重启后仍能查询。
- 查询与回放同时可供 HTTP 与上级平台（SIP）使用。
- 录制失败不影响拉流与 FLV。

**Non-Goals:**

- 不录制入站 INVITE 的媒体（上级推给本平台的流仍丢弃）。
- 不做保留策略、容量上限与删除接口。
- 不做 SIP 侧倍速与暂停控制（回放按原速窗口播放，控制面 MANSRTSP 的增强留待后续）。
- 不改设备的录像行为。

## Decisions

### Decision 1: 录制是实时会话总线上的一个消费者

`StartLive` 成功登记会话后再启动一个录制 goroutine，订阅该会话的 PS 帧；会话 `done` 关闭即封段。这样录制与 FLV 完全解耦：没有观众也在录，观众掉线也不影响录制。

*备选*：在媒体接收循环里直接写盘。否决：把 IO 塞进收流热路径，慢磁盘会反压 RTP 接收。

### Decision 2: 文件系统 + 文件名元数据，不引入数据库

分段文件名为 `<nodeID>_<channelID>_<YYYYMMDDTHHMMSS>.ps`，目录来自配置（默认 `<storage 目录>/recordings`）。启动时扫描目录重建索引；进程崩溃留下的半段以文件名时间为起点、文件 mtime 为终点参与查询。Append 打开当前段并按 `segment_seconds` 滚动切段。

*备选*：SQLite 表 + 迁移。否决：本 change 的查询只需要「节点 + 通道 + 时间」，而文件本身已经携带全部信息；引入迁移会让重启与回滚都变重。

### Decision 3: 回放按窗口拼接分段，产出 PS 帧

`Stream(node, channel, start, end)` 选出与之有交集的分段，顺序打开，按 PS pack 起始码（`00 00 01 BA`）切出帧并发出。窗口结束或数据耗尽即关闭。多个消费者各自打开文件，互不影响。

*备选*：按录像 ID 逐段回放。否决：上级平台的回放请求是一个时间窗，不是一条记录；按窗口拼接才是它期望的语义。

### Decision 4: FLV 回放复用网关管线

`streaming` 增加 `RecordingSource` 端口（`OpenRecording(ctx, node, channel, start, end) (<-chan model.PSFrame, error)`），HTTP handler 取到帧流后走与实时源相同的 `runPipeline`。管线、编码识别与 MPEG-4 降级行为一次性复用。

### Decision 5: 平台与设备在 RecordInfo 上分开

平台节点从录像库应答；设备节点保持既有的固定条目。判据是节点身份，不是「有没有录像」——一个还没录到东西的平台回空表，而不是回一条不存在的录像。

### Decision 6: 回放 INVITE 不做实时节流

v1 按读取速度发送，RTP 时间戳按帧递增。真实平台会按媒体时钟节流；本模拟器的目标是让上级能收到完整、可解码的录像，而不是精确的墙钟节奏。这一点写进 Risks，并在日志里标注回放完成。

## Risks / Trade-offs

- **磁盘无上限** → 明确 Non-goal；运维通过配置目录自行清理。日志记录每个分段的路径与大小。
- **突发发送导致对端缓冲压力** → v1 接受；若对端丢包，可后续加按 PTS 的节流。
- **崩溃留下半段** → 启动扫描用文件名时间 + mtime 兜底；半段仍可回放（其内容就是已写入的帧）。
- **同名分段冲突**（同一秒内切段） → 文件名加序号后缀；扫描时按全名去重。
- **录制器跟不上总线** → 总线本身对慢消费者丢帧；丢帧只会让录像有缺口，不会拖慢拉流。

## Migration Plan

纯增量：新目录、新端点、平台节点的两条 SIP 分支。关闭 `recording.enabled` 即回到 #41 行为；已有录制文件不会被删除。

## Open Questions

无。

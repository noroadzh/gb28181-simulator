# Design

## Context

`internal/app/acceptor_messages.go` 的 `handleMessage` 已经按 CmdType 分派了九类命令；`internal/adapter/manscdp` 有对应的编解码与 golden 测试；设备侧与平台侧共用同一个 `Acceptor`，靠 `p.isDevice` 区分。广播与下载是两条**媒体**路径，其余四类只是信令问答。

已可复用的东西：
- 对讲音频桥（`talk_call.go` + `/talk/audio`）已经能把一条 PCMU 对话接进 WebSocket；
- 文件下载的媒体形状与历史回放一致（PS/RTP + 时间窗），`answerDevicePlaybackInvite` 就是它的设备侧原型；
- 录像库（#42）能按窗口取 PS 帧，平台侧下载可以把收到的流写进同一个目录。

## Goals / Non-Goals

**Goals:**

- 六类命令双向可用，形态符合国标且带 golden 断言。
- 广播音频真正可听（接到既有对讲桥）。
- 下载在设备侧真发流、平台侧真落盘。

**Non-Goals:**

- 真实升级/重启；完整配置集；断点续传与限速；SRTP/TLS。

## Decisions

### Decision 1: 值对象放 model，编解码放 adapter，处理器放 app

沿用既有分层：六个命令各有请求/应答值对象（`model`），`MANSCDPCodec` 增补编解码方法（`adapter/manscdp`），`handleMessage` 增加分支（`app`）。golden 测试固定在 adapter 层，避免每次改处理器都要动字节断言。

### Decision 2: DeviceStatus 与「平台转发」共用一条路径

设备应答自己的状态；平台若被问到自身则同样自答，若问到下级则走既有 `controlTarget` 找到设备、发 MESSAGE、等应答（与录像查询同一套事务）。区别只在于应答体的渲染，因此抽一个 `answerDeviceStatus(target)`。

### Decision 3: 广播 = 先 ack，再由设备主动 INVITE

国标里广播的媒体方向是「平台听、设备说」，与对讲相反。设备收到 `Broadcast` 先回 ack，然后**主动**向发起方发音频 INVITE（`s=Talk` 的既有 offer 形状可复用）；平台侧收到这条 INVITE 走既有 `answerDeviceTalkInvite` 的分支逻辑，于是音频自动进入 `/talk/audio` 通道。这样广播不需要新的媒体代码。

### Decision 4: 布防状态放在节点 profile 的运行时字段

布防/撤防是节点状态而不是配置，放在 profile 的内存字段（`alarmArmed`），随节点存在；不落库（重启后回到默认「撤防」），因为模拟器的布防主要用于演示报警链路。

### Decision 5: 配置下发只让可变项生效

`ConfigDownload` 的应答携带名称/厂商/通道数/心跳间隔；下发时只接受心跳间隔（其余返回 `Result=ERROR`），因为它已经由 Keeper 消费，改它有意义。这样「下发成功」是可验证的，而不是只回一个 OK。

### Decision 6: 下载复用回放的两条腿

设备侧：`s=Download` 与 `s=Playback` 共享「按窗口取源 → PS/RTP」的实现，差别只是 SDP 的 `s=` 与是否需要文件语义。平台侧：作为请求方发起 `s=Download`，把收到的 PS 帧写入录像库目录下的 `downloads/`（复用 `recording.Store` 的写路径，节点/通道键加 `download:` 前缀以区分）。

## Risks / Trade-offs

- **广播依赖设备主动 INVITE** → 平台必须能应答来自设备的音频 INVITE；这条路径已存在（对讲），但广播会新增调用点，需覆盖测试。
- **下载落盘无上限** → 与录像同一 Non-goal；运维清理。
- **布防不持久** → 重启后回到撤防，文档写明。
- **六类命令一次做完，回归面大** → 每类都有独立 golden + app 单测，且既有命令的分派顺序不变。

## Migration Plan

纯增量。回滚即移除分支与端点；已下载的文件留在 `downloads/` 不影响启动。

## Open Questions

无。

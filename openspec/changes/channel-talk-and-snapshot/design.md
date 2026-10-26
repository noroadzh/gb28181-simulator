# Design

## Context

见 `proposal.md`。对讲与抓拍共用一条底层能力：拿到设备的媒体面（INVITE 对话 / RTP 帧）。现有 `MediaService` 已能接收入站 RTP 并交给 PS 解封装；FLV 网关能消费 RTP。

## Decisions

### Decision 1: 对讲走标准 INVITE 对话，音频经现有 WSS 通道转发

`TalkStart` 构造 INVITE（SDP：PCMU/8000，sendrecv），成功后把 RTP 音频桥到浏览器的 WebSocket；`TalkStop` 发 BYE。不引入新的媒体服务器——点对点音频对模拟器足够。

### Decision 2: 快照从最近关键帧解码，不触发新拉流

`Snapshot` 读通道媒体管道缓存的最新关键帧（H.264/H.265 IDR），解码为 YUV 再编码 JPEG。缓存不存在时按「无媒体源」503 处理，而不是为一次抓拍新建拉流。

备选：用 INVITE 拉起一次抓拍——会给设备增加一次会话，且对模拟器无收益。

### Decision 3: 会话与帧缓存都以通道为键

`map[nodeID|channelID]talkSession` 与媒体管道的帧缓存同键，节点停止时一并回收；避免跨节点串话。

### Decision 4: 对讲以 `port.TalkPort` 为准，重写现有 `app.TalkAcceptor`

现状有两套互相矛盾的东西：`port.TalkPort.StartInvite` 的契约是「平台向设备发 INVITE」，而 `internal/app/talk.go` 的 `TalkAcceptor` 实现的是相反方向（等设备发 INVITE 到平台的 talk 端口），并且从未接线（`node_service.go` 里留着 TODO）。本 change 以端口契约为准：实现一个满足 `TalkPort` 的适配器（StartInvite 发 INVITE、Stop 发 BYE），`app.TalkAcceptor` 的旧模型随之删除或改写，避免两套语义并存。

**未决（实现前需确认）**：旧 `TalkAcceptor` 的 WSS 音频帧格式与会话键（`node|channel`）可以复用，但它按「设备主动 INVITE」写的会话建立逻辑不能保留。

### Decision 5: 快照的关键帧缓存放在 MediaService 的入站管道出口

`MediaService` 目前没有帧缓存，`Snapshot` 无处取图。方案：在入站管道（RTP→PS→ES）出口按 `node|channel` 保留最新一个 **IDR 访问单元**（含 SPS/PPS），`Snapshot` 读取后解码为 YUV 再编码 JPEG。缓存只保留一个 AU，内存有界；没有关键帧时按「尚无媒体」处理（504），而不是触发新拉流。

**未决**：解码 H.264/H.265 需要引入纯 Go 解码器（当前仓库只有编码/封装侧）；若不想新增依赖，快照可先只支持 MPEG-4 Part 2 / MP4 直读的关键帧（`mp4-direct-playback` 已有 demuxer），其余编码返回 501。

## Risks / Trade-offs

- **R1 关键帧可能尚未产生** → 超时返回 504，并在文档写明「先产生媒体再抓拍」。
- **R2 浏览器音频兼容性** → 先只支持 PCMU→WebAudio，其他编码返回 501。
- **R3 对讲并发** → 单通道单会话，重复 start 返回既有 session id（幂等）。

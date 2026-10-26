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

### Decision 5: 快照只覆盖「无需解码」的源

项目约束是纯 Go、无 CGO、单二进制分发，而把 H.264/H.265 关键帧变成 JPEG 需要视频解码器——当前仓库没有，引一个纯 Go 解码器也不现实。因此快照只对**能直接产出像素**的源生成 JPEG：合成测试图（`SyntheticSource.renderFrame` 已返回 `image.Image`，直接 `image/jpeg` 编码）。需要解码的源返回 501 并说明原因，而不是假装成功。

渲染能力经 `port.FrameRenderer` 注入（app 不得直接 import adapter），无媒体源返回 503。

**已排除**：运行时调用外部 ffmpeg（破坏纯 Go 单二进制约束）、引入解码器依赖。

### Decision 6: 音频桥延后，与设备 INVITE 门禁一起单独处理

控制面（INVITE/BYE/回收）本轮交付；音频桥需要解析 200 OK 的 SDP 应答、RTP 双向收发与新的 WebSocket 路由。更关键的是：设备节点的 acceptor 门禁只放行 MESSAGE，对 INVITE 回 405，所以无论音频桥是否实现，端到端都不会有声音。**设备身份是否接受 INVITE** 是独立的设计决定（会影响 device 的 UAS 契约），不适合塞进本 change。

因此 1.2 延后并登记为路线图 #38；本 change 的验收边界是「控制面完整、音频面未接」。

## Risks / Trade-offs

- **R1 关键帧可能尚未产生** → 超时返回 504，并在文档写明「先产生媒体再抓拍」。
- **R2 浏览器音频兼容性** → 先只支持 PCMU→WebAudio，其他编码返回 501。
- **R3 对讲并发** → 单通道单会话，重复 start 返回既有 session id（幂等）。

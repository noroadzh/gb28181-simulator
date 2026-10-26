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

## Risks / Trade-offs

- **R1 关键帧可能尚未产生** → 超时返回 504，并在文档写明「先产生媒体再抓拍」。
- **R2 浏览器音频兼容性** → 先只支持 PCMU→WebAudio，其他编码返回 501。
- **R3 对讲并发** → 单通道单会话，重复 start 返回既有 session id（幂等）。

# Design

## Context

平台节点目前只会接线：收到 INVITE 用 `handleInvite` 的通用分支回 200 并建一个 `nullESWriter` 的入站管道（`internal/app/acceptor_media.go`），媒体被丢弃；HTTP-FLV 的 `streaming.Gateway.Subscribe` 只接受 `device` 节点且必须配置本地 `MediaConfig`（`internal/app/streaming/gateway.go`）。

已有可复用的部分：

- `RTPDeizer.Write(pkt)` 直接产出 `model.PSFrame`，不需要走 ES 解包；
- `Gateway.runPipeline(ctx, key, psCh, out)` 已经是「PS 帧 → FLV tag」的完整管线，只需把 `psCh` 的来源换掉；
- `acceptor.Invite/InvitePlayback` 已经能在节点自身传输上发起对话，`controlTarget` 已经能把「平台 + 通道」解析成下级 deviceID 与地址；
- 设备侧的 TCP 媒体（附录 D）已实现，`rtp_transport.go` 的 sink 结构可以镜像出「接收端」版本。

## Goals / Non-Goals

**Goals:**

- 平台节点能对下级设备发起视频 INVITE，并在自己的端口上收流。
- 一个 (节点, 通道) 维度的实时会话，向上给 FLV，向下管生命周期。
- UDP 与 TCP（附录 D 被动监听）两种接收方式。
- 失败语义与既有控制面一致（404/501/502/504）。

**Non-Goals:**

- 不落盘、不做录像查询与回放（`platform-recording`）。
- 不做级联 INVITE 转发与媒体桥接（`cascade-media-relay`）。
- 不转码，不重新打包 PS；收到的 PS 帧原样交给下游消费者。
- 不改设备节点的出流路径与 FLV 本地源行为。

## Decisions

### Decision 1: 一个 `LiveSessionService`，会话是 PS 帧的广播源

新增 app 层服务：`map[sessionKey]*LiveSession`，`sessionKey{node, channel}`。会话持有接收端口、Call-ID、传输方式、最近收包时间、以及一个 PS 帧广播器（慢订阅者丢帧而不是阻塞收流）。

Gateway 通过一个端口（`LiveSourceProvider`：`PSFrames(ctx, node, channel) (<-chan PSFrame, cleanup, bool)`）读取实时源，而不是直接依赖 app 服务——保持 `streaming` 与 app 的解耦方向（app 依赖 streaming？当前是 app 构造 gateway，gateway 依赖 registry/media service；新增接口放在 `streaming` 包内由 app 注入实现，避免反向导入）。

*备选*：让 Gateway 直接持有 `*NodeService`。否决：会把 streaming 绑到 app 的整棵依赖树，测试也难替身。

### Decision 2: 平台侧 INVITE 复用现有事务，新增 `InviteVideo`

`talk_transactions.invite` 已经返回应答消息，只是两个调用方各取所需。新增 `Acceptor.InviteVideo(ctx, nodeID, target, addr, body) (callID string, answer model.Message, err error)`：发 INVITE、等最终响应、按状态码分类错误。SSRC 从应答 SDP 的 `y=` 读取（缺失时为 0，表示不过滤）。设备应答缺 `c=` 时，回落到信令对端地址。

*备选*：另写一套 INVITE 客户端。否决：重复事务与超时逻辑，且与 talk/playback 的行为容易分叉。

### Decision 3: 接收端口先绑定，再发 INVITE

UDP：`net.ListenPacket("udp4", "0.0.0.0:0")`，端口写进 offer 的 `m=video`。
TCP：`net.Listen("tcp", "0.0.0.0:0")`，offer 声明 `m=video <port> TCP/RTP/AVP 96` + `a=setup:passive`，设备应以 `a=setup:active` 连上来；每包 2 字节大端长度前缀。

*备选*：先发 INVITE 再开端口。否决：SDP 必须携带端口，设备拿到 offer 才开始发流。

### Decision 4: 会话生命周期是显式的，不跟随 FLV 订阅者

本地源的语义是「无订阅者即停流」，实时会话相反：它由 live start 显式创建，只由 stop / BYE / 超时 / 节点停止结束。FLV 订阅者来了又走不影响设备侧的对话。

超时规则：建立后 30 秒内无首包，或此后连续 30 秒无包，结束会话。重复 start 先结束旧会话。

*备选*：让 FLV 订阅者数归零即停。否决：会与「浏览器暂停再播放」产生抖动，也会让级联（#43）无法在没有本地观众时维持媒体。

### Decision 5: 实时源优先，`?source=` 显式覆盖

`GET /v1/flv` 的默认规则：有活动实时会话用会话，否则本地源。`source=local` 强制本地源，`source=live` 强制实时源且不存在时 404（不静默回落，避免「以为在看设备」）。

*备选*：只做实时源。否决：会破坏设备节点与已有 UI 的本地预览。

### Decision 6: 错误映射沿用控制面词汇

节点身份不支持拉流（device）→ 501；无可用下级 → 404；设备拒绝或应答不可解析 → 502；无最终响应 → 504；stop 幂等 → 204。

## Risks / Trade-offs

- **真实设备不按 offer 的端口/地址发流** → 按 SSRC 过滤；应答未给 SSRC 时接受任意源地址但记录首包来源，日志里能看到实际对端。
- **TCP 被动不被部分设备实现** → 默认 UDP；TCP 仅在显式请求时使用。
- **会话泄漏**（端口/goroutine） → 所有结束路径收敛到同一个 `close()`：stop、BYE、超时、节点停止；用 `-race` 测试覆盖每条路径。
- **慢 FLV 订阅者拖垮收流** → 广播器对每个订阅者用有界队列，满则丢帧并计数。
- **实时与本地画面混淆** → `?source=` 与日志都标明来源；平台节点的默认行为是「有会话才播实时」。

## Migration Plan

纯增量：新增服务、端点与一个可选查询参数。无数据库/schema 变更。回滚即移除路由与调用方；已建立的 SIP 对话随节点停止自然结束。

## Open Questions

无。级联中继是否需要「无观众也维持」已由 Decision 4 覆盖，属 #43 的实现细节。

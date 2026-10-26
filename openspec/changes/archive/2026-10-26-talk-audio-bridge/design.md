# Design

## Context

见 `proposal.md`。现状：`channel-talk-and-snapshot` 已交付 INVITE/BYE 事务与回收；`TalkStart` 返回的 `ws_url` 无实现；device 门禁把 INVITE 拦成 405；对讲会话已有本地 RTP socket（`talkCall.rtp`）。RTP 解包/打包在 `internal/adapter/media` 已有实现，可复用。

## Decisions

### Decision 1: device 门禁放行 INVITE，而不是另开端口

设备节点的拦截分支（`p.isDevice && msg.Method() != "MESSAGE"`）改为同时放行 INVITE，交给已有的 `handleInvite` 处理。设备因此仍然是「一个地址、一套 UAS 行为」，只是多了一种它本来就该回答的方法。备选：为对讲单开监听端口——会让设备在线上暴露两个身份，不符合 GB28181。

### Decision 2: 在 Invite 事务里解析 200 OK 的 SDP

`Invite` 现在丢弃应答 body。改为扫描 `c=IN IP4 <host>` 与 `m=audio <port>` 两条，得到设备 RTP 端点并存进 `talkCall`。响应缺少这两行时按 502（对端应答不可用）处理，不猜端口。

### Decision 3: 音频扇出挂在会话上，慢订阅者丢帧

`talkCall` 保存 `subscribers map[int]chan []byte`；会话的 UDP 读循环剥掉 12 字节 RTP 头后非阻塞投递，缓冲满即丢——与日志 Hub 的 drop-on-slow 一致，浏览器卡顿不能反压 SIP。

### Decision 4: 上行用固定 SSRC、序列号与时间戳按帧递增

每帧 160 字节 PCMU（20ms）。SSRC 由 session 派生（稳定），seq 从随机起点递增，timestamp 每帧 +160（8kHz）。不做 SR/RR 与重传。

### Decision 5: WebSocket 只承载音频，不结束对话

关闭 socket 只退订；结束对话仍走 `talk/stop`（发 BYE）。这样浏览器刷新不会静默挂断，而 `talk/stop` 的语义保持单一。

## Risks / Trade-offs

- **R1 设备侧 handleInvite 依赖 mediaService** → 未接线时返回 500/488 而不是 405；文档写明前提，e2e 使用真实接线。
- **R2 UDP 读循环与 SIP 读循环争用同一会话** → 音频 socket 是独立端口（TalkStart 分配），与 SIP 监听口无关，无争用。
- **R3 WebSocket 帧大小** → 只接受 ≤ 320 字节的二进制帧（20–40ms），超长帧丢弃并记 debug，避免误用成数据通道。

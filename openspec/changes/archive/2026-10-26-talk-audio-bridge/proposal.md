# Proposal

## Why

`channel-talk-and-snapshot` 交付了对讲的控制面（INVITE/BYE、错误语义、空闲与对端挂断回收），但**没有声音**：`TalkStart` 返回的 `ws_url` 指向不存在的端点，且设备节点的 acceptor 门禁只放行 MESSAGE，对 INVITE 直接回 405。结果是接口看起来能开对讲、实际两端都收不到一个音频帧。

## What Changes

- **设备接受音频 INVITE**：`device` 节点不再对 INVITE 回 405；携带 audio m-line（PCMU）的 INVITE 由本节点以 200 OK + SDP 应答（c= 为本节点地址，m=audio 为本地 RTP 端口）。
- **解析 SDP 应答**：`Invite` 从 200 OK 的 body 取出设备的 RTP 端点（c= + m=audio 端口），存入 talk 会话，作为上行发送目标。
- **下行音频**：对讲会话的本端 UDP socket 上收到的 RTP，去掉 12 字节头后把 PCMU 载荷经 WebSocket 二进制帧推给浏览器。
- **上行音频**：浏览器发来的二进制帧打包成 RTP（PT=0、SSRC 固定、序列号与时间戳递增）后发往设备的 RTP 端点。
- **WebSocket 端点**：`GET /v1/nodes/:id/channels/:ch/talk/audio?session=<id>`；未知/过期会话 404。

**BREAKING**：无。设备此前对 INVITE 回 405，本 change 只增加音频 INVITE 的受理。

## Capabilities

### Modified Capabilities

- `channel-http-api`：新增「对讲音频 WebSocket」契约（上下行帧格式、会话校验）。
- `device-node`：新增「device 接受音频 INVITE」要求（不得对音频 INVITE 回 405）。

对应路线图编号：#38 `talk-audio-bridge`。

## Impact

- `internal/app/acceptor_run.go`（设备门禁放行 INVITE）
- `internal/app/acceptor_invite.go`（设备侧 200 OK + SDP 应答）
- `internal/app/talk_transactions.go`（解析 200 OK 的 SDP）
- `internal/app/talk_call.go`（会话保存对端 RTP 端点、订阅/扇出）
- `internal/interface/http/`（新 WebSocket 路由）
- 测试：app 单测 + HTTP 契约 + e2e（真实 UDP 收发）

## Non-goals

- 不做混音、回声消除、多方对讲。
- 不支持 PCMU 之外的编码（G.722.1/AAC 需转码，暂不引入）。
- 不做 GB35114 加密对讲。
- 不改变对话之外的 SIP 行为（INVITE 视频点播仍走既有路径）。

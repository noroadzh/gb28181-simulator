# Tasks

## 1. 打通设备侧与端点解析

- [x] 1.1 `acceptor_run.go` 的设备门禁放行 INVITE（保留 ACK 静默、其余方法 405）；验收：单测断言 device 节点收到音频 INVITE 时不再回 405
- [x] 1.2 `Invite` 解析 200 OK 的 SDP（`c=IN IP4` + `m=audio`），把设备 RTP 端点存入会话；缺字段返回 502；验收：单测覆盖成功解析与缺行两种
- [x] 1.3 设备侧 `handleInvite` 对只有 video m-line 的 INVITE 回 488；验收：单测

## 2. 音频桥

- [x] 2.1 `talkCall` 增加订阅表与会话 UDP 读循环：剥 12 字节 RTP 头，非阻塞扇出载荷；验收：单测向 socket 发一个 RTP 包，订阅者收到 160 字节载荷，慢订阅者丢帧不阻塞
- [x] 2.2 `GET /v1/nodes/:id/channels/:ch/talk/audio?session=` WebSocket 下行：订阅并写二进制帧；未知会话 404；验收：httptest + 真实 socket 的集成用例
- [x] 2.3 上行：WebSocket 二进制帧（≤320 字节）打包 RTP（PT=0、SSRC 稳定、seq/ts 递增）发往设备端点；验收：UDP 监听端断言收到的 RTP 头
- [x] 2.4 e2e：真实二进制启动 device 节点 → talk/start → 连接 WS → 上下行各断言一帧；验收：`go test ./internal/test/e2e/...` 通过

## 3. 收尾

- [x] 3.1 `docs/web-ui-guide.md`：对讲从「实现中」改为可用，写明音频前提与失败码；验收：人工审阅
- [x] 3.2 `openspec validate talk-audio-bridge --strict --type change`、`make lint`、`make test` 通过；验收：退出码 0

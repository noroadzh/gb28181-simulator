# Tasks

## 1. SDP 与平台侧 INVITE

- [x] 1.1 平台侧视频 offer 生成与 answer 解析：`s=Play`、`m=video <port> <proto> 96`、`a=recvonly`（UDP）/`a=setup:passive`（TCP）；解析应答的 `c=`、`m=` 端口、`a=setup`、`y=`(SSRC)；应答缺 `c=` 时回落信令对端地址。验收：golden test 断言 offer 字节；单位测试覆盖 UDP/TCP、缺 `c=`、缺 `y=`、缺 `m=`
- [x] 1.2 `Acceptor.InviteVideo` 事务：在节点自身传输上发 INVITE、匹配 Call-ID/CSeq、返回最终应答；4xx/5xx 与超时映射为可辨识错误。验收：单测覆盖 200、404、超时；与既有 talk/playback 事务互不影响
- [x] 1.3 目标解析复用 `controlTarget`：平台 + 通道 → 下级 deviceID 与地址；无 `parent_id` 且多下级时报错。验收：单测覆盖 parent 命中、单下级回退、多下级失败、设备离线

## 2. 实时会话与收流

- [x] 2.1 `LiveSessionService`：以 (node, channel) 为键的会话表，持有传输、Call-ID、SSRC、最近收包时间；PS 帧广播器（慢订阅者有界丢帧）；重复 start 先结束旧会话。验收：单测覆盖重复 start、订阅者加入/离开、广播不阻塞收流
- [x] 2.2 UDP 接收器：offer 端口收 RTP → `RTPDeizer` → PS 帧；SSRC 非零时过滤；记录首包来源。验收：单测断言从测试 socket 注入 PS over RTP 后订阅者收到 PS 帧，错误 SSRC 被丢弃
- [x] 2.3 TCP 被动接收器：监听端口写入 offer（`a=setup:passive`），接受设备连接，按 2 字节大端长度前缀拆包 → RTP → PS。验收：单测用 TCP 客户端按前缀发送并断言 PS 帧到达
- [x] 2.4 超时与结束：建立后 30 秒无首包、此后连续 30 秒无包即结束；所有结束路径共用同一 `close()` 释放端口并关闭 PS 流。验收：假时钟单测覆盖无首包、断流、重复 close 幂等

## 3. HTTP 与 FLV 接线

- [x] 3.1 `POST /v1/nodes/:id/channels/:ch/live` 与 `.../live/stop`：请求体 `transport`，响应 `{session_id, transport, local_port, ssrc?}`；错误映射 501/404/502/504，stop 幂等 204。验收：httptest 覆盖成功、TCP、无下级、设备拒绝、超时、幂等停止
- [x] 3.2 FLV 实时源：`streaming` 增加实时源提供者端口，`Gateway.Subscribe` 优先实时源；`GET /v1/flv` 支持 `?source=live|local`，`source=live` 无会话时 404。验收：httptest 断言存在会话时输出实时帧、`source=local` 走本地源、设备节点行为不变
- [x] 3.3 生命周期接线：live stop 发 BYE；设备 BYE 结束会话；节点停止结束该节点全部会话。验收：单测断言 stop 后设备收到 BYE、对端 BYE 后端口关闭、节点停止后会话表清空

## 4. 端到端与收尾

- [x] 4.1 e2e：真实二进制起一个 platform-large + 一个模拟 device 子节点，`POST live` 后 `GET /v1/flv` 输出该设备通道的 PS 帧（FLV 头可解析）。验收：`go test ./internal/test/e2e/` 通过
- [x] 4.2 `openspec validate platform-live-pull --strict --type change`、`make lint`、`make test`（`-race`）全部通过。验收：退出码 0

# Spec Delta

## ADDED Requirements

### Requirement: 对讲音频 WebSocket

`GET /v1/nodes/:id/channels/:ch/talk/audio?session=<id>` MUST 升级为 WebSocket。服务器发出的二进制帧 MUST 是 PCMU 载荷（每帧一个 RTP 载荷，8kHz 单声道）；客户端发来的二进制帧 MUST 被封装成 RTP（PT=0）并发送到设备在 SDP 应答中声明的 RTP 端点。未知或已结束的 session MUST 返回 404。关闭 WebSocket MUST NOT 结束 SIP 对话——那是 `talk/stop` 的职责。

#### Scenario: 下行音频到达浏览器

- **WHEN** 前端以有效 session 连接，设备向平台的 RTP 端口发来一个 PCMU 包
- **THEN** 前端在 1 秒内收到一个二进制帧，内容为该 RTP 包的载荷（去掉 12 字节头）

#### Scenario: 上行音频到达设备

- **WHEN** 前端发送一帧 PCMU 载荷
- **THEN** 设备的 RTP 端点收到一个 RTP 包，PT=0、SSRC 稳定、序列号递增

#### Scenario: 未知会话

- **WHEN** 以不存在的 session 连接
- **THEN** HTTP 404，不升级为 WebSocket

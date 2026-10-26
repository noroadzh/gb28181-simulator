# Spec Delta

## ADDED Requirements

### Requirement: device 节点受理实时视频 INVITE

`device` 节点 MUST 受理带 `m=video`（PS/96）的 INVITE，以 200 OK + SDP 应答（`c=` 设备地址、`m=video` 设备 RTP 端口、`a=sendonly`、`a=rtpmap:96 PS/90000`），并从该通道媒体源向 offer 声明的平台端点持续发送 PS/RTP，直到收到 BYE 或节点停止。通道由 `To` 头的 user 部分指定；该通道没有媒体源时 MUST 返回 404。MUST NOT 对视频 INVITE 回 405/488。

#### Scenario: 平台拉取实时流

- **WHEN** 平台向 device 节点发送 `m=video` INVITE，`To` 为某通道且该通道配置了媒体源
- **THEN** 节点回 200 OK，SDP 声明自己的视频端口，并向平台端点发送以 PS pack 头开头的 RTP

#### Scenario: 通道无媒体源

- **WHEN** 视频 INVITE 指向的通道没有配置媒体源
- **THEN** 节点回 404 Not Found，不发送任何 RTP

#### Scenario: 挂断停止发送

- **WHEN** 平台对已建立的视频对话发送 BYE
- **THEN** 节点停止发送、释放该对话的 RTP 端口

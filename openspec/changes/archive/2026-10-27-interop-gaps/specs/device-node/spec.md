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

### Requirement: device 节点按 offer 选择 RTP 媒体传输

媒体默认走 UDP。offer 的 m-line 声明 `TCP/RTP/AVP` 时，`device` 节点 MUST 按 GB/T 28181 附录 D 走 TCP：`a=setup:passive`（或缺失）时由节点主动连接 offer 的 `c=` + 端口，并以 `a=setup:active` 应答；`a=setup:active` 时由节点监听，并在应答中给出 `a=setup:passive` 与监听端口。TCP 上每个 RTP 包 MUST 前置 2 字节大端长度（RFC 4571）。声明 TCP 却无法建立时 MUST 返回 488，而不是回落到 UDP。实时视频与回放 INVITE 遵循同一规则。

#### Scenario: TCP 承载的实时流

- **WHEN** offer 以 `m=video <port> TCP/RTP/AVP` 声明 TCP 且带 `a=setup:passive`
- **THEN** 节点主动连接平台端点，应答声明 `TCP/RTP/AVP` 与 `a=setup:active`
- **AND** 每个 RTP 包前带 2 字节大端长度前缀

#### Scenario: 平台主动连接设备

- **WHEN** offer 声明 TCP 且带 `a=setup:active`
- **THEN** 节点监听一个端口，应答声明 `a=setup:passive` 与该端口
- **AND** 平台连上后节点按长度前缀发送 RTP

#### Scenario: 无法承载的传输

- **WHEN** offer 声明 TCP，但既没有可连接的端口，也没有要求节点被动监听
- **THEN** 节点回 488 Not Acceptable Here，不发送 RTP

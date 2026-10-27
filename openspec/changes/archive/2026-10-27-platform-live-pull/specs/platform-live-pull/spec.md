# Spec Delta

## Purpose

平台节点主动向下级设备拉取实时视频：发起视频 INVITE、接收 RTP 并还原为 PS 帧，并以 (节点, 通道) 为单位持有实时会话。该会话是 HTTP-FLV 实时画面、后续级联媒体中继与平台录像的共同数据源。

## ADDED Requirements

### Requirement: 平台发起实时视频拉流

platform-large 与 platform-small 节点 MUST 能沿自身信令传输，向某通道所属的下级设备发起实时视频 INVITE。offer MUST 声明 `s=Play`、`m=video <平台接收端口> RTP/AVP 96`（或 TCP 时 `TCP/RTP/AVP 96`）与 `a=recvonly`。

目标设备 MUST 按以下顺序解析：通道条目的 `parent_id` 指向的在线下级；无 `parent_id` 且该平台只有一个在线下级时取该下级；否则以「没有可拉取的下级设备」失败。设备回 2xx 且 SDP 可解析时会话建立；非 2xx 时 MUST 报告设备给出的状态码；超时 MUST 报告无应答。同一 (节点, 通道) 重复 start MUST 先终止旧会话，不得并存。

#### Scenario: 拉流成功

- **WHEN** 平台对某通道发起 live start，其下级设备回 200 + 可解析的 SDP
- **THEN** 平台返回会话标识与本地接收端口
- **AND** 会话以 (节点, 通道) 登记为活动状态

#### Scenario: 设备拒绝

- **WHEN** 下级设备对视频 INVITE 回 4xx/5xx
- **THEN** live start 失败并报告该状态码，不登记会话

#### Scenario: 没有可拉取的下级设备

- **WHEN** 通道没有 `parent_id`，且平台没有或有多于一个在线下级
- **THEN** 返回 404，且不发送 INVITE

#### Scenario: 设备无应答

- **WHEN** 视频 INVITE 在约定时间内没有最终响应
- **THEN** 返回 504，且不登记会话

#### Scenario: 重复 start 拆旧会话

- **WHEN** 同一 (节点, 通道) 已有活动会话时再次 start
- **THEN** 旧会话先结束（发 BYE、释放端口），新会话再建立

### Requirement: 媒体接收与实时会话

平台 MUST 在会话建立前绑定接收端口。UDP 会话 MUST 在 offer 声明的端口上收取 RTP，按设备应答声明的 SSRC 过滤（应答未声明 SSRC 时不过滤），并经 RTP 解包还原为 PS 帧。TCP 会话 MUST 以 `a=setup:passive` 声明由平台监听，设备连接后每个 RTP 包带 2 字节大端长度前缀。

会话 MUST 以 (节点, 通道) 为键暴露 PS 帧流，供 FLV 网关与后续消费者订阅；会话 SHALL 记录传输方式、Call-ID 与最近收包时间。会话建立后 30 秒内未收到任何 RTP，或在此后连续 30 秒无包，MUST 结束会话。

#### Scenario: UDP 收流

- **WHEN** 设备向平台 offer 的 UDP 端口发送 PS over RTP
- **THEN** 会话收到 RTP 并还原出 PS 帧，订阅者能持续读到

#### Scenario: TCP 收流

- **WHEN** live start 选择 TCP，设备按 `a=setup:active` 连接平台监听端口
- **THEN** 平台按 2 字节长度前缀拆包，还原 PS 帧

#### Scenario: 非本会话的 RTP 被忽略

- **WHEN** 收到的 RTP SSRC 与设备应答声明的 SSRC 不一致
- **THEN** 该包被丢弃，不影响会话

#### Scenario: 无媒体自动结束

- **WHEN** 会话建立后 30 秒内没有收到 RTP，或连续 30 秒无包
- **THEN** 会话结束并释放接收端口

### Requirement: 实时会话生命周期

live stop MUST 向设备发送 BYE、结束会话并释放接收端口。设备在会话期间发来的 BYE MUST 结束会话。节点停止 MUST 结束该节点的全部实时会话。会话结束时 MUST 关闭其 PS 帧流，使订阅者读到流结束而不是挂起。

#### Scenario: 主动停止

- **WHEN** 调用 live stop
- **THEN** 平台发送 BYE，会话被移除，接收端口关闭，PS 流关闭

#### Scenario: 设备主动挂断

- **WHEN** 设备对活动会话发送 BYE
- **THEN** 平台结束该会话并释放端口，不向设备再发 BYE 之外的挂断请求

#### Scenario: 节点停止

- **WHEN** 平台节点被停止
- **THEN** 该节点全部实时会话结束，端口释放，订阅者流关闭

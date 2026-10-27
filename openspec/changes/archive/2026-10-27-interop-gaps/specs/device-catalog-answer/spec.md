# device-catalog-answer Delta

## MODIFIED Requirements

### Requirement: device 节点接收并响应 catalog query

device 节点注册成功后 MUST 启动一个 SIP 读循环，接受来自 platform 节点的 `MESSAGE/Query CmdType=Catalog` 请求，返回 200 OK 携带 device 自身 channel 列表（MANSCDP 标准字段）的 catalog response。

device 同时也是媒体对话的 UAS：INVITE、ACK、BYE、CANCEL 属于对话方法，MUST NOT 被 405 拒绝——BYE 与 CANCEL MUST 正常应答（200 OK / 481），否则平台无法结束对话。其余方法（OPTIONS、SUBSCRIBE、INFO 等）仍 MUST 返回 405 Method Not Allowed，Allow 头 MUST 列出 `MESSAGE, INVITE, ACK, BYE, CANCEL`，供探测方感知节点存活。

#### Scenario: 标准 catalog query 应答

- **WHEN** platform-large 节点向 device 节点发送 `MESSAGE/Query CmdType=Catalog` (SN=1)
- **THEN** device 节点 MUST 响应 200 OK，Content-Type 为 `Application/MANSCDP+XML`，body 为完整 catalog response，包含 device 自身 profile 中的所有 channel Item

#### Scenario: device 节点对非 MESSAGE 方法返回 405

- **WHEN** platform-large 节点向 device 节点发送 OPTIONS / SUBSCRIBE / INFO 请求
- **THEN** device 节点 MUST 响应 405 Method Not Allowed，Allow 头 MUST 包含 `MESSAGE`

#### Scenario: device 节点应答对话方法

- **WHEN** platform-large 节点向 device 节点发送 BYE 或 CANCEL
- **THEN** device 节点 MUST 应答 200 OK（CANCEL 无对应事务时为 481），MUST NOT 回 405

#### Scenario: device 节点对 ACK 静默丢弃

- **WHEN** 任何 peer 向 device 节点发送 ACK 请求
- **THEN** device 节点 MUST NOT 产生任何 SIP 响应——ACK 由其对端 INVITE 的 silence 隐式确认（RFC 3261 §13.2.2.4：UAS 不答 ACK），405/200 都是协议错误

#### Scenario: device 节点与 platform-large 共用 Acceptor

- **GIVEN** 一个进程内同时存在 device 节点和 platform-large 节点
- **WHEN** platform-large 节点向 device 节点发送 catalog query
- **THEN** device 节点 MUST 通过共享的 `Acceptor` 实例接收并响应；device 节点 MUST NOT 与 platform-large 节点争夺同一 socket 端口

#### Scenario: device 节点 read loop 与 Registrar 并存

- **GIVEN** device 节点注册流程已成功（RecordRegistration 完成）
- **THEN** device 节点 MUST 同时具备 keepalive 心跳发送（Keeper）与 catalog query 接收（ServeDevice）
- **AND** device 节点 MUST NOT 在注册成功前启动 catalog 接收 goroutine

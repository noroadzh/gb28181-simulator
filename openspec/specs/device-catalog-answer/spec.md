# device-catalog-answer Specification

## Purpose

本能力使 device 身份的节点在向 platform 完成注册后启动一个 SIP 读循环，接收并应答上游 platform 发来的 `MESSAGE/Query CmdType=Catalog`（目录查询）请求。它复用 Acceptor 的 `answerCatalog` 流水线，把 device 自身 channel 列表连同每个 channel 的 `<MediaSource>` 扩展段返回给 platform，从而打通「platform catalogPoller → device → 媒体源回填」的级联链路，让 platform 能在 catalog 中发现自己下游设备所暴露的媒体源。device 作为 UAS 只应答 MESSAGE（ACK 静默丢弃），其余方法返回 405 Method Not Allowed，供探测方感知节点存活。

## Requirements

### Requirement: device 节点接收并响应 catalog query

device 节点注册成功后 MUST 启动一个 SIP 读循环，接受来自 platform 节点的 `MESSAGE/Query CmdType=Catalog` 请求，返回 200 OK 携带 device 自身 channel 列表（MANSCDP 标准字段）的 catalog response。

#### Scenario: 标准 catalog query 应答

- **WHEN** platform-large 节点向 device 节点发送 `MESSAGE/Query CmdType=Catalog` (SN=1)
- **THEN** device 节点 MUST 响应 200 OK，Content-Type 为 `Application/MANSCDP+XML`，body 为完整 catalog response，包含 device 自身 profile 中的所有 channel Item

#### Scenario: device 节点对非 MESSAGE 方法返回 405

- **WHEN** platform-large 节点向 device 节点发送 OPTIONS / INVITE / SUBSCRIBE / BYE / INFO 非 MESSAGE 请求
- **THEN** device 节点 MUST 响应 405 Method Not Allowed，Allow 头 MUST 包含 `MESSAGE`

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

### Requirement: catalog response 携带 MediaSource 扩展

device 节点在 catalog response 中 MUST 复用 `cascade-media-source-propagation` 规定的 `<Extension><MediaSource>` 扩展段格式，将其 profile 中按 channel 配的 `MediaConfig` 嵌入 Item 内。

#### Scenario: 含 MediaSource 的 catalog response

- **GIVEN** device 节点 profile 中 `mediaConfigByChannel["34020000130000000001"] = {Kind: "file", Path: "/uploads/a.mp4", Loop: true}`
- **WHEN** device 节点应答 catalog query
- **THEN** Item 中 MUST 包含 `<Extension><MediaSource kind="file" path="/uploads/a.mp4" loop="true"/></Extension>` 段

#### Scenario: 无 MediaSource 的 catalog response 兼容真实设备

- **GIVEN** device 节点 profile 中无任何 channel 配置 MediaSource
- **WHEN** device 节点应答 catalog query
- **THEN** Item MUST NOT 包含 Extension 段（与真实国标设备行为一致）

#### Scenario: node 级 MediaSource fallback 到 channel

- **GIVEN** device 节点 profile 仅节点级 `mediaConfig = {Kind: "rtsp", Path: "rtsp://cam/stream"}`，无 channel 级配置
- **WHEN** device 节点应答 catalog query
- **THEN** 每个 channel Item MUST 携带节点级 MediaSource 扩展段（channel → node fallback 规则）

### Requirement: device 节点不发起 catalog poller

device 节点 MUST NOT 启动 `CatalogPoller`（device 无下游设备可查询）。

#### Scenario: device 节点不发送 catalog query

- **WHEN** device 节点通过 `ServeDevice` 启动
- **THEN** Acceptor 的 `catalogPoller` MUST 为 nil，`a.catalogPollEvery` 在 device 节点上 MUST 保持 0

### Requirement: NodeService 注册成功后启动 ServeDevice

`NodeService.register` 成功完成后，对 device 节点 MUST 调用 `Acceptor.ServeDevice(id, tr, realm)`，对 platform-large 节点 MUST 调用 `Acceptor.Serve(id, tr, realm, policy)`。

#### Scenario: device 节点注册后启动 read loop

- **WHEN** device 节点 `NodeService.register()` 返回 nil（注册成功）
- **THEN** NodeService MUST 调用 `s.acceptor.ServeDevice(id, tr, realm)` 启动 catalog query 接收 goroutine

#### Scenario: platform-large 节点注册后启动 read loop

- **WHEN** platform-large 节点 `NodeService.register()` 返回 nil（注册成功）
- **THEN** NodeService MUST 调用 `s.acceptor.Serve(id, tr, realm, policy)` 启动 SIP 接收 goroutine

#### Scenario: 注册失败时不启动 ServeDevice

- **WHEN** device 节点 `NodeService.register()` 返回 error（注册失败）
- **THEN** NodeService MUST NOT 调用 `s.acceptor.ServeDevice()`，node 状态 MUST 转入 `fault` 而非 `online`

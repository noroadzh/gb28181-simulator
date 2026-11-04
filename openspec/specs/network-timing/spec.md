# network-timing Specification

## Purpose

以模拟级实现 GB/T 28181 网络校时与弱媒体保活：信令 Date / DeviceTime 对齐，媒体面无媒体超时回收；不做完整 NTP 或 RTCP 栈。

## Requirements

### Requirement: SIP Date 与模拟校时

SIP 应答 MUST 携带 `Date` 头（调用方已显式提供时除外）。设备侧 MUST 可从 REGISTER 成功应答的 `Date` 学习与平台的时钟偏差，并在 `DeviceStatus.DeviceTime` 中体现；若查询请求本身带 `Date`，DeviceTime MUST 优先跟随该值。

#### Scenario: 应答带 Date

- **WHEN** 平台或设备发出 SIP 最终应答且调用方未自定义 Date
- **THEN** 应答含 IMF-fixdate 形式的 Date

### Requirement: 弱媒体保活

实时拉流/中继会话 MUST 在持续无媒体超过空闲阈值（默认 30s，可配置 `liveIdle`）后回收。完整 RTCP 收发不在本规格范围。

#### Scenario: 无媒体超时

- **WHEN** 活动 live 会话超过空闲阈值未收到媒体
- **THEN** 会话被结束（与既有 live watchdog 一致）

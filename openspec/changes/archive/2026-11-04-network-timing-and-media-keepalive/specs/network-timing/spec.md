## ADDED Requirements
### Requirement: SIP Date 与模拟校时
SIP 应答 MUST 携带 Date（除非调用方已提供）。设备 MUST 可从 REGISTER 200 的 Date 学习时钟偏差，并在 DeviceStatus.DeviceTime 中体现。

# Spec Delta

## ADDED Requirements

### Requirement: 设备控制状态面

`device` 节点 MUST 实现 `device-control-semantics` 中的 PTZ 姿态、光圈/预置位、RecordCmd、IFrameCmd 受理与状态更新；对未实现的 DragZoom/TargetTrack MUST 显式失败。

#### Scenario: DeviceControl PTZ 更新姿态

- **WHEN** device 在线并收到合法 PTZ DeviceControl
- **THEN** 回 200，且节点姿态状态相对受理前可观测地变化

#### Scenario: 入站 PlaybackControl 驱动会话

- **WHEN** device 存在回放会话并收到 PlaybackControl Pause
- **THEN** 回 200，会话进入暂停（与 `dynamic-catalog-alarm-and-playback` 一致）

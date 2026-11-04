# Spec Delta

## Purpose

定义设备侧 DeviceControl / 相关控制命令的可观测状态语义：云台姿态、录像控制标志、强制关键帧请求、光圈与预置位，使模拟器在联调中不再「只 ACK 不改状态」。

## ADDED Requirements

### Requirement: PTZ 姿态状态

设备节点在成功受理含 PTZ 指令的 `DeviceControl` 后 MUST 更新该节点（或目标通道）的模拟姿态：至少包含水平、垂直与变倍分量的相对状态；`stop` MUST 清除运动中标志。状态 MUST 可被后续查询路径读取（例如 DeviceStatus 扩展字段、2022 精确位置，或测试专用只读观测），MUST NOT 仅写日志而无状态变化。

#### Scenario: 上视后姿态可观测

- **WHEN** 设备收到向上 PTZ 的 DeviceControl 并回 200
- **THEN** 该设备的模拟垂直分量相对受理前发生变化（或运动中标志为 up）
- **AND** 再次查询/观测可见该变化

#### Scenario: stop 停止运动

- **WHEN** 设备在运动中收到 `stop`
- **THEN** 运动中标志清除，姿态保持停止前的值

### Requirement: 光圈与预置位控制

系统 MUST 支持光圈开/关与预置位调用的设备侧受理：HTTP/API 的 `irisin` / `irisout` / `preset_call`（及等价 SIP DeviceControl）在 device 节点上 MUST 更新对应模拟状态并回成功应答；若某编码路径仍无法表示，MUST 返回可辨识失败（501/ERROR），MUST NOT 回 200 空操作。

#### Scenario: 调用预置位

- **WHEN** 对 device 通道下发 `preset_call` 且 `preset_id` 在节点预置位列表中（或允许任意 id 的模拟器策略）
- **THEN** 回成功，当前预置位索引更新为该 id

#### Scenario: 光圈开

- **WHEN** 下发 `irisin`
- **THEN** 回成功，模拟光圈开度增加（或置为 opening 状态）

### Requirement: RecordCmd 与 IFrameCmd

设备节点 MUST 识别 `DeviceControl` 中的录像控制（`RecordCmd` 启停）与强制关键帧（`IFrameCmd`）：成功时回 200，并更新节点可观测标志（正在录像 / 最近一次 I 帧请求时间或计数）。平台侧转发行为与既有 DeviceControl 转发一致。

#### Scenario: 开始录像标志

- **WHEN** 设备收到启动录像的 RecordCmd
- **THEN** 回 200，节点「正在录像」标志为 true

#### Scenario: 强制关键帧

- **WHEN** 设备收到 IFrameCmd
- **THEN** 回 200，最近 I 帧请求计数加一或时间戳更新

### Requirement: 不支持的几何类控制显式失败

`DragZoom`、`TargetTrack` 在本阶段若未实现完整语义，设备 MUST 以可辨识失败应答（MANSCDP `Result=ERROR` 或 SIP/HTTP 4xx/501），MUST NOT 回 200 假装执行。

#### Scenario: DragZoom 未实现

- **WHEN** 设备收到 DragZoom 控制且本构建未实现
- **THEN** 应答标明 ERROR/未实现，不改变 PTZ 姿态

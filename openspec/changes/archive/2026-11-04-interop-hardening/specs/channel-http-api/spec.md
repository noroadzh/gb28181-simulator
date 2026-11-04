# Spec Delta

## MODIFIED Requirements

### Requirement: PTZ 云台控制

- `POST /v1/nodes/:id/channels/:ch/ptz` MUST 接收 `{cmd, speed?, preset_id?}` 并通过 MANSCDP DeviceControl 下发（device 自环或 platform 向下级）。
- cmd 枚举: `up, down, left, right, upleft, upright, downleft, downright, zoomin, zoomout, focusnear, focusfar, irisin, irisout, stop, preset_call`
- speed 取值 1-255；`preset_call` 时 `preset_id` 1-255 必填。
- `irisin` / `irisout` / `preset_call` MUST 在 device 路径上成功更新模拟状态（见 `device-control-semantics`）；不得再因「位布局未定义」对上述三者一律返回 501——仅当目标非 device、无下级或编码/策略明确拒绝时才返回 501/502/504。

#### Scenario: 八方向 PTZ

- **WHEN** POST ptz body=`{cmd:"up", speed:128}`
- **THEN** DeviceControl MANSCDP XML 通过 SIP MESSAGE 发送，结果 200

#### Scenario: 调预置位

- **WHEN** POST ptz body=`{cmd:"preset_call", preset_id:5}` 且目标为可达 device
- **THEN** DeviceControl 携带预置位 5，200 返回，设备当前预置位更新

#### Scenario: 光圈

- **WHEN** POST ptz body=`{cmd:"irisin"}` 且目标为可达 device
- **THEN** 200 返回，设备光圈模拟状态更新

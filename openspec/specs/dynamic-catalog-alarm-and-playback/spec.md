# dynamic-catalog-alarm-and-playback 规范

## Purpose
定义模拟器的运行时动态行为：通道列表变更、告警通知发送、录像查询/回放控制，以及通过扩展 MediaStatus 上报移动位置。

## Requirements

### Requirement: dynamic-catalog-update

设备节点 的通道列表 MUST 支持运行时更新。后续的 Catalog 查询 MUST 反映新增/移除的通道以及每个通道的在线/离线状态，且无需重启节点。

#### Scenario: channel goes offline
- **WHEN** an operator sets channel 1 status to offline via the runtime API
- **THEN** the next Catalog query response omits channel 1 or marks it with `Status=OFF`

### Requirement: alarm-notification-emission

设备节点 MUST 能够在运行时向其上级平台发出 Alarm notify 类型的 MANSCDP 消息。该消息 MUST 包含告警级别、时间和描述。

#### Scenario: proactive alarm push
- **WHEN** a device triggers an alarm event through the runtime API
- **THEN** the device sends a MANSCDP MESSAGE with `CmdType=Alarm` to its registered parent, and the parent responds with `200 OK`

### Requirement: record-query-response

设备节点 MUST 对 RecordInfo 查询返回 RecordItem 条目列表（含开始时间、结束时间、文件路径/大小），对应所请求的通道与时间范围。

#### Scenario: record list fetch
- **WHEN** a platform queries RecordInfo for channel 1 on device 34020000001320000001 for the last hour
- **THEN** the device returns a MANSCDP response containing at least one RecordItem covering that window

### Requirement: playback-control-handling

设备节点 MUST 接受 PlaybackControl 请求（开始/停止），参数合法时响应 `200 OK`，参数非法时响应 `400 Bad Request`。

#### Scenario: start playback
- **WHEN** a platform sends PlaybackControl with Start, valid channel, and time range
- **THEN** the device responds `200 OK`; subsequent MediaStatus may indicate playback session state

### Requirement: mobile-position-reporting

设备节点 MUST 在 MediaStatus notify 中包含可选的经度、纬度、速度字段（当这些字段已配置时）。

#### Scenario: position update
- **WHEN** a device has position configured (longitude 121.47, latitude 31.23)
- **THEN** its next MediaStatus notify contains `<Longitude>121.47</Longitude>` and `<Latitude>31.23</Latitude>`

### Requirement: runtime-trigger-api

模拟器 MUST 暴露 HTTP API 用于触发动态行为：推送告警、切换通道状态、更新设备位置。

#### Scenario: API-driven alarm
- **WHEN** a test script POSTs `/nodes/{id}/alarm` with priority and description
- **THEN** the device emits an Alarm notify to its parent and returns the generated event ID

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

### Requirement: 入站 SUBSCRIBE 接受 alarm / mobileposition 事件

节点必须（MUST）接受入站 `SUBSCRIBE` 的 `Event` 头部值为 `catalog`、`alarm`、`mobileposition`（大小写不敏感，空格去 TrimSpace），并返回 `200 OK`。其他事件值（如 `presence`、`dialog`）必须（MUST）回 `489 Bad Event`。`SubscribePort` 接口的 `Subscribe` 签名扩展第三个参数 `event string`（值仅保留原始输入，不校验大小写）；stub adapter 同步更新。订阅状态仍以 Call-ID 为键存入 `platform.subscribers`，由 Expires 定时器负责过期清理。

#### Scenario: 平台订阅 alarm 事件

- **WHEN** 对端向节点发送 `SUBSCRIBE sip:34020000002000000001@... SIP/2.0`，头部包含 `Event: alarm`、`Expires: 3600`
- **THEN** 节点回 `200 OK`，并在内部按 Call-ID 记录一条 `event=alarm` 的订阅者

#### Scenario: 未知事件仍 489

- **WHEN** 对端发送 `Event: presence` 的 SUBSCRIBE
- **THEN** 节点回 `489 Bad Event`

### Requirement: 报警触发向 alarm 订阅者推送 NOTIFY

当节点收到来自上级平台的 `Alarm` 类型 MESSAGE（或内部触发的 `AppendAlarm`）时，节点必须（MUST）向所有活跃的 `event=alarm` 订阅者发送 `CmdType=Alarm` 的 MANSCDP NOTIFY；订阅者数组为空时静默。NOTIFY 的 `Event` 头为 `alarm`，`SN` 递增且全局单增，`Content-Type: Application/MANSCDP+XML`。节点本身不做报警持久化排队，订阅者离线则事件只在本次触发范围内可见。

#### Scenario: 一个 alarm 订阅者收到 NOTIFY

- **WHEN** 节点已有一条 `event=alarm` 订阅者，且设备通过 runtime API 触发报警（优先级 1，移动侦测）
- **THEN** 节点向该订阅者发送 NOTIFY，body 为 `<Notify><CmdType>Alarm><SN>n</SN><AlarmPriority>1</AlarmPriority><AlarmMethod>...</AlarmMethod></Notify>`；上级无需重新查询即可看到事件

#### Scenario: 无 alarm 订阅者时不推送

- **WHEN** 节点没有任何活跃 alarm 订阅者，且设备触发报警
- **THEN** 节点仅记录 debug 日志，不发送任何 NOTIFY；后续订阅者在下次事件发生时才能看到

### Requirement: 位置变更向 mobileposition 订阅者推送 NOTIFY

当节点通过 runtime API 更新位置（`SetPosition` 或等价操作）时，节点必须（MUST）向所有活跃的 `event=mobileposition` 订阅者发送 `CmdType=MobilePosition` 的 MANSCDP NOTIFY。NOTIFY body 携带 `DeviceID`、`SN`、`Longitude`、`Latitude`、`Speed`、`Time`。位置订阅者没有位置配置时，初始 NOTIFY body 为空体或仅含 `CmdType`/`SN`。

#### Scenario: 位置更新触发推送

- **WHEN** 节点已有一条 `event=mobileposition` 订阅者，且通过 runtime API 将位置设为 `Longitude=121.47, Latitude=31.23, Speed=10`
- **THEN** 节点向订阅者发送 `MobilePosition` NOTIFY；body 中 `<Longitude>121.47</Longitude>` 与 `<Latitude>31.23</Latitude>` 均存在

### Requirement: alarm/mobileposition 初始 NOTIFY 语义

当平台第一次订阅 `alarm` 或 `mobileposition` 时，节点必须（MUST）在返回 `200 OK` 后立即发送一条初始 NOTIFY：
- `alarm`：空体或最小 body（仅 `CmdType`/`SN`），表示"无挂起报警"。
- `mobileposition`：若节点已配置位置，则携带当前坐标；否则空体。

后续的事件变化（触发报警 / 更新位置）再按上述规则推送。

#### Scenario: alarm 初始 NOTIFY 为空

- **WHEN** 平台订阅 `alarm`，节点无挂起报警
- **THEN** `200 OK` 之后立即有一条 `Event: alarm` NOTIFY，body 不含任何 `AlarmPriority` 或 `AlarmMethod`

#### Scenario: mobileposition 初始 NOTIFY 带当前位置

- **WHEN** 平台订阅 `mobileposition`，节点 profile 已配置 `Position{Longitude: 121.47, Latitude: 31.23}`
- **THEN** `200 OK` 之后立即有一条 `Event: mobileposition` NOTIFY，body 包含该坐标

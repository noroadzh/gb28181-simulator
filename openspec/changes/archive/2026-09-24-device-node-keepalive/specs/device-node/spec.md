# Spec Delta

## ADDED Requirements

### Requirement: Device node sends periodic MESSAGE keepalives while online

While a device node is online the system SHALL send a GB/T 28181 keepalive as a SIP `MESSAGE`
to the platform it registered with: the body is a MANSCDP `Keepalive` notify carrying the
20-digit device id and a monotonically increasing `SN`, sent with `Content-Type:
Application/MANSCDP+XML` through the node's own transport, and the sender waits a bounded time
for a matching 2xx. The keepalive interval, response timeout and consecutive-failure threshold
SHALL be configurable with defaults of 60s, 5s and 3.

#### Scenario: 心跳报文符合 MANSCDP Keepalive 通知

- **WHEN** 一个已注册并 `online` 的 device 节点到达一个心跳周期
- **THEN** 发出的 `MESSAGE` 满足：Request-URI 为该平台地址对应的 `sip:<上级ID>@<域>`，`From` / `To` 为该设备 ID，body 为含 `<CmdType>Keepalive</CmdType>`、`<DeviceID>`、`<SN>`、`<Status>OK</Status>` 的 XML，`Content-Type` 为 `Application/MANSCDP+XML`
- **AND** body 可被独立 XML 解析器解析且字段与构造值相等（golden test）

#### Scenario: SN 单调递增

- **WHEN** 同一节点连续发送三次心跳
- **THEN** 三次的 `SN` 严格递增（如 1、2、3），且每条心跳使用新的 `Call-ID`

#### Scenario: 心跳收到 200 OK 记为成功

- **WHEN** 平台对心跳 `MESSAGE` 返回 200 OK
- **THEN** 该次心跳成功，连续失败计数归零，结构化日志以 `node_id` 记录一次心跳成功事件

#### Scenario: 单次心跳无响应仅告警

- **WHEN** 一次心跳在超时时间内未收到匹配的 2xx，且连续失败次数尚未达到阈值
- **THEN** 记录一条告警日志（含 `node_id` 与连续失败次数），节点保持 `online` 并在下一个周期继续发送

#### Scenario: 连续失败达到阈值后回落 fault

- **WHEN** 连续 `max_failures`（默认 3）次心跳未收到匹配的 2xx
- **THEN** 停止该节点的心跳与重注册，节点状态由 `online` 转为 `fault`，监听端口被释放
- **AND** 错误信息说明是心跳连续失败并给出失败次数与阈值

#### Scenario: 非 2xx 终态响应记一次失败

- **WHEN** 平台对心跳返回 4xx / 5xx 终态响应
- **THEN** 该次心跳计为一次失败（计入连续失败阈值），不视为成功

#### Scenario: 不匹配的响应被忽略

- **WHEN** 等待心跳响应期间收到 `Call-ID` 与该心跳事务不匹配、或来自其他对端的响应
- **THEN** 该响应被忽略并继续等待匹配的响应，不据此判定心跳成功或失败

### Requirement: Device node re-registers before the granted expiry lapses

The system SHALL renew a device node's registration before the expiry the platform granted
lapses: the renewal point is half the granted lifetime, or 60 seconds before it when that is
earlier; a renewal reuses the registration transaction (including answering a 401 challenge)
and records the newly granted result on the node. A failed renewal SHALL be retried with
exponential backoff capped at 60s until it succeeds or the node stops.

#### Scenario: 到期前半程触发重注册

- **WHEN** 平台授予的 `Expires` 为 3600 秒，且已过约一半有效期
- **THEN** 节点重新发起 REGISTER 事务（新的 `Call-ID`），成功后更新记录的注册结果与到期时刻

#### Scenario: 平台缩短过期时间时按新值重算

- **WHEN** 重注册时平台在 200 OK 中给出更短的 `Expires`（如 600）
- **THEN** 后续重注册时刻按 600 秒重新计算（半程或到期前 60 秒，取更早者）

#### Scenario: 重注册同样应答 401 挑战

- **WHEN** 平台对重注册的 REGISTER 返回 401
- **THEN** 使用相同凭据计算 `Authorization` 并在同一 `Call-ID` 内重发，与首次注册流程一致

#### Scenario: 重注册失败后退避重试

- **WHEN** 一次重注册失败（超时 / 被拒 / 5xx）
- **THEN** 按指数退避（首次约 5 秒，上限 60 秒）安排下一次重注册并继续心跳；节点保持 `online`
- **AND** 退避期间不重复发起重注册，退避计数在成功后归零

#### Scenario: 重注册成功不改变在线状态

- **WHEN** 一个 `online` 节点完成一次重注册
- **THEN** 节点仍为 `online`（重注册是数据刷新，不是状态迁移），仅注册结果与到期时刻被更新

### Requirement: Device node unregisters gracefully when asked

The system SHALL let a device node leave explicitly: send a REGISTER with `Expires: 0` to the
platform (answering a 401 challenge if challenged), stop the node's keepalive and renewal, and
advance it `online → offline`, releasing its listener. A failed unregistration SHALL be reported
with its stage and leave the node `online` — its registration is still valid.

#### Scenario: 注销成功推进到 offline 并释放端口

- **WHEN** 对一个 `online` 且已注册的 device 节点请求注销，平台对 `Expires: 0` 的 REGISTER 返回 200 OK
- **THEN** 节点状态转为 `offline`，监听端口被释放，心跳与重注册均已停止
- **AND** 结构化日志以 `node_id` 记录一次注销成功事件

#### Scenario: 注销先停后台任务再释放端口

- **WHEN** 注销成功且节点被停止
- **THEN** 心跳 goroutine 在端口释放之前结束；注销后不再有任何心跳或 REGISTER 发往该平台

#### Scenario: 注销应答 401 挑战

- **WHEN** 平台对 `Expires: 0` 的 REGISTER 返回 401
- **THEN** 以相同凭据计算 `Authorization` 后重发 `Expires: 0` 的 REGISTER，收到 2xx 才算注销成功

#### Scenario: 注销失败保持 online 并报错

- **WHEN** 注销事务超时或收到终态非 2xx 响应
- **THEN** 返回含失败阶段的错误（复用 `send` / `challenge` / `response` / `timeout` 分期），节点保持 `online` 且心跳继续；注册本身仍有效

#### Scenario: 未在线节点注销为非法迁移

- **WHEN** 对一个 `offline`（或未注册）的节点请求注销
- **THEN** 返回非法状态迁移错误（HTTP 409），不发出任何报文

### Requirement: Keepalive and renewal run on the node's own transport and stop with the node

The system SHALL run keepalive and renewal as per-node background work confined to the
transport that node bound: starting when the node reaches `online` and stopping — before the
listener is released — when the node is stopped, unregistered, or faulted. No node's background
work may use another node's transport, and no goroutine may outlive its node.

#### Scenario: 停止节点后不再发送心跳

- **WHEN** 一个正在发心跳的节点被停止
- **THEN** 端口释放前心跳 goroutine 已退出；停止后不再有任何 `MESSAGE` 发往平台

#### Scenario: 故障节点不再发送心跳

- **WHEN** 节点因心跳连续失败回落 `fault`
- **THEN** 其心跳与重注册 goroutine 均已退出；对该节点再次 start 会启动新的后台任务

#### Scenario: 多节点后台任务互不串扰

- **WHEN** 进程内两个 device 节点各自向不同平台注册并同时保活
- **THEN** 每个节点只用自己的 transport 与自己的平台通信；停止其中一个不影响另一个的心跳与重注册

#### Scenario: 进程关闭时后台任务退出

- **WHEN** 模拟器进程开始关闭
- **THEN** 所有节点的心跳与重注册 goroutine 在监听端口关闭前后有序退出，不残留 goroutine

### Requirement: Keepalive parameters are declarative, optional and validated

The system SHALL accept keepalive parameters on a node's registration entry —
`heartbeat_interval`, `heartbeat_timeout` and `heartbeat_max_failures` — all optional, with
defaults of 60s, 5s and 3. Invalid values MUST fail configuration loading with an error naming
the entry index and the offending field, rather than being ignored.

#### Scenario: 声明的心跳参数生效

- **WHEN** 配置给出 `heartbeat_interval: 30s`、`heartbeat_timeout: 2s`、`heartbeat_max_failures: 5`
- **THEN** 该节点按 30 秒周期发心跳、单次等待 2 秒、连续 5 次失败才回落 `fault`

#### Scenario: 省略或零值时取缺省值

- **WHEN** 注册段给出但未声明任何心跳参数，或把某个心跳参数显式声明为 `0`
- **THEN** 周期为 60 秒、超时 5 秒、阈值为 3；零值与"未声明"等价，不视为非法

#### Scenario: 非法心跳参数导致配置加载失败

- **WHEN** `heartbeat_interval` 或 `heartbeat_timeout` 为负数，或 `heartbeat_timeout` 不小于 `heartbeat_interval`
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动
- **AND** `heartbeat_max_failures` 只能取 0（缺省）或正整数，负值在解码阶段即被拒绝，同样不会静默生效

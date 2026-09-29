## ADDED Requirements

### Requirement: Platform-large node accepts downstream keepalives

A platform-large node SHALL accept a downstream's MANSCDP `Keepalive` carried by a SIP `MESSAGE`
and SHALL answer it with `200 OK` when the device is in that node's online table; a keepalive MUST
refresh the row's last-seen time only and MUST NOT extend the granted lifetime. A keepalive from a
device that is not in the table SHALL be ignored.

作为 UAS 的 platform-large 节点必须接收下级的 MANSCDP `Keepalive`（承载于 SIP `MESSAGE`），
承认"这个下级还活着"，但不把它当作一次新的注册。

#### Scenario: 在册设备的心跳刷新最后可见时间

- **WHEN** 平台收到一条 `MESSAGE`，其 MANSCDP 体为 `CmdType = Keepalive`、`DeviceID` 与在线表中某条记录相同
- **THEN** 该记录的 `last_seen_at` 更新为当前时间
- **AND** 其 `registered_at`、`expires_at` 与 `granted` 均不变——有效期只由重注册刷新
- **AND** 在线表条目数不变

#### Scenario: 在册设备的心跳被确认

- **WHEN** 上述心跳来自一个在册设备且解析成功
- **THEN** 平台回一条 `200 OK`（无 body）给该请求的来源地址

#### Scenario: 未在册设备的心跳被忽略

- **WHEN** 心跳的 `DeviceID` 不在该节点的在线表中
- **THEN** 平台不落表、不回应，并记一条 warn 日志（含 `node_id` 与 device id）
- **AND** 该节点在线表不受影响

#### Scenario: 无法解析或非心跳的 MESSAGE 被忽略

- **WHEN** `MESSAGE` 的 body 不是合法 MANSCDP、缺 `CmdType`/`DeviceID`，或 `CmdType` 既非 `Keepalive` 也非 `Catalog`
- **THEN** 平台记一条 debug 日志后丢弃该消息，不回应、不改表、不因解析失败而中断受理循环

### Requirement: Platform-large node evicts devices whose registration lapsed

A serving platform-large node SHALL periodically remove every online row whose granted lifetime has
lapsed without a re-registration, and SHALL log each eviction with node id and device id. The sweep
MUST end with the node's serving goroutine and MUST NOT survive process shutdown.

平台必须周期性地把"授权有效期已过且未再注册"的下级移出在线表，否则拔了网线的设备会永远显示在线。

#### Scenario: 有效期过期且未刷新即被踢下线

- **WHEN** 平台节点在服务，且某条在线记录的 `expires_at` 已早于当前时间
- **THEN** 清扫时该记录被移出在线表
- **AND** 记一条 info 日志（含 `node_id`、device id、已过期时长），不含任何凭据
- **AND** 后续 `GET /v1/nodes/{id}/devices` 不再列出该设备

#### Scenario: 到期前重注册的设备继续保留

- **WHEN** 某记录在 `expires_at` 之前收到一次成功的 REGISTER
- **THEN** 清扫不移除它，且其 `expires_at` 按新授予的有效期顺延

#### Scenario: 心跳刷新不构成续期

- **WHEN** 某记录仅通过心跳刷新过 `last_seen_at`，但从未在有效期内重注册
- **THEN** 到 `expires_at` 时它仍被移出在线表

#### Scenario: 平台停止后清扫即停止

- **WHEN** 平台节点被停止，或服务进程关闭
- **THEN** 该节点的清扫 goroutine 随之结束，不再改动在线表
- **AND** 进程退出时不残留清扫 goroutine

### Requirement: Platform-large node answers MANSCDP catalog queries with its online device table

On receiving a `MESSAGE` whose MANSCDP `CmdType` is `Catalog` and whose `DeviceID` names the node
itself, a platform-large node SHALL answer `200 OK` carrying a `Catalog` response built from that
node's own online device table. The response SHALL echo the query's `SN`, SHALL list rows ordered by
device id, and SHALL report `SumNum = 0` with an empty `DeviceList` when the table is empty.

上级平台通过 `MESSAGE` 下发 `CmdType = Catalog` 的目录查询；平台必须以自己的在线设备表作答。

#### Scenario: 用在线设备应答目录查询

- **WHEN** 平台收到 `CmdType = Catalog` 的查询，`DeviceID` 指向该平台节点自身
- **THEN** 平台回一条 `200 OK`，body 为 MANSCDP `Catalog` 响应
- **AND** 响应的 `SN` 与查询的 `SN` 一致，便于查询方匹配事务
- **AND** `DeviceList` 由该节点当前在线设备表逐条生成，按 device id 升序，`SumNum` 等于条目数

#### Scenario: 空表应答

- **WHEN** 查询到达时该节点没有任何在线设备
- **THEN** 仍回 `200 OK`，body 为 `SumNum = 0` 且 `DeviceList` 为空的 `Catalog` 响应（不是错误、不是空 body）

#### Scenario: 目录条目的字段

- **WHEN** 一条在线记录被渲染为目录条目
- **THEN** 至少包含 `DeviceID`、`Name`、`Manufacturer`、`Model`、`Status`（在册即 `ON`）、`CivilCode`
  （取自 device id 前 6 位行政区域码）
- **AND** 条目不含地址之外的任何凭据信息

#### Scenario: 无法解析的目录查询被忽略

- **WHEN** `CmdType = Catalog` 但 body 无法解析或缺少 `SN`
- **THEN** 平台记一条 warn 日志后丢弃，不回应

#### Scenario: 两个平台的目录互不相见

- **WHEN** 两个 platform-large 节点各自有在线设备，且都收到目录查询
- **THEN** 每个节点只把自己表里的设备渲染进响应

### Requirement: MANSCDP parsing and rendering stay behind a port

MANSCDP decoding and rendering SHALL stay in an adapter behind `port.MANSCDPCodec`: the app layer
MUST see only domain values and rendered strings, and MUST NOT import `internal/adapter`.

MANSCDP 是 XML 协议细节，必须留在 adapter 层，用例层只看到领域值与渲染好的字符串。

#### Scenario: 解码与渲染都经端口

- **WHEN** 用例层需要理解或生成 MANSCDP 内容
- **THEN** 它调用 `port.MANSCDPCodec` 的 `DecodeNotify`（收）与 `MarshalCatalog`（发），且 `internal/app` 不 import `internal/adapter`

#### Scenario: XML 变化有 golden 覆盖

- **WHEN** MANSCDP 目录响应的字节序列发生变化
- **THEN** `internal/adapter/manscdp` 的 golden 测试失败，从而把改动暴露在适配层而非联调阶段

#### Scenario: 解码失败是错误而非恐慌

- **WHEN** 收到畸形的 MANSCDP body
- **THEN** 解码返回一个 error（不 panic），由调用方决定忽略

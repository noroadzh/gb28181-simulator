# device-node Specification

## Purpose

device（IPC / NVR / DVR）身份的行为规范。本 capability 覆盖其注册全流程：按配置向上级平台完成 REGISTER 事务、应答 401 Digest 挑战、凭 200 OK 推进到 `online`、失败回落 `fault`，以及注册所需配置的声明与校验。保活、注销、目录上报、点播等 device 其余行为后续在同一 capability 下增量添加。

## Requirements

### Requirement: Device node completes a REGISTER transaction when started

The system SHALL complete a REGISTER transaction for a device node that carries a registration
configuration: build REGISTER with `From` / `To` carrying the 20-digit device id at its domain,
a `Contact` bound to the node's own listening address, an `Expires`, a single `Call-ID` for the
whole transaction and `CSeq 1 REGISTER` for the first attempt; send it to the configured
upstream platform address through the node's own transport; and wait for a matching response up
to a bounded timeout.

#### Scenario: 发出的 REGISTER 报文符合 GB/T 28181 §L.1

- **WHEN** 启动一个携带注册配置的 device 节点（id `34020000001320000001`、域 `3402000000`、上级 `34020000002000000001`）
- **THEN** 对端收到的 REGISTER 满足：Request-URI 为 `sip:<上级ID>@<域>`，`From` 与 `To` 为该设备 ID，`Contact` 指向该节点自身绑定地址，含 `Expires`、`Via`（branch 唯一）、`Max-Forwards`、`User-Agent`、`Content-Length`
- **AND** 报文字节可被独立解析器（gosip）解析且头部字段与构造值逐字段相等

#### Scenario: 事务内 Call-ID 一致、CSeq 递增

- **WHEN** 一次注册事务包含首包与 401 后的重发
- **THEN** 两包 `Call-ID` 相同，`CSeq` 序号递增且方法均为 `REGISTER`，`Via` branch 各不相同

#### Scenario: 未配置注册信息时不发送 REGISTER

- **WHEN** 启动的 device 节点配置中没有注册段
- **THEN** 不发出任何 REGISTER，节点保持 `registering`，行为与引入本 capability 之前完全一致

#### Scenario: 可选 GB 版本头

- **WHEN** 节点配置声明了 GB 版本
- **THEN** 发出的 REGISTER 含 `X-GB-Ver` 头且值等于配置值；未声明时该头不出现在报文中

### Requirement: Device node answers a 401 Digest challenge

The system SHALL parse the `WWW-Authenticate` header of a 401 response to its REGISTER
(`realm`, `nonce`, `opaque`, `qop`, `algorithm`), compute the Digest response with the node's
credentials, and retransmit REGISTER carrying a complete `Authorization` header within the same
`Call-ID` and an incremented `CSeq`. `qop=auth` (RFC 7616 §3.4, with `nc` and `cnonce`) MUST be
used when the challenge offers it, and the RFC 2617 §3 form when it does not.

#### Scenario: 401 后带 Authorization 重发并通过服务端校验

- **WHEN** 收到 401，其 `WWW-Authenticate` 含 `realm="3402000000"`、`nonce`、`qop="auth"`、`algorithm=MD5`
- **THEN** 重发的 REGISTER 含 `Authorization`，其中 `username`、`realm`、`nonce`、`uri`、`response`、`nc`、`cnonce`、`qop`、`algorithm` 齐备
- **AND** `response` 与服务端用同一密码独立计算的结果逐字节相等，服务端据此返回 200 OK

#### Scenario: 挑战不含 qop 时回退 RFC 2617

- **WHEN** 收到的 `WWW-Authenticate` 不含 `qop`
- **THEN** `Authorization` 不含 `qop` / `nc` / `cnonce`，`response` 按 `MD5(HA1:nonce:HA2)` 计算并通过校验

#### Scenario: 挑战缺少必需字段时事务失败

- **WHEN** 401 响应的 `WWW-Authenticate` 缺少 `realm` 或 `nonce`，或该头根本不存在
- **THEN** 注册事务以错误结束并说明缺少的字段，不发出无意义的重发，节点不进入 `registered`

#### Scenario: 挑战算法不被支持时显式报错

- **WHEN** `WWW-Authenticate` 声明了实现不支持的 `algorithm`（例如 `SHA-256`）
- **THEN** 返回"算法不支持"的错误，不静默按 MD5 计算并发送

#### Scenario: nc 与 cnonce 在事务内唯一

- **WHEN** 同一节点连续完成两次注册事务，且挑战均要求 `qop=auth`
- **THEN** 每次事务使用新的 `cnonce`，`nc` 在该事务内从 `00000001` 起计数；两次事务的 `Authorization` 不完全相同（重放可区分）

### Requirement: Successful registration advances the node to online

The system SHALL advance a device node `registering → registered → online` only after a 2xx
response to the authenticated REGISTER arrives, and SHALL record the registration result
(expiry granted by the platform, the platform address, the completion time) on the node.

#### Scenario: 200 OK 后推进到 online

- **WHEN** 服务端对带 `Authorization` 的 REGISTER 返回 200 OK
- **THEN** 节点依次经历 `registered` 与 `online`；`GET /v1/nodes/{id}` 返回 `status=online`
- **AND** 结构化日志以 `node_id` 记录一次注册成功事件，含平台地址与服务端授予的 `Expires`

#### Scenario: 服务端缩短过期时间时以服务端为准

- **WHEN** 请求 `Expires=3600` 而 200 OK 中的 `Expires` 为 `600`
- **THEN** 节点记录的注册有效期为 600 秒，并在日志中体现该值

#### Scenario: 终态非 2xx 响应视为注册失败

- **WHEN** 服务端返回 403 / 404 / 5xx 等终态响应
- **THEN** 事务失败、节点回落 `fault`，错误说明响应状态码与阶段；不推进到 `registered`

#### Scenario: 等待响应超时视为注册失败

- **WHEN** 在配置的超时时间内未收到任何与该事务匹配的响应
- **THEN** 事务失败、节点回落 `fault`，错误表明是超时并给出超时值

#### Scenario: 状态推进被拒时不静默吞错

- **WHEN** 注册事务成功但状态迁移被拒绝（例如并发对该节点执行了停止）
- **THEN** 返回该迁移错误，节点状态保持实际值，不谎报注册成功

### Requirement: Registration failures are observable and leave no half-started node

The system SHALL surface every registration failure as an error carrying the failing stage
(bind / send / challenge / response / timeout), log it with the `node_id` field, and release the
node's signalling listener so the port is not held; credentials and Digest secrets MUST be
redacted in logs and in any API response.

#### Scenario: 注册失败回落 fault 并释放端口

- **WHEN** 注册事务在任何阶段失败
- **THEN** 节点状态由 `registering` 转为 `fault`
- **AND** 该节点绑定的监听端口被释放，可被同地址的其他节点或下一次 start 立即重新绑定

#### Scenario: HTTP 启动接口返回失败原因

- **WHEN** 通过控制接口启动 device 节点且注册失败
- **THEN** 接口返回非 2xx 状态码与 JSON 错误体，错误体含失败阶段与原因，且不含密码、`response`、`nonce` 明文

#### Scenario: 故障节点可再次启动

- **WHEN** 对处于 `fault` 的节点再次执行启动
- **THEN** 允许重走 `fault → idle → registering` 并重新发起注册；前一次的失败不阻塞后续尝试

#### Scenario: 注册相关日志与审计记录脱敏

- **WHEN** 注册过程产生日志或审计记录（含 REGISTER 报文原文）
- **THEN** `password`、`response`、`nonce`、`cnonce` 等敏感字段按既有脱敏规则处理，`node_id` 保留以便按节点过滤

### Requirement: Registration configuration is declarative, optional and validated

The system SHALL accept registration parameters on a node entry — upstream platform address,
authentication username and password, requested `expires` and transport — where the whole
section is optional; omitting it MUST preserve the pre-change behaviour (start binds the
listener and stops at `registering`). Invalid values MUST fail configuration loading with an
error naming the entry index and the offending field, rather than being ignored.

#### Scenario: 声明注册参数后按参数注册

- **WHEN** 节点配置含平台地址 `127.0.0.1:5060`、用户名等于设备 ID、密码、expires `3600`、transport `udp`
- **THEN** REGISTER 发往该地址、使用这些凭据、请求该有效期、经 UDP 发送

#### Scenario: 缺省值与省略

- **WHEN** 配置给出注册段但省略 `expires` 或 `transport`
- **THEN** `expires` 取 3600 秒、`transport` 取 `udp`；全部省略时节点不注册

#### Scenario: 非法注册参数导致配置加载失败

- **WHEN** 某条目平台地址缺少端口、`expires` 非正数、`transport` 不在 `udp` / `tcp` 内，或给出平台地址但未给出密码
- **THEN** 配置加载返回错误并指出条目的序号与字段，进程不启动

#### Scenario: 密码不以明文出现在输出中

- **WHEN** 配置中的密码出现在日志、配置回显或接口响应中
- **THEN** 该值被脱敏替换，原文不出现在任何输出通道

### Requirement: Each node registers over its own transport without crosstalk

The system SHALL confine a registration transaction to the transport bound by that node: the
REGISTER MUST leave through the node's own listener, and only responses matching the
transaction (`Call-ID`, and the peer the request was sent to) MAY conclude it.

#### Scenario: 两个 device 节点同时注册互不串扰

- **WHEN** 进程内两个 device 节点分别向两个不同的测试平台地址注册
- **THEN** 每个节点用自己的绑定地址收发，各自收到自己事务的 401 与 200 OK，最终均进入 `online`
- **AND** 任一节点的报文不出现在另一节点的事务记录中

#### Scenario: 不匹配的响应被忽略

- **WHEN** 节点在等待注册响应期间收到 `Call-ID` 与该事务不匹配的响应
- **THEN** 该响应被忽略并继续等待匹配的响应，不据此判定注册成功或失败

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

# device-node 规范

## Purpose

device（IPC / NVR / DVR）身份的行为规范。本 capability 覆盖其注册全流程：按配置向上级平台完成 REGISTER 事务、应答 401 Digest 挑战、凭 200 OK 推进到 `online`、失败回落 `fault`，以及注册所需配置的声明与校验。保活、注销、目录上报、点播等 device 其余行为后续在同一 capability 下增量添加。

## Requirements

### 需求：设备节点启动时完成 REGISTER 事务

系统 MUST 为携带注册配置的 设备节点 完成 REGISTER 事务：使用携带 20 位设备 id 在其域中的 `From` / `To`、绑定到节点自身监听地址的 `Contact`、`Expires`、贯穿整个事务的单一 `Call-ID`，以及首发包的 `CSeq 1 REGISTER` 构造 REGISTER；经节点自身的 transport 发往配置的上游平台地址；并在有界超时内等待匹配的响应。

#### 场景：发出的 REGISTER 报文符合 GB/T 28181 §L.1

- **WHEN** 启动一个携带注册配置的 device 节点（id `34020000001320000001`、域 `3402000000`、上级 `34020000002000000001`）
- **THEN** 对端收到的 REGISTER 满足：Request-URI 为 `sip:<上级ID>@<域>`，`From` 与 `To` 为该设备 ID，`Contact` 指向该节点自身绑定地址，含 `Expires`、`Via`（branch 唯一）、`Max-Forwards`、`User-Agent`、`Content-Length`
- **AND** 报文字节可被独立解析器（gosip）解析且头部字段与构造值逐字段相等

#### 场景：事务内 Call-ID 一致、CSeq 递增

- **WHEN** 一次注册事务包含首包与 401 后的重发
- **THEN** 两包 `Call-ID` 相同，`CSeq` 序号递增且方法均为 `REGISTER`，`Via` branch 各不相同

#### 场景：未配置注册信息时不发送 REGISTER

- **WHEN** 启动的 device 节点配置中没有注册段
- **THEN** 不发出任何 REGISTER，节点保持 `registering`，行为与引入本 capability 之前完全一致

#### 场景：可选 GB 版本头

- **WHEN** 节点配置声明了 GB 版本
- **THEN** 发出的 REGISTER 含 `X-GB-Ver` 头且值等于配置值；未声明时该头不出现在报文中

### 需求：设备节点应答 401 Digest 挑战

系统 MUST 解析其 REGISTER 收到的 401 响应中的 `WWW-Authenticate` 头部（`realm`、`nonce`、`opaque`、`qop`、`algorithm`），用节点凭据计算 Digest response，并在同一 `Call-ID`、递增 `CSeq` 下重发携带完整 `Authorization` 头的 REGISTER。挑战提供 `qop=auth` 时 MUST 采用 RFC 7616 §3.4 形式（含 `nc` 与 `cnonce`），未提供时 MUST 采用 RFC 2617 §3 形式。

#### 场景：401 后带 Authorization 重发并通过服务端校验

- **WHEN** 收到 401，其 `WWW-Authenticate` 含 `realm="3402000000"`、`nonce`、`qop="auth"`、`algorithm=MD5`
- **THEN** 重发的 REGISTER 含 `Authorization`，其中 `username`、`realm`、`nonce`、`uri`、`response`、`nc`、`cnonce`、`qop`、`algorithm` 齐备
- **AND** `response` 与服务端用同一密码独立计算的结果逐字节相等，服务端据此返回 200 OK

#### 场景：挑战不含 qop 时回退 RFC 2617

- **WHEN** 收到的 `WWW-Authenticate` 不含 `qop`
- **THEN** `Authorization` 不含 `qop` / `nc` / `cnonce`，`response` 按 `MD5(HA1:nonce:HA2)` 计算并通过校验

#### 场景：挑战缺少必需字段时事务失败

- **WHEN** 401 响应的 `WWW-Authenticate` 缺少 `realm` 或 `nonce`，或该头根本不存在
- **THEN** 注册事务以错误结束并说明缺少的字段，不发出无意义的重发，节点不进入 `registered`

#### 场景：挑战算法不被支持时显式报错

- **WHEN** `WWW-Authenticate` 声明了实现不支持的 `algorithm`（例如 `SHA-256`）
- **THEN** 返回"算法不支持"的错误，不静默按 MD5 计算并发送

#### 场景：nc 与 cnonce 在事务内唯一

- **WHEN** 同一节点连续完成两次注册事务，且挑战均要求 `qop=auth`
- **THEN** 每次事务使用新的 `cnonce`，`nc` 在该事务内从 `00000001` 起计数；两次事务的 `Authorization` 不完全相同（重放可区分）

### 需求：注册成功后节点推进到 online

系统 MUST 仅在带鉴权的 REGISTER 收到 2xx 响应后，将设备节点从 `registering` 推进到 `registering → registered → online`，并 MUST 在节点上记录注册结果（平台授予的有效期、平台地址、完成时刻）。

#### 场景：200 OK 后推进到 online

- **WHEN** 服务端对带 `Authorization` 的 REGISTER 返回 200 OK
- **THEN** 节点依次经历 `registered` 与 `online`；`GET /v1/nodes/{id}` 返回 `status=online`
- **AND** 结构化日志以 `node_id` 记录一次注册成功事件，含平台地址与服务端授予的 `Expires`

#### 场景：服务端缩短过期时间时以服务端为准

- **WHEN** 请求 `Expires=3600` 而 200 OK 中的 `Expires` 为 `600`
- **THEN** 节点记录的注册有效期为 600 秒，并在日志中体现该值

#### 场景：终态非 2xx 响应视为注册失败

- **WHEN** 服务端返回 403 / 404 / 5xx 等终态响应
- **THEN** 事务失败、节点回落 `fault`，错误说明响应状态码与阶段；不推进到 `registered`

#### 场景：等待响应超时视为注册失败

- **WHEN** 在配置的超时时间内未收到任何与该事务匹配的响应
- **THEN** 事务失败、节点回落 `fault`，错误表明是超时并给出超时值

#### 场景：状态推进被拒时不静默吞错

- **WHEN** 注册事务成功但状态迁移被拒绝（例如并发对该节点执行了停止）
- **THEN** 返回该迁移错误，节点状态保持实际值，不谎报注册成功

### 需求：注册失败可观测且不留半启动节点

系统 MUST 将每次注册失败作为携带失败阶段（bind / send / challenge / response / timeout）的错误暴露，以 `node_id` 字段记录日志，并释放节点的信令监听器以不占用端口；凭据与 Digest 密钥 MUST 在日志与任何 API 响应中脱敏。

#### 场景：注册失败回落 fault 并释放端口

- **WHEN** 注册事务在任何阶段失败
- **THEN** 节点状态由 `registering` 转为 `fault`
- **AND** 该节点绑定的监听端口被释放，可被同地址的其他节点或下一次 start 立即重新绑定

#### 场景：HTTP 启动接口返回失败原因

- **WHEN** 通过控制接口启动 device 节点且注册失败
- **THEN** 接口返回非 2xx 状态码与 JSON 错误体，错误体含失败阶段与原因，且不含密码、`response`、`nonce` 明文

#### 场景：故障节点可再次启动

- **WHEN** 对处于 `fault` 的节点再次执行启动
- **THEN** 允许重走 `fault → idle → registering` 并重新发起注册；前一次的失败不阻塞后续尝试

#### 场景：注册相关日志与审计记录脱敏

- **WHEN** 注册过程产生日志或审计记录（含 REGISTER 报文原文）
- **THEN** `password`、`response`、`nonce`、`cnonce` 等敏感字段按既有脱敏规则处理，`node_id` 保留以便按节点过滤

### 需求：注册配置是声明式、可选且经校验的

系统 MUST 接受节点条目上的注册参数——上游平台地址、鉴权用户名与密码、请求的 `expires` 与 transport——整段可选；省略时 MUST 保持变更前行为（start 绑定监听器并停留在 `registering`）。非法值 MUST 使配置加载失败并报出条目序号与出错字段，而不是被忽略。

#### 场景：声明注册参数后按参数注册

- **WHEN** 节点配置含平台地址 `127.0.0.1:5060`、用户名等于设备 ID、密码、expires `3600`、transport `udp`
- **THEN** REGISTER 发往该地址、使用这些凭据、请求该有效期、经 UDP 发送

#### 场景：缺省值与省略

- **WHEN** 配置给出注册段但省略 `expires` 或 `transport`
- **THEN** `expires` 取 3600 秒、`transport` 取 `udp`；全部省略时节点不注册

#### 场景：非法注册参数导致配置加载失败

- **WHEN** 某条目平台地址缺少端口、`expires` 非正数、`transport` 不在 `udp` / `tcp` 内，或给出平台地址但未给出密码
- **THEN** 配置加载返回错误并指出条目的序号与字段，进程不启动

#### 场景：密码不以明文出现在输出中

- **WHEN** 配置中的密码出现在日志、配置回显或接口响应中
- **THEN** 该值被脱敏替换，原文不出现在任何输出通道

### 需求：每个节点经自身 transport 注册且互不串扰

系统 MUST 将注册事务限制在该节点绑定的 transport 上：REGISTER 必须经节点自身的监听器发出，且只有与该事务匹配（`Call-ID` 及请求发往的对端）的响应 MAY 结论该事务。

#### 场景：两个 device 节点同时注册互不串扰

- **WHEN** 进程内两个 device 节点分别向两个不同的测试平台地址注册
- **THEN** 每个节点用自己的绑定地址收发，各自收到自己事务的 401 与 200 OK，最终均进入 `online`
- **AND** 任一节点的报文不出现在另一节点的事务记录中

#### 场景：不匹配的响应被忽略

- **WHEN** 节点在等待注册响应期间收到 `Call-ID` 与该事务不匹配的响应
- **THEN** 该响应被忽略并继续等待匹配的响应，不据此判定注册成功或失败

### 需求：设备节点在线期间周期性发送 MESSAGE 保活

当 设备节点 处于 online 状态时，系统 MUST 以 SIP `MESSAGE` 形式向其注册的平台发送 GB/T 28181 保活：body 为携带 20 位设备 id 与单调递增 `SN` 的 MANSCDP `Keepalive` notify，以 `Content-Type: Application/MANSCDP+XML` 经节点自身 transport 发送，发送方在有界时间内等待匹配的 2xx。心跳间隔、响应超时与连续失败阈值 MUST 可配置，默认值分别为 60s、5s 与 3。

#### 场景：心跳报文符合 MANSCDP Keepalive 通知

- **WHEN** 一个已注册并 `online` 的 device 节点到达一个心跳周期
- **THEN** 发出的 `MESSAGE` 满足：Request-URI 为该平台地址对应的 `sip:<上级ID>@<域>`，`From` / `To` 为该设备 ID，body 为含 `<CmdType>Keepalive</CmdType>`、`<DeviceID>`、`<SN>`、`<Status>OK</Status>` 的 XML，`Content-Type` 为 `Application/MANSCDP+XML`
- **AND** body 可被独立 XML 解析器解析且字段与构造值相等（golden test）

#### 场景：SN 单调递增

- **WHEN** 同一节点连续发送三次心跳
- **THEN** 三次的 `SN` 严格递增（如 1、2、3），且每条心跳使用新的 `Call-ID`

#### 场景：心跳收到 200 OK 记为成功

- **WHEN** 平台对心跳 `MESSAGE` 返回 200 OK
- **THEN** 该次心跳成功，连续失败计数归零，结构化日志以 `node_id` 记录一次心跳成功事件

#### 场景：单次心跳无响应仅告警

- **WHEN** 一次心跳在超时时间内未收到匹配的 2xx，且连续失败次数尚未达到阈值
- **THEN** 记录一条告警日志（含 `node_id` 与连续失败次数），节点保持 `online` 并在下一个周期继续发送

#### 场景：连续失败达到阈值后回落 fault

- **WHEN** 连续 `max_failures`（默认 3）次心跳未收到匹配的 2xx
- **THEN** 停止该节点的心跳与重注册，节点状态由 `online` 转为 `fault`，监听端口被释放
- **AND** 错误信息说明是心跳连续失败并给出失败次数与阈值

#### 场景：非 2xx 终态响应记一次失败

- **WHEN** 平台对心跳返回 4xx / 5xx 终态响应
- **THEN** 该次心跳计为一次失败（计入连续失败阈值），不视为成功

#### 场景：不匹配的响应被忽略

- **WHEN** 等待心跳响应期间收到 `Call-ID` 与该心跳事务不匹配、或来自其他对端的响应
- **THEN** 该响应被忽略并继续等待匹配的响应，不据此判定心跳成功或失败

### 需求：设备节点在授予的有效期届满前重注册

系统 MUST 在平台授予的有效期届满前续订设备节点的注册：续订点为授予有效期的半程，或到期前 60 秒（取更早者）；续订复用注册事务（包括应答 401 挑战），并把新授予的结果记录在节点上。失败的续订 MUST 以指数退避重试（上限 60s），直至成功或节点停止。

#### 场景：到期前半程触发重注册

- **WHEN** 平台授予的 `Expires` 为 3600 秒，且已过约一半有效期
- **THEN** 节点重新发起 REGISTER 事务（新的 `Call-ID`），成功后更新记录的注册结果与到期时刻

#### 场景：平台缩短过期时间时按新值重算

- **WHEN** 重注册时平台在 200 OK 中给出更短的 `Expires`（如 600）
- **THEN** 后续重注册时刻按 600 秒重新计算（半程或到期前 60 秒，取更早者）

#### 场景：重注册同样应答 401 挑战

- **WHEN** 平台对重注册的 REGISTER 返回 401
- **THEN** 使用相同凭据计算 `Authorization` 并在同一 `Call-ID` 内重发，与首次注册流程一致

#### 场景：重注册失败后退避重试

- **WHEN** 一次重注册失败（超时 / 被拒 / 5xx）
- **THEN** 按指数退避（首次约 5 秒，上限 60 秒）安排下一次重注册并继续心跳；节点保持 `online`
- **AND** 退避期间不重复发起重注册，退避计数在成功后归零

#### 场景：重注册成功不改变在线状态

- **WHEN** 一个 `online` 节点完成一次重注册
- **THEN** 节点仍为 `online`（重注册是数据刷新，不是状态迁移），仅注册结果与到期时刻被更新

### 需求：设备节点被要求时优雅注销

系统 MUST 允许设备节点显式离开：向平台发送 `Expires: 0` 的 REGISTER（被挑战时应答 401 挑战），停止节点的心跳与续订，并将其推进 `online → offline`、释放监听器。失败的注销 MUST 连同其阶段一起上报，并让节点保持 `online`——其注册仍然有效。

#### 场景：注销成功推进到 offline 并释放端口

- **WHEN** 对一个 `online` 且已注册的 device 节点请求注销，平台对 `Expires: 0` 的 REGISTER 返回 200 OK
- **THEN** 节点状态转为 `offline`，监听端口被释放，心跳与重注册均已停止
- **AND** 结构化日志以 `node_id` 记录一次注销成功事件

#### 场景：注销先停后台任务再释放端口

- **WHEN** 注销成功且节点被停止
- **THEN** 心跳 goroutine 在端口释放之前结束；注销后不再有任何心跳或 REGISTER 发往该平台

#### 场景：注销应答 401 挑战

- **WHEN** 平台对 `Expires: 0` 的 REGISTER 返回 401
- **THEN** 以相同凭据计算 `Authorization` 后重发 `Expires: 0` 的 REGISTER，收到 2xx 才算注销成功

#### 场景：注销失败保持 online 并报错

- **WHEN** 注销事务超时或收到终态非 2xx 响应
- **THEN** 返回含失败阶段的错误（复用 `send` / `challenge` / `response` / `timeout` 分期），节点保持 `online` 且心跳继续；注册本身仍有效

#### 场景：未在线节点注销为非法迁移

- **WHEN** 对一个 `offline`（或未注册）的节点请求注销
- **THEN** 返回非法状态迁移错误（HTTP 409），不发出任何报文

### 需求：保活与续订运行在节点自身 transport 上并随节点停止

系统 MUST 将保活与续订作为每节点后台任务运行，且限制在该节点绑定的 transport 上：节点到达 `online` 时启动，节点被停止、注销或故障时——在监听器释放之前——停止。任何节点的后台工作不得使用其他节点的 transport，任何 goroutine 不得比其节点活得更久。

#### 场景：停止节点后不再发送心跳

- **WHEN** 一个正在发心跳的节点被停止
- **THEN** 端口释放前心跳 goroutine 已退出；停止后不再有任何 `MESSAGE` 发往平台

#### 场景：故障节点不再发送心跳

- **WHEN** 节点因心跳连续失败回落 `fault`
- **THEN** 其心跳与重注册 goroutine 均已退出；对该节点再次 start 会启动新的后台任务

#### 场景：多节点后台任务互不串扰

- **WHEN** 进程内两个 device 节点各自向不同平台注册并同时保活
- **THEN** 每个节点只用自己的 transport 与自己的平台通信；停止其中一个不影响另一个的心跳与重注册

#### 场景：进程关闭时后台任务退出

- **WHEN** 模拟器进程开始关闭
- **THEN** 所有节点的心跳与重注册 goroutine 在监听端口关闭前后有序退出，不残留 goroutine

### 需求：保活参数是声明式、可选且经校验的

系统 MUST 接受节点注册条目上的保活参数——`heartbeat_interval`、`heartbeat_timeout` 与 `heartbeat_max_failures`——全部可选，默认值分别为 60s、5s 与 3。非法值 MUST 使配置加载失败并报出条目序号与出错字段，而不是被忽略。

#### 场景：声明的心跳参数生效

- **WHEN** 配置给出 `heartbeat_interval: 30s`、`heartbeat_timeout: 2s`、`heartbeat_max_failures: 5`
- **THEN** 该节点按 30 秒周期发心跳、单次等待 2 秒、连续 5 次失败才回落 `fault`

#### 场景：省略或零值时取缺省值

- **WHEN** 注册段给出但未声明任何心跳参数，或把某个心跳参数显式声明为 `0`
- **THEN** 周期为 60 秒、超时 5 秒、阈值为 3；零值与"未声明"等价，不视为非法

#### 场景：非法心跳参数导致配置加载失败

- **WHEN** `heartbeat_interval` 或 `heartbeat_timeout` 为负数，或 `heartbeat_timeout` 不小于 `heartbeat_interval`
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动
- **AND** `heartbeat_max_failures` 只能取 0（缺省）或正整数，负值在解码阶段即被拒绝，同样不会静默生效

### Requirement: Multi-channel Catalog response
A device node with multiple channels in its `NodeProfile` MUST respond to a Catalog query with one `<Item>` per channel, each with the channel's own 20-digit DeviceID (not the parent device id).

#### Scenario: Three-channel device responds with three items
- **WHEN** a device node (id `34020000001320000001`) with channels `ch01` (`34020000001320000001`), `ch02` (`34020000001320000002`), `ch03` (`34020000001320000003`) receives a Catalog query
- **THEN** the response contains three `<Item>` elements, one per channel, each with the respective channel DeviceID

#### Scenario: Single-channel device responds with one item
- **WHEN** a device node without channels (fallback to device id as default channel) receives a Catalog query
- **THEN** the response contains one `<Item>` with the device id as DeviceID (backward compatible)

### Requirement: Channel-level SIP INVITE routing
When a device node receives a SIP INVITE for a specific channel (via `Channel-ID` parameter or route), it MUST route to the media source configured for that channel.

#### Scenario: INVITE with Channel-ID routes to channel media
- **WHEN** device receives INVITE with `Channel-ID: 34020000001320000002` and channel `ch02` has RTSP media source
- **THEN** the PS output corresponds to the RTSP source of `ch02`, not the node-level default

### Requirement: Channel status in Catalog
Each channel item in the Catalog response MUST include the channel's current status (ON/OFF).

#### Scenario: Channel status reflected in Catalog
- **WHEN** device channel `ch01` status is `ON` and `ch02` status is `OFF`
- **THEN** Catalog items reflect `Status` accordingly

# platform-large-node 规范

## Purpose

platform-large（大平台）身份的行为规范。本 capability 覆盖其作为 UAS 的注册受理：
接收下级的 REGISTER、下发 401 Digest 挑战、校验 Authorization、回 200 OK 并协商有效期，
以及随之建立的在线设备表与其声明式配置。心跳接收与超时踢线、MANSCDP 目录查询与响应、
级联转发属于后续增量。

---

## Requirements

### 需求：platform-large 节点以 UAS 身份接受下级 REGISTER

系统 MUST 在其自身传输上充当 UAS：收到 REGISTER 后先下发挑战，校验下级回传的凭据，再以 200 OK
授予注册。授予响应 MUST 携带协商后的 `Expires`、回显下级的 `Contact`，并复制请求中的事务标识
（`Via`、`From`、`To` 带 tag、`Call-ID`、`CSeq`），使下级能匹配应答与其事务。

#### 场景：首次 REGISTER 触发 401 挑战

- **WHEN** 平台收到一条不含 `Authorization` 的 REGISTER
- **THEN** 回 401，带 `WWW-Authenticate: Digest realm="<realm>", nonce="...", qop="auth", algorithm=MD5`
- **AND** 响应回抄请求的 `Via` / `From` / `To`（带 tag）/ `Call-ID` / `CSeq`；不修改在线设备表

#### 场景：挑战每次都是新的 nonce

- **WHEN** 同一或不同下级连续两次收到挑战
- **THEN** 两次的 `nonce` 不同；挑战不依赖任何请求上下文（无状态）

#### 场景：凭据正确后回 200 OK 并入表

- **WHEN** 下级以 `Authorization` 重发 REGISTER，且 username 在平台 `accounts` 内、response 校验通过
- **THEN** 回 200 OK，`Expires` 为协商后的有效期，`Contact` 与请求一致，并带 `Date`
- **AND** 该下级被记入在线设备表（deviceID、来源地址、Contact、传输、注册时刻、授予有效期、最后活跃时刻）

#### 场景：重注册刷新同一条记录

- **WHEN** 一个已在表中的下级再次成功注册
- **THEN** 在线表里仍只有一条该 deviceID 的记录，且其注册时刻、授予有效期与最后活跃时刻被刷新

#### 场景：非 REGISTER 报文被忽略

- **WHEN** 平台收到非 REGISTER 的 SIP 报文（如 MESSAGE 心跳）
- **THEN** 记一条 debug 日志后丢弃，不影响在线设备表
- **AND** 受理循环继续运行（本 capability 不消费心跳；心跳接收与踢线属于后续 change）

### 需求：platform-large 节点按配置账号校验下级身份

系统 MUST 仅按平台节点上声明的账号校验下级身份。缺失或无法解析的凭据 MUST 以 401 重新挑战；
未知 username，或格式合法但 response 不匹配的 `Authorization`，MUST 以 403 作答且不再下发新挑战
（GB/T 28181 §L.2）。密码 MUST NOT 出现在日志、错误 body 或 HTTP 响应中。

#### 场景：Authorization 无法解析时重新挑战

- **WHEN** 收到的 REGISTER 带 `Authorization`，但头值无法解析（缺字段 / 非 Digest / 语法错）
- **THEN** 回 401 并给出新挑战；不计入拒绝，不影响在线设备表

#### 场景：未知 username 回 403

- **WHEN** `Authorization` 的 username 不在平台 `accounts` 内
- **THEN** 回 403，**不再下发新挑战**；在线设备表不变；日志以 username（不含密码）记录一次拒绝

#### 场景：response 校验失败回 403

- **WHEN** username 已知且 `Authorization` 可解析，但 response 与按存储密码计算的期望值不符
- **THEN** 回 403，不下发新挑战；在线设备表不变

#### 场景：鉴权失败不影响既有在线记录

- **WHEN** 一个已在表中的下级后续注册鉴权失败
- **THEN** 表中既有记录保持不变（本 capability 不因单次失败踢线）

### 需求：platform-large 节点协商授予的注册有效期

系统 MUST 将下级请求的 `Expires` 钳制到平台配置的 `min_expires` .. `max_expires` 窗口；请求未带
`Expires` 时回退到 `default_expires`。`Expires: 0` 必须被视为显式注销：回 200 OK 并将该 deviceID
从在线设备表移除。

#### 场景：缺省有效期

- **WHEN** REGISTER 未带 `Expires`（或带 0 以外但平台未配置任何档位）
- **THEN** 授予 `default_expires`（默认 3600 秒）并在 200 OK 中给出

#### 场景：请求值被钳制到窗口内

- **WHEN** 请求 `Expires` 小于 `min_expires` 或大于 `max_expires`
- **THEN** 授予值分别为 `min_expires` / `max_expires`；不会出现窗口外的授予值

#### 场景：请求值在窗口内时原样授予

- **WHEN** 请求 `Expires` 落在窗口内
- **THEN** 授予值与请求值相同

#### 场景：Expires: 0 视为注销

- **WHEN** 一条通过鉴权的 REGISTER 带 `Expires: 0`
- **THEN** 回 200 OK（含 `Expires: 0`）并把该 deviceID 从在线设备表移除；不报错、不改节点状态

### 需求：platform-large 节点为每个节点维护独立的在线设备表

系统 MUST 为每个 platform-large 节点维护一张当前已授予注册的下级表，包含：device id、来源地址、
`Contact`、传输、协议版本、授予有效期、注册时刻与最后活跃时刻。该表局限于接受该注册的节点，
节点停止时清空，且不能被其他节点可见或修改。

#### 场景：受理成功后出现在表中

- **WHEN** 一个下级成功注册到某 platform-large 节点
- **THEN** 该节点的在线设备表含这条记录，字段与受理结果一致；其他节点的表不受影响

#### 场景：两个平台节点各自记账

- **WHEN** 两个 platform-large 节点各受理一个下级
- **THEN** 每个节点只看到自己的下级；停止其中一个不影响另一个的表

#### 场景：节点停止后表被清空

- **WHEN** 一个 platform-large 节点被停止
- **THEN** 其在线设备表被清空，该节点不再受理任何 REGISTER，监听端口被释放

### 需求：平台服务随节点启停

platform-large 节点启动时必须开始服务——推进 `registering → online` 表示"平台正在服务"——
并在监听端口释放前停止服务，无论节点是被停止、发生故障还是进程关闭。任何服务 goroutine 的
生命周期不得超过其所属节点或进程。

#### 场景：启动即进入服务状态

- **WHEN** 启动一个 platform-large 节点
- **THEN** 节点推进到 `online`，并在其 transport 上受理 REGISTER

#### 场景：停止先停受理再释放端口

- **WHEN** 停止一个正在服务的 platform-large 节点
- **THEN** 受理 goroutine 在监听端口释放前退出；停止后不再发出任何响应

#### 场景：进程关闭时无残留 goroutine

- **WHEN** 模拟器进程开始关闭
- **THEN** 所有 platform-large 节点的受理 goroutine 有序退出，在线设备表随之释放

### 需求：平台服务为声明式、可选且经配置校验

系统 MUST 在 `nodes:` 条目上接受可选的 `platform:` 段——`realm`、`accounts`（username/password 对）
以及 `min_expires` / `default_expires` / `max_expires` 有效期窗口。所有键可选且有文档化的缺省值
（realm 默认为节点的 domain，有效期窗口默认为 60/3600/86400 秒）。非法值必须导致配置加载失败并
报错，指出条目序号与违规字段，而非被静默忽略。

#### 场景：声明的 accounts 与 realm 生效

- **WHEN** 条目声明 `platform.realm` 与两条 accounts
- **THEN** 只有这两个 username 能通过鉴权，挑战中的 realm 与配置一致

#### 场景：省略 platform 段时取缺省

- **WHEN** 一个 platform-large 条目未声明 `platform:` 段
- **THEN** realm 取该节点的 domain，有效期窗口取 60 / 3600 / 86400 秒；节点仍会受理注册
  （但没有声明任何 account 时，所有 username 都会被拒绝）

#### 场景：非法 platform 配置导致加载失败

- **WHEN** `platform.accounts` 出现重复 username 或空密码、`realm` 为空，或有效期三档不满足
  `0 < min ≤ default ≤ max`
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动；错误信息不含密码明文

### 需求：platform-large 节点接受下级心跳

platform-large 节点必须接受下级的 MANSCDP `Keepalive`（承载于 SIP `MESSAGE`），
承认"这个下级还活着"，但不把它当作一次新的注册。

#### 场景：在册设备的心跳刷新最后可见时间

- **WHEN** 平台收到一条 `MESSAGE`，其 MANSCDP 体为 `CmdType = Keepalive`、`DeviceID` 与在线表中某条记录相同
- **THEN** 该记录的 `last_seen_at` 更新为当前时间
- **AND** 其 `registered_at`、`expires_at` 与 `granted` 均不变——有效期只由重注册刷新
- **AND** 在线表条目数不变

#### 场景：在册设备的心跳被确认

- **WHEN** 上述心跳来自一个在册设备且解析成功
- **THEN** 平台回一条 `200 OK`（无 body）给该请求的来源地址

#### 场景：未在册设备的心跳被忽略

- **WHEN** 心跳的 `DeviceID` 不在该节点的在线表中
- **THEN** 平台不落表、不回应，并记一条 warn 日志（含 `node_id` 与 device id）
- **AND** 该节点在线表不受影响

#### 场景：无法解析或非心跳的 MESSAGE 被忽略

- **WHEN** `MESSAGE` 的 body 不是合法 MANSCDP、缺 `CmdType`/`DeviceID`，或 `CmdType` 既非 `Keepalive` 也非 `Catalog`
- **THEN** 平台记一条 debug 日志后丢弃该消息，不回应、不改表、不因解析失败而中断受理循环

### 需求：platform-large 节点踢掉注册过期的设备

正在服务的 platform-large 节点必须周期性移除每条"授权有效期已过且未再注册"的在线记录，并记录
每次踢线的 node id 与 device id。清扫必须随节点的服务 goroutine 结束而结束，不得在进程关闭后残留。

平台必须周期性地把"授权有效期已过且未再注册"的下级移出在线表，否则拔了网线的设备会永远显示在线。

#### 场景：有效期过期且未刷新即被踢下线

- **WHEN** 平台节点在服务，且某条在线记录的 `expires_at` 已早于当前时间
- **THEN** 清扫时该记录被移出在线表
- **AND** 记一条 info 日志（含 `node_id`、device id、已过期时长），不含任何凭据
- **AND** 后续 `GET /v1/nodes/{id}/devices` 不再列出该设备

#### 场景：到期前重注册的设备继续保留

- **WHEN** 某记录在 `expires_at` 之前收到一次成功的 REGISTER
- **THEN** 清扫不移除它，且其 `expires_at` 按新授予的有效期顺延

#### 场景：心跳刷新不构成续期

- **WHEN** 某记录仅通过心跳刷新过 `last_seen_at`，但从未在有效期内重注册
- **THEN** 到 `expires_at` 时它仍被移出在线表

#### 场景：平台停止后清扫即停止

- **WHEN** 平台节点被停止，或服务进程关闭
- **THEN** 该节点的清扫 goroutine 随之结束，不再改动在线表
- **AND** 进程退出时不残留清扫 goroutine

### 需求：platform-large 节点用在线设备表应答 MANSCDP 目录查询

收到 MANSCDP `CmdType` 为 `Catalog` 且 `DeviceID` 指向节点自身的 `MESSAGE` 时，platform-large 节点
必须回 `200 OK`，body 为从该节点自身在线设备表构建的 `Catalog` 响应。响应必须回显查询的 `SN`，
按 device id 升序列出行，空表时报告 `SumNum = 0` 且 `DeviceList` 为空。

上级平台通过 `MESSAGE` 下发 `CmdType = Catalog` 的目录查询；平台必须以自己的在线设备表作答。

#### 场景：用在线设备应答目录查询

- **WHEN** 平台收到 `CmdType = Catalog` 的查询，`DeviceID` 指向该平台节点自身
- **THEN** 平台回一条 `200 OK`，body 为 MANSCDP `Catalog` 响应
- **AND** 响应的 `SN` 与查询的 `SN` 一致，便于查询方匹配事务
- **AND** `DeviceList` 由该节点当前在线设备表逐条生成，按 device id 升序，`SumNum` 等于条目数

#### 场景：空表应答

- **WHEN** 查询到达时该节点没有任何在线设备
- **THEN** 仍回 `200 OK`，body 为 `SumNum = 0` 且 `DeviceList` 为空的 `Catalog` 响应（不是错误、不是空 body）

#### 场景：目录条目的字段

- **WHEN** 一条在线记录被渲染为目录条目
- **THEN** 至少包含 `DeviceID`、`Name`、`Manufacturer`、`Model`、`Status`（在册即 `ON`）、`CivilCode`
  （取自 device id 前 6 位行政区域码）
- **AND** 条目不含地址之外的任何凭据信息

#### 场景：无法解析的目录查询被忽略

- **WHEN** `CmdType = Catalog` 但 body 无法解析或缺少 `SN`
- **THEN** 平台记一条 warn 日志后丢弃，不回应

#### 场景：两个平台的目录互不相见

- **WHEN** 两个 platform-large 节点各自有在线设备，且都收到目录查询
- **THEN** 每个节点只把自己表里的设备渲染进响应

### 需求：MANSCDP 编解码留在 adapter 端口后

MANSCDP 解码与渲染必须留在 `port.MANSCDPCodec` 后的 adapter 中：app 层只能看到领域值与渲染好的
字符串，且 MUST NOT 导入 `internal/adapter`。

MANSCDP 是 XML 协议细节，必须留在 adapter 层，用例层只看到领域值与渲染好的字符串。

#### 场景：解码与渲染都经端口

- **WHEN** 用例层需要理解或生成 MANSCDP 内容
- **THEN** 它调用 `port.MANSCDPCodec` 的 `DecodeNotify`（收）与 `MarshalCatalog`（发），且 `internal/app` 不 import `internal/adapter`

#### 场景：XML 变化有 golden 覆盖

- **WHEN** MANSCDP 目录响应的字节序列发生变化
- **THEN** `internal/adapter/manscdp` 的 golden 测试失败，从而把改动暴露在适配层而非联调阶段

#### 场景：解码失败是错误而非恐慌

- **WHEN** 收到畸形的 MANSCDP body
- **THEN** 解码返回一个 error（不 panic），由调用方决定忽略

# Spec Delta

## ADDED Requirements

### Requirement: Platform-large node accepts downstream REGISTER as a UAS

A platform-large node SHALL act as a UAS on its own transport: on receiving a REGISTER it SHALL
answer a challenge first, verify the credentials the downstream sends back, and only then grant the
registration with 200 OK. The granted response MUST carry the negotiated `Expires`, echo the
downstream's `Contact`, and copy the transaction identifiers (`Via`, `From`, `To` with a tag,
`Call-ID`, `CSeq`) from the request, so the downstream can match the answer to its transaction.

#### Scenario: 首次 REGISTER 触发 401 挑战

- **WHEN** 平台收到一条不含 `Authorization` 的 REGISTER
- **THEN** 回 401，带 `WWW-Authenticate: Digest realm="<realm>", nonce="...", qop="auth", algorithm=MD5`
- **AND** 响应回抄请求的 `Via` / `From` / `To`（带 tag）/ `Call-ID` / `CSeq`；不修改在线设备表

#### Scenario: 挑战每次都是新的 nonce

- **WHEN** 同一或不同下级连续两次收到挑战
- **THEN** 两次的 `nonce` 不同；挑战不依赖任何请求上下文（无状态）

#### Scenario: 凭据正确后回 200 OK 并入表

- **WHEN** 下级以 `Authorization` 重发 REGISTER，且 username 在平台 `accounts` 内、response 校验通过
- **THEN** 回 200 OK，`Expires` 为协商后的有效期，`Contact` 与请求一致，并带 `Date`
- **AND** 该下级被记入在线设备表（deviceID、来源地址、Contact、传输、注册时刻、授予有效期、最后活跃时刻）

#### Scenario: 重注册刷新同一条记录

- **WHEN** 一个已在表中的下级再次成功注册
- **THEN** 在线表里仍只有一条该 deviceID 的记录，且其注册时刻、授予有效期与最后活跃时刻被刷新

#### Scenario: 非 REGISTER 报文被忽略

- **WHEN** 平台收到非 REGISTER 的 SIP 报文（如 MESSAGE 心跳）
- **THEN** 记一条 debug 日志后丢弃，不影响在线设备表
- **AND** 受理循环继续运行（本 capability 不消费心跳；心跳接收与踢线属于后续 change）

### Requirement: Platform-large node authenticates downstreams against configured accounts

The system SHALL authenticate a downstream only against the accounts declared on the platform
node. Missing or unparseable credentials MUST be re-challenged with 401; an unknown username, or a
well-formed `Authorization` whose response does not match, MUST be answered 403 without a second
challenge (GB/T 28181 §L.2). Passwords MUST NOT appear in logs, error bodies, or HTTP responses.

#### Scenario: Authorization 无法解析时重新挑战

- **WHEN** 收到的 REGISTER 带 `Authorization`，但头值无法解析（缺字段 / 非 Digest / 语法错）
- **THEN** 回 401 并给出新挑战；不计入拒绝，不影响在线设备表

#### Scenario: 未知 username 回 403

- **WHEN** `Authorization` 的 username 不在平台 `accounts` 内
- **THEN** 回 403，**不再下发新挑战**；在线设备表不变；日志以 username（不含密码）记录一次拒绝

#### Scenario: response 校验失败回 403

- **WHEN** username 已知且 `Authorization` 可解析，但 response 与按存储密码计算的期望值不符
- **THEN** 回 403，不下发新挑战；在线设备表不变

#### Scenario: 鉴权失败不影响既有在线记录

- **WHEN** 一个已在表中的下级后续注册鉴权失败
- **THEN** 表中既有记录保持不变（本 capability 不因单次失败踢线）

### Requirement: Platform-large node negotiates the registration lifetime it grants

The system SHALL clamp the `Expires` a downstream requests into the platform's configured
`min_expires` .. `max_expires` window, falling back to `default_expires` when the request carries
none. `Expires: 0` SHALL be honoured as an explicit unregistration: answered 200 OK and removed
from the online device table.

#### Scenario: 缺省有效期

- **WHEN** REGISTER 未带 `Expires`（或带 0 以外但平台未配置任何档位）
- **THEN** 授予 `default_expires`（默认 3600 秒）并在 200 OK 中给出

#### Scenario: 请求值被钳制到窗口内

- **WHEN** 请求 `Expires` 小于 `min_expires` 或大于 `max_expires`
- **THEN** 授予值分别为 `min_expires` / `max_expires`；不会出现窗口外的授予值

#### Scenario: 请求值在窗口内时原样授予

- **WHEN** 请求 `Expires` 落在窗口内
- **THEN** 授予值与请求值相同

#### Scenario: Expires: 0 视为注销

- **WHEN** 一条通过鉴权的 REGISTER 带 `Expires: 0`
- **THEN** 回 200 OK（含 `Expires: 0`）并把该 deviceID 从在线设备表移除；不报错、不改节点状态

### Requirement: Platform-large node keeps an online device table per node

The system SHALL maintain, per platform-large node, a table of the downstreams currently granted:
device id, source address, `Contact`, transport, protocol version, granted expiry, registration
time and last-seen time. The table is confined to the node that accepted the registration, is
cleared when that node stops, and MUST NOT be visible from or writable by another node.

#### Scenario: 受理成功后出现在表中

- **WHEN** 一个下级成功注册到某 platform-large 节点
- **THEN** 该节点的在线设备表含这条记录，字段与受理结果一致；其他节点的表不受影响

#### Scenario: 两个平台节点各自记账

- **WHEN** 两个 platform-large 节点各受理一个下级
- **THEN** 每个节点只看到自己的下级；停止其中一个不影响另一个的表

#### Scenario: 节点停止后表被清空

- **WHEN** 一个 platform-large 节点被停止
- **THEN** 其在线设备表被清空，该节点不再受理任何 REGISTER，监听端口被释放

### Requirement: Platform serving starts and stops with the node

A platform-large node SHALL begin serving when it is started — advancing `registering → online`
to mean "the platform is serving" — and stop serving before its listener is released, whether it
is stopped, faults, or the process shuts down. No serving goroutine may outlive its node or the
process.

#### Scenario: 启动即进入服务状态

- **WHEN** 启动一个 platform-large 节点
- **THEN** 节点推进到 `online`，并在其 transport 上受理 REGISTER

#### Scenario: 停止先停受理再释放端口

- **WHEN** 停止一个正在服务的 platform-large 节点
- **THEN** 受理 goroutine 在监听端口释放前退出；停止后不再发出任何响应

#### Scenario: 进程关闭时无残留 goroutine

- **WHEN** 模拟器进程开始关闭
- **THEN** 所有 platform-large 节点的受理 goroutine 有序退出，在线设备表随之释放

### Requirement: Platform serving is declarative, optional and validated

The system SHALL accept an optional `platform:` section on a `nodes:` entry — `realm`, `accounts`
(username/password pairs) and the `min_expires` / `default_expires` / `max_expires` window. All
keys are optional with documented defaults (realm defaults to the node's domain, expires window
defaults to 60/3600/86400 seconds). Invalid values MUST fail configuration loading with an error
naming the entry index and the offending field, rather than being ignored.

#### Scenario: 声明的 accounts 与 realm 生效

- **WHEN** 条目声明 `platform.realm` 与两条 accounts
- **THEN** 只有这两个 username 能通过鉴权，挑战中的 realm 与配置一致

#### Scenario: 省略 platform 段时取缺省

- **WHEN** 一个 platform-large 条目未声明 `platform:` 段
- **THEN** realm 取该节点的 domain，有效期窗口取 60 / 3600 / 86400 秒；节点仍会受理注册
  （但没有声明任何 account 时，所有 username 都会被拒绝）

#### Scenario: 非法 platform 配置导致加载失败

- **WHEN** `platform.accounts` 出现重复 username 或空密码、`realm` 为空，或有效期三档不满足
  `0 < min ≤ default ≤ max`
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动；错误信息不含密码明文

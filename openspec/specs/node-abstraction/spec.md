# node-abstraction 规范

## Purpose

定义 GB28181 仿真的"节点"抽象：节点身份值对象、生命周期状态机、进程内多节点注册与共存，
以及对外暴露的节点清单与单节点启停接口。本 capability 只提供节点**骨架**，三种具体身份
（device / platform-large / platform-small）的协议行为由后续 change 填充。

## Requirements

### Requirement: Node identity is a validated immutable value object


系统 MUST 提供不可变的 `NodeID` 值对象，放在 `internal/domain/model/` 下，
表示 GB/T 28181 的 20 位编码，并提供 `NodeKind` 枚举
（`device` / `platform-large` / `platform-small`）与承载节点
信令地址、归属域和厂商信息的 `NodeProfile`。构造函数 MUST 拒绝非法输入而非
返回部分初始化的值；已构造的身份MUST NOT 被调用方修改。

#### Scenario: 合法 20 位编码构造成功

- **WHEN** 以 20 位数字编码（中心编码 8 位 + 行业编码 2 位 + 类型编码 3 位 + 网络标识 1 位 + 序号 6 位）构造 `NodeID`
- **THEN** 构造成功；`String()` 返回原编码；`Kind()` 按类型编码段返回对应 `NodeKind`

#### Scenario: 非法编码被拒绝

- **WHEN** 以长度不等于 20、含非数字字符、或类型编码段不匹配的输入构造 `NodeID`
- **THEN** 构造返回错误，错误信息包含"非法"原因与输入长度；不 panic

#### Scenario: 身份不可变

- **WHEN** 持有 `NodeProfile` 的调用方尝试修改其字段
- **THEN** 编译失败或运行时 panic（与既有 `model.Message` / `model.Credentials` 的不可变性约定一致）；
  修改需通过显式的 `With...` 方法返回新值

### Requirement: Node lifecycle is an explicit state machine


系统 MUST 定义节点状态枚举，包含状态 `Idle`、`Registering`、
`Registered`、`Online`、`Offline`、`Fault`，以及位于
`internal/domain/model/` 的显式合法迁移表。表中不存在的迁移 MUST 返回哨兵错误
`ErrIllegalTransition` 并保持状态不变；MUST NOT panic，MUST NOT 静默成功。

#### Scenario: 合法迁移推进状态

- **WHEN** 节点处于 `Idle` 并调用启动
- **THEN** 状态迁移为 `Registering`；注册成功后依次可迁移至 `Registered` 与 `Online`

#### Scenario: 非法迁移被拒绝且状态不变

- **WHEN** 节点处于 `Idle` 时请求迁移到 `Online`
- **THEN** 返回 `ErrIllegalTransition`（`errors.Is` 可判定）；节点状态仍为 `Idle`；不 panic

#### Scenario: Fault 为可恢复终态

- **WHEN** 节点进入 `Fault`
- **THEN** 仅允许迁移回 `Idle`（重置）或 `Offline`（摘除）；迁移到 `Online` 返回 `ErrIllegalTransition`

### Requirement: Multiple nodes coexist in one process with isolated lifecycles


系统 MUST 维护进程级节点注册表（端口 `NodeRegistry`，位于 `internal/domain/port/`），同时持有多个节点。每个节点 MUST 拥有独立的传输监听器、独立生命周期，以及以节点 id 为键的独立日志字段。启动或停止某个节点 MUST NOT 影响其他节点，且注册表 MUST 是并发安全的。

节点后台资源的节点级归属（2026-09-30 fix-concurrency-lifecycles 新增的不变量）：节点为其对话框（含 INVITE 过期看门狗）发起的所有后台 goroutine MUST 在以下任一条件发生时终止——(a) 定时器自然到期；(b) 对话框被确认或拆除；(c) 所属节点被停止。看门狗注册表 MUST 以节点实例为作用域而非进程级全局，同一进程内的两个节点实例 MUST NOT 互相观测到对方的计时器。节点停止后 MUST 不残留任何看门狗 goroutine。

#### Scenario: 两个节点各自绑定独立端口并共存

- **WHEN** 在同一进程内注册 `bind=127.0.0.1:5060` 与 `bind=127.0.0.1:5061` 的两个节点并同时启动
- **THEN** 两者均启动成功并各自持有独立 列出ener（本 capability 交付的状态为 `Registering`，
  `Online` 需由身份实现推进，见 design D9）；各自 列出ener 独立收发互不串扰；
  `registry.列出()` 返回 2 个节点

#### Scenario: 停止单个节点不影响其他节点

- **WHEN** 停止其中一个节点
- **THEN** 该节点状态变为 `Offline` 且其端口被释放（可立即重新绑定）；
  另一节点的状态与 列出ener 均不受影响，可继续收发

#### Scenario: 注册表并发安全

- **WHEN** 多个 goroutine 并发注册/注销/查找节点
- **THEN** 在 `-race` 下无数据竞争；无重复 id；查找结果与实际注册一致

#### Scenario: 节点停止取消所有 pending 看门狗（fix-concurrency-lifecycles 新增）

- **WHEN** 一个 platform-large 节点有 50 个 pending INVITE 对话在等 ACK，节点被停止
- **THEN** 全部 50 个看门狗 goroutine 退出，均不记录 "expired" 日志、不触发对话拆除

#### Scenario: 看门狗注册表按节点隔离（fix-concurrency-lifecycles 新增）

- **WHEN** 同一进程内两个 platform-large 节点各有一个 Call-ID 相同的 pending INVITE
- **THEN** 确认其中一个节点的对话不会取消另一节点的看门狗，各自计时器独立到期

#### Scenario: 已确认对话及时释放看门狗（fix-concurrency-lifecycles 新增）

- **WHEN** 一个 pending INVITE 的 ACK 在 30s 到期前到达
- **THEN** 看门狗 goroutine 立即退出（不等定时器触发），且计时器被停止

#### Scenario: 重复 id 注册被拒绝

- **WHEN** 以已存在的 id 再次注册
- **THEN** 返回错误且不覆盖既有节点

### Requirement: Node lifecycle use cases are orchestrated in the app layer


系统 MUST 提供放在 `internal/app/` 下的 `NodeService`，该服务仅依赖 domain ports（`NodeRegistry`、`NodeLifecycle`、`SIPTransport`、`Clock`、`AuditSink`），并暴露用于创建、启动、停止和查询节点的自解释方法。该服务 MUST 不得通过端口之外的路径导入 adapter 包。

#### Scenario: 创建并启动节点

- **WHEN** 以合法 `NodeProfile` 调用创建并随后启动
- **THEN** 节点进入注册表并经历 `Idle → Registering`；启动结果通过返回值表达，不吞错

#### Scenario: 启动失败不留下半启动节点

- **WHEN** 启动过程中 transport 绑定失败（端口被占用）
- **THEN** 返回错误且错误含失败原因；节点状态回到 `Idle`（首次启动失败）或 `Fault`
  （运行中断连后重启失败）；端口未被占用；节点不处于"半启动"的中间态

#### Scenario: app 层不依赖 adapter

- **WHEN** 执行 `go list -deps ./internal/app/...` 并筛去标准库
- **THEN** 结果仅含 `internal/domain/...` 与 `internal/platform/...`，不含 `internal/adapter/...`

### Requirement: Node configuration is declarative and backward compatible


系统 MUST 接受可选的 `nodes:` 列表，在 YAML 配置中每个条目至少包含
`id`、`kind`、`domain` 和 `addr`。条目可以额外携带可选的注册段，包含上游平台地址、
鉴权用户名与密码、请求的 `expires` 和 `transport`；省略该段保持节点未注册状态。条目
还可以携带可选的 platform 段，包含对下游挑战的 realm、接受的账号，以及授予的
有效期窗口；省略它以记录的默认值使节点提供服务（见 `platform-large-node`）。省略整个
`nodes:` 段 MUST 保持当前行为（进程以零节点启动）；id 非法、kind 未知或注册/
platform 值非法的条目 MUST 使配置加载失败并返回可操作的错误，而非被静默跳过。

#### Scenario: 声明两个节点后被加载

- **WHEN** 配置含两个 `nodes:` 条目且均合法
- **THEN** 启动时按配置注册两个节点；`GET /v1/nodes` 返回这两个节点

#### Scenario: 缺省配置行为不变

- **WHEN** 配置不含 `nodes:` 段
- **THEN** 进程以零节点启动；HTTP 服务与日志行为与引入本 capability 之前完全一致

#### Scenario: 非法节点配置导致加载失败

- **WHEN** 某条目 `id` 长度不为 20 或 `kind` 不在枚举内
- **THEN** 配置加载返回错误并指出具体条目的序号与字段；进程不启动

#### Scenario: 条目携带注册参数后被加载并生效

- **WHEN** 某条目除 `id` / `kind` / `domain` / `addr` 外还声明了平台地址、鉴权用户名与密码、`expires`、`transport`
- **THEN** 配置加载成功，节点启动后按该注册参数完成注册（见 `device-node` capability）；缺省的 `expires` 取 3600 秒、`transport` 取 `udp`

#### Scenario: 非法注册参数导致加载失败

- **WHEN** 某条目的平台地址缺少端口、`expires` 非正数、`transport` 不在 `udp` / `tcp` 内，或给出平台地址却未提供密码
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动；错误体不含密码明文

#### Scenario: 条目携带 platform 段后被加载并生效

- **WHEN** 某 platform-large 条目声明了 `platform.realm`、若干 `accounts` 与有效期三档
- **THEN** 配置加载成功，节点启动后按该配置受理下级注册（见 `platform-large-node` capability）；缺省的 realm 取该节点的 domain，有效期窗口取 60 / 3600 / 86400 秒

#### Scenario: 非法 platform 参数导致加载失败

- **WHEN** 某条目的 `platform.realm` 为空、`accounts` 含重复 username 或空密码，或有效期三档不满足 `0 < min ≤ default ≤ max`
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动；错误体不含密码明文

### Requirement: HTTP API exposes node inventory and per-node control


系统 MUST 暴露 JSON 端点，在现有的 `/v1` 前缀下：
`GET /v1/nodes`（列出）、`GET /v1/nodes/{id}`（详情）、
`POST /v1/nodes/{id}/start`、`POST /v1/nodes/{id}/stop`
以及 `POST /v1/nodes/{id}/unregister`，用于单节点控制；
此外还包括 `GET /v1/nodes/{id}/devices`
与 `GET /v1/nodes/{id}/devices/{deviceID}` 用于读取 platform-large 节点的在线设备表。
未知 id MUST 返回 HTTP 404 及 JSON 错误体；非法迁移 MUST 返回 HTTP 409。
携带注册配置的节点启动时会额外执行注册事务：注册成功使节点进入 `online`，失败则返回
non-2xx 响应描述失败阶段并使节点进入 `fault`。没有注册配置的 device 节点启动后直接
进入 `online`（无上游注册事务可等待）；platform-large 节点同样前进到 `online`，因为服务是其注册行为。
注销在线节点会发送 `Expires: 0`，成功则使其进入 `offline`；注销失败则返回 non-2xx 响应
命名失败阶段并使节点保持 `online`，注销非在线节点为非法迁移（HTTP 409）。
节点响应 MUST 附带 `has_registration` 布尔字段，反映该节点是否配置了上游注册段。

#### Scenario: 列出节点

- **WHEN** 注册表含 N 个节点并调用 `GET /v1/nodes`
- **THEN** 返回 HTTP 200 与 JSON 数组，每项含 `id`、`kind`、`status`、`addr` 字段

#### Scenario: 查询未知节点

- **WHEN** 调用 `GET /v1/nodes/{id}` 且 id 未注册
- **THEN** 返回 HTTP 404 与 JSON 错误体（含 `error` 字段）

#### Scenario: 非法状态迁移返回 409

- **WHEN** 对已经处于 `online` 的节点调用 `/start`
- **THEN** 返回 HTTP 409，响应体标明当前状态与请求动作冲突；节点状态不变

#### Scenario: 单节点启停

- **WHEN** 对 `Offline` 且未配置注册的 device 节点调用 `/start`，随后调用 `/stop`
- **THEN** 两次请求均返回 HTTP 200；`/start` 后状态为 `online`（无上游注册时不进入 `registering`），`/stop` 后状态为 `offline`

#### Scenario: 启动即注册成功

- **WHEN** 对 `Offline` 且配置了注册的 device 节点调用 `/start`，且平台侧完成 401 挑战并返回 200 OK
- **THEN** 返回 HTTP 200，`/start` 后状态为 `online`；响应与后续 `GET /v1/nodes/{id}` 均体现该状态

#### Scenario: 启动即注册失败

- **WHEN** 对 `Offline` 且配置了注册的 device 节点调用 `/start`，但注册事务失败（超时 / 认证被拒 / 平台返回 5xx）
- **THEN** 返回非 2xx 状态码与 JSON 错误体（含失败阶段与原因，不含凭据明文）；节点状态为 `fault`；其监听端口已被释放

#### Scenario: 注销在线节点

- **WHEN** 对 online 且已注册的 device 节点调用 `/unregister`，平台对 `Expires: 0` 返回 200 OK
- **THEN** 返回 HTTP 200，节点状态为 `offline`，监听端口被释放

#### Scenario: 注销失败返回失败阶段

- **WHEN** 对 online 节点调用 `/unregister`，但注销事务失败
- **THEN** 返回非 2xx 与 JSON 错误体（含失败阶段）；节点保持 `online`

#### Scenario: 注销未在线节点返回 409

- **WHEN** 对未在线（如 `offline`）的节点调用 `/unregister`
- **THEN** 返回 HTTP 409 与 JSON 错误体；不发出任何报文

#### Scenario: 启动 platform-large 节点后进入服务状态

- **WHEN** 对 `Offline` 的 platform-large 节点调用 `/start`
- **THEN** 返回 HTTP 200，状态为 `online`；该节点开始在其地址上受理 REGISTER

#### Scenario: 查询平台节点的在线设备

- **WHEN** 一个 platform-large 节点已受理若干下级，调用 `GET /v1/nodes/{id}/devices`
- **THEN** 返回 HTTP 200 与 JSON 数组，每项含 `device_id`、`addr`、`expires`、`registered_at` 等字段，按 `device_id` 升序

#### Scenario: 查询未知或离线设备

- **WHEN** 调用 `GET /v1/nodes/{id}/devices/{deviceID}` 但节点未知、或该 deviceID 不在其在线表中
- **THEN** 返回 HTTP 404 与 JSON 错误体（含 `error` 字段）

### Requirement: 无上游注册的设备节点启动后直接在线

`device` 身份的节点在不携带 `registration:` 配置段时，其 `Start()` 操作 MUST 在绑定信令监听器成功后将节点推进到 `online` 状态——设备此时仅提供被动的 UAS 服务（接收 INVITE、订阅等），没有上游注册事务需要执行，MUST NOT 停留在 `registering` 状态。携带 `registration:` 配置段的 `device` 节点行为 MUST 保持不变：启动后执行注册事务，成功进入 `online`、失败进入 `fault`。

#### Scenario: 无 registration 的设备节点启动

- **WHEN** 一个 `device` 节点的配置不含 `registration:` 段，用户对该节点执行启动
- **THEN** 信令监听器绑定成功后节点状态为 `online`，节点可接收下级 INVITE 等请求

#### Scenario: 有 registration 的设备节点行为不变

- **WHEN** 一个 `device` 节点的配置含 `registration:` 段，用户对该节点执行启动且上级可达
- **THEN** 节点执行注册事务并在成功后进入 `online`，与既有行为一致

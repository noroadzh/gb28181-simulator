# node-abstraction Specification

## Purpose

定义 GB28181 仿真的"节点"抽象：节点身份值对象、生命周期状态机、进程内多节点注册与共存，
以及对外暴露的节点清单与单节点启停接口。本 capability 只提供节点**骨架**，三种具体身份
（device / platform-large / platform-small）的协议行为由后续 change 填充。

## Requirements

### Requirement: Node identity is a validated immutable value object

The system SHALL provide an immutable `NodeID` value object under `internal/domain/model/`
representing a GB/T 28181 20-digit identifier, together with a `NodeKind` enumeration
(`device` / `platform-large` / `platform-small`) and a `NodeProfile` carrying the node's
signalling address, home domain and vendor. Constructors MUST reject malformed input rather
than returning a partially initialised value, and a constructed identity MUST NOT be mutable
by callers.

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

The system SHALL define a node status enumeration with the states `Idle`, `Registering`,
`Registered`, `Online`, `Offline`, `Fault`, plus an explicit legal-transition table under
`internal/domain/model/`. A transition not present in the table MUST return the sentinel error
`ErrIllegalTransition` and leave the status unchanged; it MUST NOT panic and MUST NOT silently
succeed.

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

The system SHALL maintain a process-wide node registry (port `NodeRegistry` in
`internal/domain/port/`) holding many nodes simultaneously. Each node MUST own an independent
transport listener, an independent lifecycle, and independent log fields keyed by its node id.
Starting or stopping one node MUST NOT affect any other node, and the registry MUST be safe for
concurrent use.

#### Scenario: 两个节点各自绑定独立端口并共存

- **WHEN** 在同一进程内注册 `bind=127.0.0.1:5060` 与 `bind=127.0.0.1:5061` 的两个节点并同时启动
- **THEN** 两者均启动成功并各自持有独立 listener（本 capability 交付的状态为 `Registering`，
  `Online` 需由身份实现推进，见 design D9）；各自 listener 独立收发互不串扰；
  `registry.List()` 返回 2 个节点

#### Scenario: 停止单个节点不影响其他节点

- **WHEN** 停止其中一个节点
- **THEN** 该节点状态变为 `Offline` 且其端口被释放（可立即重新绑定）；
  另一节点的状态与 listener 均不受影响，可继续收发

#### Scenario: 注册表并发安全

- **WHEN** 多个 goroutine 并发注册/注销/查找节点
- **THEN** 在 `-race` 下无数据竞争；无重复 id；查找结果与实际注册一致

#### Scenario: 重复 id 注册被拒绝

- **WHEN** 以已存在的 id 再次注册
- **THEN** 返回错误且不覆盖既有节点

### Requirement: Node lifecycle use cases are orchestrated in the app layer

The system SHALL provide a `NodeService` under `internal/app/` that depends only on domain
ports (`NodeRegistry`, `NodeLifecycle`, `SIPTransport`, `Clock`, `AuditSink`) and exposes
intent-revealing methods for creating, starting, stopping and querying nodes. The service MUST
NOT import adapter packages other than through ports.

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

The system SHALL accept an optional `nodes:` list in the YAML configuration, where each entry
carries at least `id`, `kind`, `domain` and `addr`. An entry MAY additionally carry an optional
registration section with the upstream platform address, authentication username and password,
requested `expires` and `transport`; omitting that section keeps the node unregistered. Omitting
the whole `nodes:` section MUST preserve today's behaviour (process starts with zero nodes); an
entry with an invalid id, unknown kind, or invalid registration values MUST fail configuration
loading with an actionable error rather than being skipped silently.

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

### Requirement: HTTP API exposes node inventory and per-node control

The system SHALL expose JSON endpoints under the existing `/v1` prefix: `GET /v1/nodes`
(list), `GET /v1/nodes/{id}` (detail), and `POST /v1/nodes/{id}/start` and
`POST /v1/nodes/{id}/stop` for per-node control. Unknown ids MUST return HTTP 404 with a JSON
error body; illegal transitions MUST return HTTP 409. Starting a node that carries a
registration configuration additionally performs its registration transaction: a successful
registration leaves the node `online`, while a failed one returns a non-2xx response describing
the failing stage and leaves the node `fault`. A node without a registration configuration stops
at `registering`, exactly as before.

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
- **THEN** 两次请求均返回 HTTP 200；`/start` 后状态为 `registering`，`/stop` 后状态为 `offline`

#### Scenario: 启动即注册成功

- **WHEN** 对 `Offline` 且配置了注册的 device 节点调用 `/start`，且平台侧完成 401 挑战并返回 200 OK
- **THEN** 返回 HTTP 200，`/start` 后状态为 `online`；响应与后续 `GET /v1/nodes/{id}` 均体现该状态

#### Scenario: 启动即注册失败

- **WHEN** 对 `Offline` 且配置了注册的 device 节点调用 `/start`，但注册事务失败（超时 / 认证被拒 / 平台返回 5xx）
- **THEN** 返回非 2xx 状态码与 JSON 错误体（含失败阶段与原因，不含凭据明文）；节点状态为 `fault`；其监听端口已被释放

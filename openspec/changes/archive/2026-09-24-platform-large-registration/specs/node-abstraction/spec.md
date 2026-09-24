# Spec Delta

## MODIFIED Requirements

### Requirement: Node configuration is declarative and backward compatible

The system SHALL accept an optional `nodes:` list in the YAML configuration, where each entry
carries at least `id`, `kind`, `domain` and `addr`. An entry MAY additionally carry an optional
registration section with the upstream platform address, authentication username and password,
requested `expires` and `transport`; omitting that section keeps the node unregistered. An entry
MAY also carry an optional platform section with the realm it challenges downstreams in, the
accounts it accepts, and the expires window it grants; omitting it leaves the node serving with
documented defaults (see `platform-large-node`). Omitting the whole `nodes:` section MUST preserve
today's behaviour (process starts with zero nodes); an entry with an invalid id, unknown kind, or
invalid registration or platform values MUST fail configuration loading with an actionable error
rather than being skipped silently.

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

The system SHALL expose JSON endpoints under the existing `/v1` prefix: `GET /v1/nodes`
(list), `GET /v1/nodes/{id}` (detail), `POST /v1/nodes/{id}/start`, `POST /v1/nodes/{id}/stop`
and `POST /v1/nodes/{id}/unregister` for per-node control, plus `GET /v1/nodes/{id}/devices`
and `GET /v1/nodes/{id}/devices/{deviceID}` to read the online device table of a platform-large
node. Unknown ids MUST return HTTP 404 with a JSON error body; illegal transitions MUST return
HTTP 409. Starting a node that carries a registration configuration additionally performs its
registration transaction: a successful registration leaves the node `online`, while a failed one
returns a non-2xx response describing the failing stage and leaves the node `fault`. A node
without a registration configuration stops at `registering`, exactly as before — except a
platform-large node, which advances to `online` because serving is its registered behaviour.
Unregistering a node that is online sends `Expires: 0` and leaves it `offline` on success; a
failed unregistration returns a non-2xx response naming the failing stage and leaves the node
`online`, and unregistering a node that is not online is an illegal transition (HTTP 409).

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

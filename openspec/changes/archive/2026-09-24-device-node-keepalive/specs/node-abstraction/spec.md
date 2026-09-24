# Spec Delta

## MODIFIED Requirements

### Requirement: HTTP API exposes node inventory and per-node control

The system SHALL expose JSON endpoints under the existing `/v1` prefix: `GET /v1/nodes`
(list), `GET /v1/nodes/{id}` (detail), `POST /v1/nodes/{id}/start`, `POST /v1/nodes/{id}/stop`
and `POST /v1/nodes/{id}/unregister` for per-node control. Unknown ids MUST return HTTP 404 with
a JSON error body; illegal transitions MUST return HTTP 409. Starting a node that carries a
registration configuration additionally performs its registration transaction: a successful
registration leaves the node `online`, while a failed one returns a non-2xx response describing
the failing stage and leaves the node `fault`. A node without a registration configuration stops
at `registering`, exactly as before. Unregistering a node that is online sends `Expires: 0` and
leaves it `offline` on success; a failed unregistration returns a non-2xx response naming the
failing stage and leaves the node `online`, and unregistering a node that is not online is an
illegal transition (HTTP 409).

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

- **WHEN** 对一个 `online` 的 device 节点调用 `/unregister`，且平台对 `Expires: 0` 的 REGISTER 返回 200 OK
- **THEN** 返回 HTTP 200，响应体状态为 `offline`；该节点的心跳与重注册均已停止

#### Scenario: 注销失败返回失败阶段

- **WHEN** 对一个 `online` 的 device 节点调用 `/unregister`，但注销事务失败（超时 / 5xx）
- **THEN** 返回非 2xx 状态码与 JSON 错误体（含失败阶段，不含凭据明文）；节点仍为 `online`

#### Scenario: 注销未在线节点返回 409

- **WHEN** 对一个 `offline` 的节点调用 `/unregister`
- **THEN** 返回 HTTP 409 与 JSON 错误体（标明当前状态），不发出任何报文

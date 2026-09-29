# Spec Delta

## MODIFIED Requirements

### Requirement: 不支持方法的应答可配置

节点可配置为对不支持的请求方法回 `501 Not Implemented`。**当节点未配置 `UnsupportedMethod` 或该配置缺失时**，节点必须（MUST）对不支持的请求方法回 `405 Method Not Allowed`（不再静默丢弃），并在 `Allow` 头中列出该节点支持的方法集合（`REGISTER, MESSAGE, INVITE, ACK, BYE, OPTIONS, SUBSCRIBE, INFO`）。`UnsupportedMethod` 显式配置时，按配置的状态码作答并回显事务头。以下两个原有场景按新默认行为更新保留。

#### Scenario: 未知方法配置 501

- **WHEN** 节点被配置为 `UnsupportedMethod: 501`，且一个方法为 FOO 的请求到达
- **THEN** 节点回 `501 Not Implemented`，并回显请求的事务头

#### Scenario: 默认保持静默丢弃

- **WHEN** 节点没有任何故障配置，且一个方法为 FOO 的请求到达
- **THEN** 节点回 `405 Method Not Allowed` 并携带 `Allow` 头——本场景自本变更起由"静默丢弃"更新为"显式 405"，原静默行为仅保留在故障 profile 黑洞（blackhole）动作中

#### Scenario: 故障 profile 中的 canned 响应覆盖 UnsupportedMethod

- **WHEN** 节点同时安装 `UnsupportedMethod: 501` 故障 profile 且 `Canned: {"FOO": 403}`
- **THEN** 对 `FOO` 请求回 `403 Forbidden`（canned 优先于 unsupported-method），故障计数计入 canned_response

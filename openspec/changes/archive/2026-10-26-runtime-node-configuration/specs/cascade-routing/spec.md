# Spec Delta

## ADDED Requirements

### Requirement: HTTP API 设置级联拓扑

系统 MUST 支持通过 HTTP API `PUT /v1/nodes/:id/cascade` 运行时设置节点的 `cascade_parent` 和 `cascade_children`，调用方在 HTTP 边界传入 ID 字符串，存储层将 `cascade_children` 序列化为 JSON 文本写入 SQLite。设置成功后 MUST 触发 `nodereg.Registry` 拓扑重算（`refreshCascadeTopologyLocked`），使后续 `X-RoutePath` 注入反映新拓扑。

#### Scenario: HTTP API 设置级联

- **WHEN** 节点 A 已存在，调用 `PUT /v1/nodes/A/cascade` 携带 `{"parent": "B", "children": ["C"]}`
- **THEN** DB `nodes` 表 A 行的 `cascade_parent='B'`、`cascade_children='["C"]'`；`Registry` 中 A 的 Profile 已更新为带 B 为父、C 为子

#### Scenario: 环路被拒绝

- **WHEN** 节点 X 的祖先链为 `X→Y→Z`，调用 `PUT /v1/nodes/Z/cascade` 携带 `{"parent": "X"}`
- **THEN** HTTP 409，DB 与 Registry 均未修改

### Requirement: 级联设置错误码

`PUT /v1/nodes/:id/cascade` 在以下情况下 MUST 返回相应错误：`{"error": "cascade would create cycle"}` 返回 409、`{"error": "node cannot be its own parent"}` 返回 400、`{"error": "parent node not found"}` 返回 404、`{"error": "child node not found"}` 返回 404。所有错误响应 MUST 含 `id` 字段标识操作的节点。

#### Scenario: 自引用被拒绝

- **WHEN** 调用 `PUT /v1/nodes/A/cascade` 携带 `{"parent": "A"}`
- **THEN** HTTP 400，响应体 `{"error": "node cannot be its own parent", "id": "A"}`

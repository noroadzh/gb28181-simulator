# Spec Delta

## Purpose

定义节点 CRUD、注册目标、平台账号、级联拓扑的 HTTP API 契约，让操作员通过 RESTful 接口管理节点定义并实时查看节点状态。

## ADDED Requirements

### Requirement: 列出节点

`GET /v1/nodes` MUST 返回所有节点的摘要信息数组。响应 JSON 数组每个元素包含：`id`、`kind`、`domain`、`addr`、`vendor`、`cascade_parent`、`cascade_children`、`registration`（子对象或 null）、`platform`（子对象或 null）、`status`（运行时状态字符串）、`created_at`、`updated_at`。请求支持查询参数 `kind`（可选，过滤节点种类）和 `domain`（可选，过滤域）。

#### Scenario: 列出所有节点

- **WHEN** DB 中存在 3 条节点记录，调用 `GET /v1/nodes`
- **THEN** HTTP 200，响应 JSON 数组长度为 3，每个元素含上述全部字段

#### Scenario: 按 kind 过滤

- **WHEN** DB 中存在 2 个 device 节点和 1 个 platform-large 节点，调用 `GET /v1/nodes?kind=device`
- **THEN** HTTP 200，响应 JSON 数组长度为 2，全部为 device 节点

### Requirement: 获取单个节点

`GET /v1/nodes/:id` MUST 返回指定 ID 节点的完整信息。若节点不存在 MUST 返回 HTTP 404，响应体 `{"error": "node not found", "id": "<id>"}`。

#### Scenario: 存在的节点

- **WHEN** 调用 `GET /v1/nodes/34020000001310000001`，节点存在
- **THEN** HTTP 200，响应 JSON 含 `id="34020000001310000001"` 及完整字段

#### Scenario: 不存在的节点

- **WHEN** 调用 `GET /v1/nodes/99999999999999999999`，节点不存在
- **THEN** HTTP 404，响应体含 `error="node not found"`

### Requirement: 创建节点

`POST /v1/nodes` MUST 接受 JSON 请求体创建新节点。请求体必含字段：`id`（20 位编码）、`kind`、`domain`、`addr`、`vendor`。可选字段：`registration`（子对象）、`platform`（子对象）、`cascade_parent`（字符串）、`cascade_children`（JSON 数组）。创建成功 MUST 写入 `nodes` 表及相关子表，返回 HTTP 201 和完整节点对象。

#### Scenario: 正常创建

- **WHEN** 提交请求体含合法节点字段，ID 在 DB 中不存在
- **THEN** HTTP 201，响应体含新节点完整信息；DB 中 `nodes` 表新增一行

#### Scenario: ID 已存在

- **WHEN** 提交请求体 ID 与已有节点重复
- **THEN** HTTP 409，响应体 `{"error": "node already exists", "id": "<id>"}`

#### Scenario: 字段校验失败

- **WHEN** 提交请求体 ID 长度不为 20 或含非数字字符
- **THEN** HTTP 400，响应体 `{"error": "invalid id", "field": "id", "reason": "<detail>"}`

#### Scenario: 端口被占用

- **WHEN** 提交请求体 `addr` 端口已被另一节点占用
- **THEN** HTTP 409，响应体 `{"error": "address already in use", "addr": "<addr>", "owner_id": "<id>"}`

### Requirement: 更新节点

`PUT /v1/nodes/:id` MUST 接受 JSON 请求体更新节点的 `addr`、`vendor`、`domain` 字段以及 `registration` / `platform` 子对象。`id` 和 `kind` 不可通过该接口修改。更新成功 MUST 写入 DB 并返回 HTTP 200 和更新后的节点对象。

#### Scenario: 正常更新

- **WHEN** 提交请求体修改 `addr` 字段，节点存在
- **THEN** HTTP 200，响应体含更新后的节点信息；DB 中该行 addr 字段已更新

#### Scenario: 试图修改 id

- **WHEN** 提交请求体包含 `id` 字段
- **THEN** HTTP 400，响应体 `{"error": "id is immutable"}`

### Requirement: 删除节点

`DELETE /v1/nodes/:id` MUST 先校验节点当前状态为 `idle` 或 `stopped`，再从 DB 删除该行及 `node_registrations` / `node_platforms` 相关记录，并从 `nodereg.Registry` 注销节点。

#### Scenario: 已停止的节点可删除

- **WHEN** 节点 ID `34020000001310000001` 当前状态为 `idle` 或 `stopped`
- **THEN** HTTP 200，响应体 `{"status": "deleted", "id": "<id>"}`；DB 中 `nodes` 表该行已删除；`nodereg.Registry` 中无该 ID

#### Scenario: 运行中节点拒绝删除

- **WHEN** 节点 ID `34020000001310000001` 当前状态为 `online` 或 `registering`
- **THEN** HTTP 409，响应体 `{"error": "node must be stopped first", "id": "<id>", "current_status": "online"}`

#### Scenario: 不存在的节点

- **WHEN** 调用 `DELETE /v1/nodes/<不存在 ID>`
- **THEN** HTTP 404

### Requirement: 设置级联拓扑

`PUT /v1/nodes/:id/cascade` MUST 接受 JSON 请求体 `{"parent": "<id>", "children": ["<id>", ...]}`。`parent` 字段若为空字符串表示取消上级；`children` 数组若为空数组表示无下级。设置成功 MUST 校验不形成环路，写入 DB，并触发 `nodereg.Registry` 拓扑重算。

#### Scenario: 设置级联父子关系

- **WHEN** 调用 `PUT /v1/nodes/34020000001310000002/cascade`，请求体 `{"parent": "34020000002000000001", "children": ["34020000001310000003"]}`
- **THEN** HTTP 200，响应体含更新后的节点；DB 中 `nodes` 表的 `cascade_parent` 与 `cascade_children` 已更新

#### Scenario: 形成环路

- **WHEN** 调用 `PUT /v1/nodes/<id>/cascade` 将 `<id>` 设为某下级的上级，下级又指向 `<id>` 的祖先
- **THEN** HTTP 409，响应体 `{"error": "cascade would create cycle", "path": [<id>, ..., <id>]}`

### Requirement: WebSocket 状态推送

`/ws/nodes` 端点 MUST 推送节点状态变更事件。事件格式为 JSON：`{"type": "node_status_changed", "id": "<id>", "status": "<status>", "at": "<ISO8601>"}`。当节点状态机推进时（idle → registering → online/fault 等），订阅端点 MUST 收到对应事件。

#### Scenario: 节点状态变化

- **WHEN** 节点从 `idle` 推进到 `online`
- **THEN** `/ws/nodes` 订阅客户端在 1 秒内收到 `node_status_changed` 事件

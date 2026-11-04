## ADDED Requirements

### Requirement: 实时拉流会话只读查询

系统 MUST 提供 `GET /v1/nodes/:id/channels/:ch/live`：若该通道存在活动实时拉流会话，返回 `200 {session_id, transport, local_port, ssrc?}`（字段语义与 POST live 成功体一致）；若无会话，返回 `404` 与 JSON 错误体。该端点 MUST NOT 创建或修改会话。

#### Scenario: 查询活动会话

- **WHEN** 平台已对某通道成功 POST live
- **THEN** GET live 返回 200 与同一会话的 transport、local_port

#### Scenario: 无会话

- **WHEN** 该通道没有活动拉流会话
- **THEN** GET live 返回 404

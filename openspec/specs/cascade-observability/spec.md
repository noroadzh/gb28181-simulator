# cascade-observability Specification

## Purpose

只读暴露平台级联媒体中继会话，便于联调排障，而不另建拆链控制面。

## Requirements

### Requirement: 级联媒体会话只读列表

系统 MUST 提供 `GET /v1/nodes/:id/cascade/sessions`，返回该节点当前活动的媒体中继会话；每项 MUST 含 `upstream_call_id`、`channel_id`、`peer`、`transport`、`local_port`、`started_at`（RFC3339，可空）。无会话时 MUST 返回 `200` 与空 `sessions` 数组。该端点 MUST NOT 创建或拆除会话。

#### Scenario: 有中继时非空

- **WHEN** 平台正在中继上级对某通道的实时 INVITE
- **THEN** 列表含对应 `upstream_call_id` 与 `channel_id`

#### Scenario: BYE 后清空

- **WHEN** 上级 BYE 结束中继
- **THEN** 随后查询不再包含该 Call-ID

## ADDED Requirements
### Requirement: 级联媒体会话只读列表
系统 MUST 提供 `GET /v1/nodes/:id/channels`… 更正为 `GET /v1/nodes/:id/cascade/sessions`，返回活动中继会话数组；无会话返回空列表。MUST NOT 经该端点拆除会话。
#### Scenario: 有中继时非空
- **WHEN** 平台正在中继上级 INVITE
- **THEN** 列表含对应 upstream_call_id 与 channel_id

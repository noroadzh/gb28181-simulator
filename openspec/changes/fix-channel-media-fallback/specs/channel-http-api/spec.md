# Spec Delta

## MODIFIED Requirements

### Requirement: 通道列表与媒体源
- `GET /v1/nodes/:id/channels` MUST 返回 `200 {channels: [...]}`，每项含 `id, name, status, media_kind, media_path`
- `GET /v1/nodes/:id/channels/:ch/media` MUST 返回 `200 {kind, path, ...}` 或 `204`
- `PUT /v1/nodes/:id/channels/:ch/media` MUST 接收 `MediaConfig` JSON 并返回 `200`
- `DELETE /v1/nodes/:id/channels/:ch/media` MUST 移除通道级媒体源并返回 `204`
- `GET /v1/nodes/:id/channels` 与 `GET /v1/nodes/:id/channels/:ch` 响应中的 `has_media` / `media_kind` MUST 按"通道级媒体配置优先、通道级未配置时回退到节点级媒体配置"的顺序解析；两者均未配置时 `has_media` MUST 为 `false` 且 `media_kind` MUST 省略
- `GET /v1/nodes/:id/channels/:ch/media` 响应中的 `has` 字段 MUST 保持通道级语义（仅表示通道级配置是否存在），MUST NOT 包含节点级回退含义；节点级回退仅通过上述列表与详情端点的 `has_media` 字段暴露

#### Scenario: 通道级未配置媒体源时回退到节点级
- **WHEN** 节点已配置节点级媒体源，通道未单独配置媒体源，GET `/v1/nodes/{nodeID}/channels` 或 GET `/v1/nodes/{nodeID}/channels/{chID}`
- **THEN** 200，响应中该通道的 `has_media` 为 `true` 且 `media_kind` 等于节点级媒体配置的 `kind`

#### Scenario: 通道级已配置媒体源时优先返回通道级
- **WHEN** 通道已单独配置媒体源，且节点同时存在节点级媒体配置，GET `/v1/nodes/{nodeID}/channels/{chID}`
- **THEN** 200，响应中该通道的 `has_media` 为 `true` 且 `media_kind` 等于通道级媒体配置的 `kind`

#### Scenario: 两级均未配置时返回 false
- **WHEN** 通道与节点均未配置媒体源，GET `/v1/nodes/{nodeID}/channels/{chID}`
- **THEN** 200，响应中该通道的 `has_media` 为 `false` 且 `media_kind` 字段省略

#### Scenario: 配置通道级媒体源
- **WHEN** PUT `/v1/nodes/{deviceID}/channels/{chID}/media` body=`{kind:"file",path:"/tmp/a.mp4",loop:true}`
- **THEN** 200 + 该通道后续 SIP INVITE 走该媒体源

#### Scenario: 查询设备通道列表
- **WHEN** GET `/v1/nodes/{deviceID}/channels`
- **THEN** 200 + 通道数组，每个含 id/name/status

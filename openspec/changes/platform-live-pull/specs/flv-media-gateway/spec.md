# flv-media-gateway Delta

## MODIFIED Requirements

### Requirement: HTTP-FLV 拉流端点
系统 MUST 暴露 `GET /v1/flv/:nodeID/:channelID` HTTP 端点，返回 `Content-Type: video/x-flv`，body 为连续的 FLV 流。

数据源选择：当该 (节点, 通道) 存在活动实时会话（`platform-live-pull`）时 MUST 使用实时会话；否则 MUST 回落该通道的本地媒体源。查询参数 `source=live|local` MUST 强制选择指定源，被选源不存在时 MUST 返回 404。设备节点的行为不变：没有实时会话时仍走本地媒体源。

#### Scenario: 单播放器订阅
- **WHEN** 浏览器请求 `/v1/flv/{device}/{ch01}`
- **THEN** 服务器响应 200 + `Content-Type: video/x-flv`；body 为 FLV 头 + 持续追加的 FLV tag

#### Scenario: 多个播放器同时订阅同一通道
- **WHEN** 两个浏览器同时请求同一通道
- **THEN** 两路 FLV 输出并行，每个订阅方独立获得流；上游媒体源不被重复拉取

#### Scenario: 通道不存在
- **WHEN** 请求的 nodeID 或 channelID 不存在
- **THEN** 返回 404 + JSON 错误体，不影响其他订阅者

#### Scenario: 实时会话优先
- **WHEN** 平台节点对某通道存在活动实时会话，浏览器请求该通道的 FLV
- **THEN** 输出的是设备经 RTP 回传的 PS 流，而不是平台本地媒体源

#### Scenario: 显式选择数据源
- **WHEN** 请求带 `source=local` 且该通道配置了本地媒体源
- **THEN** 即使存在实时会话，也输出本地媒体源
- **WHEN** 请求带 `source=live` 但没有活动实时会话
- **THEN** 返回 404，不静默回落

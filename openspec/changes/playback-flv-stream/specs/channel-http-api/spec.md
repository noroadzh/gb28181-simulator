# Spec Delta

## ADDED Requirements

### Requirement: 回放 FLV 流端点

`GET /v1/nodes/:id/channels/:ch/playback/flv?sn=<sn>` MUST 返回该回放会话的 HTTP-FLV 流（`Content-Type: video/x-flv`）。`sn` 必须对应一个已建立的回放会话；未知或已结束的会话 MUST 返回 404，MUST NOT 返回空的 200。

#### Scenario: 播放中的回放会话

- **WHEN** 回放会话 `sn=12345` 已建立并正在接收设备 RTP
- **THEN** GET 该端点返回 200、`Content-Type: video/x-flv`，body 以 `FLV` 魔数开头并可被 flv.js 解析

#### Scenario: 未知会话

- **WHEN** 以不存在的 `sn` 请求该端点
- **THEN** 返回 404 与 JSON 错误体

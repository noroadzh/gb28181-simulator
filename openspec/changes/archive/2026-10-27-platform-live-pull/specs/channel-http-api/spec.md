# channel-http-api Delta

## ADDED Requirements

### Requirement: 实时拉流控制端点

系统 MUST 提供通道级实时拉流的控制端点：

- `POST /v1/nodes/:id/channels/:ch/live` MUST 拉起实时会话，请求体可带 `{"transport": "udp"|"tcp"}`（缺省 `udp`），成功返回 `200 {session_id, transport, local_port, ssrc?}`。
- `POST /v1/nodes/:id/channels/:ch/live/stop` MUST 停止该通道的实时会话，成功返回 `204`。

失败 MUST 可辨识：节点身份不支持拉流（如 `device`）返回 `501`；没有可拉取的下级设备返回 `404`；设备不可达或应答不可用返回 `502`；设备无最终应答返回 `504`；通道或节点不存在返回 `404`。停止一个不存在的会话 MUST 返回 `204`（幂等）。

#### Scenario: 平台拉起实时流

- **WHEN** 对 platform-large 节点的某通道 POST live
- **THEN** 返回 200 与 session_id、transport、local_port
- **AND** 随后的 `/v1/flv/:nodeID/:channelID` 输出该会话的画面

#### Scenario: 指定 TCP 传输

- **WHEN** 请求体为 `{"transport":"tcp"}`
- **THEN** 平台以 TCP 被动方式监听并在响应中给出 local_port

#### Scenario: 无下级设备

- **WHEN** 平台没有可拉取的下级设备
- **THEN** 返回 404，响应体说明原因

#### Scenario: 设备拒绝或无应答

- **WHEN** 下级设备回 4xx/5xx 或超时
- **THEN** 分别返回 502 与 504

#### Scenario: 幂等停止

- **WHEN** 对没有活动会话的通道调用 live stop
- **THEN** 返回 204，不报错

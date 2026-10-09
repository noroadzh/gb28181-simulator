# channel-http-api Specification

## Purpose
把通道粒度的操作（PTZ/录像/对讲/快照）通过 HTTP API 暴露给前端。该能力定义 RESTful 端点的契约：路径、请求体、响应体、错误码。

## Requirements

### Requirement: 通道列表与媒体源
- `GET /v1/nodes/:id/channels` MUST 返回 `200 {channels: [...]}`，每项含 `id, name, status, media_kind, media_path`
- `GET /v1/nodes/:id/channels/:ch/media` MUST 返回 `200 {kind, path, ...}` 或 `204`
- `PUT /v1/nodes/:id/channels/:ch/media` MUST 接收 `MediaConfig` JSON 并返回 `200`
- `DELETE /v1/nodes/:id/channels/:ch/media` MUST 移除通道级媒体源并返回 `204`

#### Scenario: 查询设备通道列表
- **WHEN** GET `/v1/nodes/{deviceID}/channels`
- **THEN** 200 + 通道数组，每个含 id/name/status

#### Scenario: 配置通道级媒体源
- **WHEN** PUT `/v1/nodes/{deviceID}/channels/{chID}/media` body=`{kind:"file",path:"/tmp/a.mp4",loop:true}`
- **THEN** 200 + 该通道后续 SIP INVITE 走该媒体源

### Requirement: PTZ 云台控制
- `POST /v1/nodes/:id/channels/:ch/ptz` MUST 接收 `{cmd, speed?, preset_id?}` 并通过 MANSCDP DeviceControl 下发
- cmd 枚举: `up, down, left, right, upleft, upright, downleft, downright, zoomin, zoomout, focusnear, focusfar, irisin, irisout, stop`
- speed 取值 1-255；preset_id 1-255

#### Scenario: 八方向 PTZ
- **WHEN** POST ptz body=`{cmd:"up", speed:128}`
- **THEN** DeviceControl MANSCDP XML 通过 SIP MESSAGE 发送，包含 PTZCmd=AAAA...，结果 200

#### Scenario: 调预置位
- **WHEN** POST ptz body=`{cmd:"preset_call", preset_id:5}`
- **THEN** DeviceControl XML 携带 preset 5，200 返回

### Requirement: 录像查询与回放控制
- `GET /v1/nodes/:id/channels/:ch/records?start=&end=` MUST 触发 RecordInfo MANSCDP 查询，返回 `200 {records: [{sn, start, end, type, size}]}`
- `POST /v1/nodes/:id/channels/:ch/playback` MUST 接收 `{action, sn, transport?}`，action 枚举 `play, pause, teardown, speed`
- `GET /v1/nodes/:id/channels/:ch/playback/flv?sn=` MUST 返回 FLV 格式的历史流（直接 SIP INVITE 走历史回放）

#### Scenario: 查询录像列表
- **WHEN** GET records?start=2026-10-01T00:00:00&end=2026-10-08T00:00:00
- **THEN** 200 + records 数组

#### Scenario: 开始回放
- **WHEN** POST playback body=`{action:"play", sn:"12345", transport:"UDP"}`
- **THEN** 服务器发送 PlaybackControl MANSCDP XML 并通过 INVITE 启动回放流

### Requirement: 语音对讲
- `POST /v1/nodes/:id/channels/:ch/talk/start` MUST 触发 SIP INVITE（audio）建立对讲会话，返回 `200 {session_id}`
- `POST /v1/nodes/:id/channels/:ch/talk/stop` MUST 发送 BYE 关闭会话，返回 `204`

#### Scenario: 开始对讲
- **WHEN** POST talk/start
- **THEN** 服务器发出 SIP INVITE，SDP 携带 PCMU 编码；200 + session_id

#### Scenario: 停止对讲
- **WHEN** POST talk/stop with session_id
- **THEN** 服务器发送 SIP BYE，204

### Requirement: 快照抓图
- `GET /v1/nodes/:id/channels/:ch/snapshot` MUST 返回 `200 image/jpeg`，body 为最近一帧 JPEG 图像（从 PS 流中解码或解码后转码）。

#### Scenario: 抓快照
- **WHEN** GET snapshot
- **THEN** 200 + JPEG bytes

#### Scenario: 通道无媒体源
- **WHEN** GET snapshot 但通道无媒体源
- **THEN** 503 + JSON 错误

### Requirement: Dashboard 统计
- `GET /v1/stats` MUST 返回 `{total_channels, online_channels, offline_channels, active_streams, total_nodes, online_nodes}`

#### Scenario: 查询统计
- **WHEN** GET /v1/stats
- **THEN** 200 + JSON 含上述字段

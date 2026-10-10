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

### Requirement: 通道增删 HTTP 端点

系统 MUST 提供以下端点对 device 节点的通道进行运行时增删：

- `POST /v1/nodes/:id/channels` —— 新增通道（body：channel_id + name + status?）；成功回 201 与新建通道对象；重复 id 回 409；非 device 节点回 404
- `DELETE /v1/nodes/:id/channels/:ch` —— 删除通道；成功回 204；不存在回 404；非 device 节点回 404

新增/删除 MUST 即时反映到后续的 `GET /v1/nodes/:id/channels` 与 Catalog 查询结果。

#### Scenario: 新增通道后可查询到

- **WHEN** POST 新增通道 `34020000001320000099`，随后 GET `/v1/nodes/:id/channels`
- **THEN** 返回列表含该通道

#### Scenario: 新增重复 id 回 409

- **WHEN** POST 一个已存在的 channel_id
- **THEN** 回 409 Conflict

#### Scenario: 删除通道后查询不到

- **WHEN** DELETE 一个存在的通道，随后 GET 列表
- **THEN** 列表不含该通道

#### Scenario: 非 device 节点回 404

- **WHEN** 对 platform-large 节点调用通道增删端点
- **THEN** 回 404 Not Found

### Requirement: 媒体文件直出

系统 MUST 提供 `GET /v1/nodes/{nodeID}/channels/{channelID}/media-file` 端点，按节点+通道解析媒体配置并直接输出其指向的媒体文件，供浏览器原生 video 播放使用。

- 解析顺序 MUST 为：通道级媒体配置 → 节点级媒体配置（回退）；两者均未配置时返回 404
- 仅 `kind=file` 的媒体源可被服务；其他 kind（rtsp/hls/synthetic 等）MUST 返回 400
- 响应 MUST 支持 HTTP Range 请求（206 Partial Content），以支持浏览器拖动与断点续传
- 文件不存在或不是普通文件时 MUST 返回 404
- 文件路径 MUST 仅来自已保存的媒体配置，MUST NOT 接受请求参数指定的任意路径

#### Scenario: MP4 文件直出成功

- **WHEN** 通道（或回退到节点）的媒体配置为 `kind=file` 且 path 指向一个存在的 .mp4 文件
- **THEN** `GET /v1/nodes/{nodeID}/channels/{channelID}/media-file` 返回 200，Content-Type 为 video/mp4，响应体为文件内容

#### Scenario: Range 请求返回 206

- **WHEN** 客户端携带 `Range: bytes=0-1023` 请求该端点且媒体文件存在
- **THEN** 响应状态码为 206，Content-Range 标头指示所返回的字节区间，响应体长度为请求区间长度

#### Scenario: 通道级未配置回退节点级

- **WHEN** 通道未配置媒体源，但其所属节点配置了 `kind=file` 的媒体源且文件存在
- **THEN** 请求返回 200 并输出节点级配置指向的文件

#### Scenario: 非 file 源拒绝

- **WHEN** 解析得到的媒体配置 kind 为 rtsp/hls/synthetic
- **THEN** 请求返回 400，响应体说明仅文件源可直出

#### Scenario: 无配置时 404

- **WHEN** 通道级与节点级均未配置媒体源
- **THEN** 请求返回 404

#### Scenario: 文件缺失时 404

- **WHEN** 媒体配置 kind=file 但 path 指向的文件不存在或不是普通文件
- **THEN** 请求返回 404

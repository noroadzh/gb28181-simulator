# channel-http-api Specification

## Purpose
把通道粒度的操作（PTZ/录像/对讲/快照）通过 HTTP API 暴露给前端。该能力定义 RESTful 端点的契约：路径、请求体、响应体、错误码。

## Requirements

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

> 旧条款：对 `device` 节点调用 `POST /v1/nodes/:id/channels` 与 `DELETE /v1/nodes/:id/channels/:ch` 可成功；非 `device` 节点回 404。
>
> 新条款：对 `device` / `platform-large` / `platform-small` 节点调用上述两个端点均可成功；非这三类节点回 404。新增的通道即时反映到 `GET /v1/nodes/:id/channels` 与节点后续的 Catalog 响应。
>
> 命名说明：下方 `非 device 节点回 404` 这一 Scenario 的标题沿用主 spec 原标题未改，但其规则已收窄为"非 platform 节点回 404"——保留标题是为满足 MODIFIED 块的 scenario 防丢失校验（按标题精确匹配），语义以 WHEN/THEN 为准。

系统 MUST 提供以下端点对节点的通道进行运行时增删：

- `POST /v1/nodes/:id/channels` —— 新增通道（body：channel_id + name + status?）；成功回 201 与新建通道对象；重复 id 回 409；非 platform 节点回 404
- `DELETE /v1/nodes/:id/channels/:ch` —— 删除通道；成功回 204；不存在回 404；非 platform 节点回 404

新增 / 删除 MUST 即时反映到后续的 `GET /v1/nodes/:id/channels` 与 Catalog 查询结果。

#### Scenario: 新增通道后可查询到

- **WHEN** POST 新增通道 `34020000001320000099` 至 `device` 节点，随后 GET `/v1/nodes/:id/channels`
- **THEN** 返回列表含该通道

#### Scenario: 新增重复 id 回 409

- **WHEN** POST 一个已存在的 channel_id
- **THEN** 回 409 Conflict

#### Scenario: 删除通道后查询不到

- **WHEN** DELETE 一个存在的通道，随后 GET 列表
- **THEN** 列表不含该通道

#### Scenario: 非 device 节点回 404

> 标题沿用主 spec 原标题，规则已收窄为"非 platform 节点回 404"——保留标题是为满足 MODIFIED 块的 scenario 防丢失校验（按标题精确匹配），语义以 WHEN/THEN 为准。

- **WHEN** POST / DELETE 通道对未知节点类型（如未来引入的中间件节点）
- **THEN** 回 404 Not Found

#### Scenario: 对 device 节点调用的原有行为不变

- **WHEN** POST / DELETE 通道对 `device` 节点
- **THEN** 行为与该 capability 原有契约一致

#### Scenario: 对 platform-large 节点新增通道成功

- **WHEN** POST `/v1/nodes/{platform-large-id}/channels` body 含合法 channel_id / name
- **THEN** 回 201 + 新建通道对象；该通道持久化到 channel store

#### Scenario: 对 platform-large 节点删除通道成功

- **WHEN** DELETE `/v1/nodes/{platform-large-id}/channels/{channel-id}` 且该通道存在
- **THEN** 回 204；channel store 中该通道被删除

#### Scenario: 新增通道后立即出现在 Catalog 响应中

- **WHEN** POST 新增通道至 `platform-large` 节点
- **AND** 第三方上级随即发送 `MESSAGE(Catalog)` 至该 `platform-large`
- **THEN** Catalog 响应的 `DeviceList` 含该通道，按 device id 升序排列

#### Scenario: 删除通道后立即从 Catalog 响应中消失

- **WHEN** DELETE 一条已存在于 `platform-large` 节点的通道
- **AND** 第三方上级随即发送 `MESSAGE(Catalog)` 至该 `platform-large`
- **THEN** Catalog 响应的 `DeviceList` 不含该通道

#### Scenario: 新增通道时 ID 必须符合 20 位国标码格式

- **WHEN** POST `/v1/nodes/:id/channels` body 的 channel_id 不是 20 位数字
- **THEN** 回 400 Bad Request，错误信息指明 ID 格式要求

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

### Requirement: 通道控制操作的能力缺失语义

当节点不是 `device` 身份、没有在线下级设备、或下发失败时，PTZ / 录像查询 / 回放控制 MUST 返回可辨识的失败：能力缺失 `501 Not Implemented`，下级不可达 `502 Bad Gateway`，附属超时 `504 Server Time-out`。MUST NOT 以 200 空操作掩盖未实现的路径，响应体 MUST 说明原因。

#### Scenario: 非 device 节点执行 PTZ

- **WHEN** 对 `platform-large` 节点调用 `POST /channels/:ch/ptz`
- **THEN** 返回 501，响应体说明"节点不是 device"

#### Scenario: 下级设备不可达

- **WHEN** device 节点存在但信令地址不可达，调用 PTZ 或录像查询
- **THEN** 返回 502，响应体含下级 deviceID 与原因

#### Scenario: 正常下发

- **WHEN** device 节点在线且可达，调用 PTZ
- **THEN** 返回 200，DeviceControl MANSCDP XML 已通过 SIP MESSAGE 发出

### Requirement: 回放会话生命周期

同一通道的重复 `play` MUST NOT 泄漏会话：要么复用既有会话，要么先终止旧会话再建新的。`teardown` MUST 释放该会话占用的端口与上游对话；会话超过约定时长无控制命令 MUST 自动回收。

#### Scenario: 重复 play 只留一个会话

- **WHEN** 对同一通道连续两次 `play`
- **THEN** 服务端只保留一个活动回放会话，且第一次占用的资源已释放

#### Scenario: teardown 释放资源

- **WHEN** `play` 后调用 `teardown`
- **THEN** 返回 200，回放端口可立即重新绑定，上游对话收到 BYE

### Requirement: 对讲会话生命周期

`TalkStart` MUST 建立 SIP 音频对话（INVITE，SDP 携带 PCMU）并返回 session id 与浏览器可连接的 WSS 地址；`TalkStop` MUST 发送 BYE 并回收会话。对端 BYE、媒体超时或节点停止 MUST 回收会话；重复 `TalkStop` MUST 幂等。

#### Scenario: 建立与结束对讲

- **WHEN** 对在线 device 通道调用 `talk/start`，随后 `talk/stop`
- **THEN** 第一次返回 200 与 session_id/WSS 地址；第二次返回 204，且上游收到 BYE

#### Scenario: 对端先挂断

- **WHEN** 设备侧先发 BYE
- **THEN** 本地会话被回收，再次 `talk/stop` 仍返回 204（幂等）

### Requirement: 快照来源与失败语义

`Snapshot` MUST 对**能直接产出像素**的媒体源生成 JPEG：合成测试图（彩条/时钟等）直接渲染当前帧；需要解码的视频编码（H.264 / H.265 / MPEG-4 等）MUST 返回 501 并说明原因——本 change 不引入解码器依赖。无媒体源 MUST 返回 503；节点不是 `device` MUST 返回 501；MUST NOT 返回空 body 的 200。

#### Scenario: 合成测试图源抓到 JPEG

- **WHEN** 通道配置了合成测试图媒体源
- **THEN** 返回 200，`Content-Type: image/jpeg`，body 为可解码的 JPEG（SOI 头 0xFFD8）

#### Scenario: 需要解码的源返回 501

- **WHEN** 通道配置的是 H.264/H.265 等需要解码的文件或流
- **THEN** 返回 501，错误体说明"需要解码器" 

#### Scenario: 无媒体源

- **WHEN** 通道未配置媒体源
- **THEN** 返回 503 与 JSON 错误体

### Requirement: 对讲音频 WebSocket

`GET /v1/nodes/:id/channels/:ch/talk/audio?session=<id>` MUST 升级为 WebSocket。服务器发出的二进制帧 MUST 是 PCMU 载荷（每帧一个 RTP 载荷，8kHz 单声道）；客户端发来的二进制帧 MUST 被封装成 RTP（PT=0）并发送到设备在 SDP 应答中声明的 RTP 端点。未知或已结束的 session MUST 返回 404。关闭 WebSocket MUST NOT 结束 SIP 对话——那是 `talk/stop` 的职责。

#### Scenario: 下行音频到达浏览器

- **WHEN** 前端以有效 session 连接，设备向平台的 RTP 端口发来一个 PCMU 包
- **THEN** 前端在 1 秒内收到一个二进制帧，内容为该 RTP 包的载荷（去掉 12 字节头）

#### Scenario: 上行音频到达设备

- **WHEN** 前端发送一帧 PCMU 载荷
- **THEN** 设备的 RTP 端点收到一个 RTP 包，PT=0、SSRC 稳定、序列号递增

#### Scenario: 未知会话

- **WHEN** 以不存在的 session 连接
- **THEN** HTTP 404，不升级为 WebSocket

### Requirement: 回放 FLV 流端点

`GET /v1/nodes/:id/channels/:ch/playback/flv?sn=<sn>` MUST 返回该回放会话的 HTTP-FLV 流（`Content-Type: video/x-flv`）。`sn` 必须对应一个已建立的回放会话；未知或已结束的会话 MUST 返回 404，MUST NOT 返回空的 200。

#### Scenario: 播放中的回放会话

- **WHEN** 回放会话 `sn=12345` 已建立并正在接收设备 RTP
- **THEN** GET 该端点返回 200、`Content-Type: video/x-flv`，body 以 `FLV` 魔数开头并可被 flv.js 解析

#### Scenario: 未知会话

- **WHEN** 以不存在的 `sn` 请求该端点
- **THEN** 返回 404 与 JSON 错误体

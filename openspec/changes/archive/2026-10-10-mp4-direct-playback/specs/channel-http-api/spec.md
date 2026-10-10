# Delta: channel-http-api

## ADDED Requirements

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

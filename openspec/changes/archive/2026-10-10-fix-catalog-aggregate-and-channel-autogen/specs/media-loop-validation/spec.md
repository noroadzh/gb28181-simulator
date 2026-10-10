# media-loop-validation Delta

## ADDED Requirements

### Requirement: Loop 仅允许 file 类型

`PUT /v1/nodes/:id/media` 与 `PUT /v1/nodes/:id/channels/:ch/media` 在收到 `loop = true` 且 `kind != "file"` 的 MediaConfig 时，MUST 返回 `400 Bad Request`，错误信息 MUST 指明 loop 仅支持 file 来源并回显实际类型。服务端 MUST NOT 将此类配置静默写入 profile。

#### Scenario: 非 file 类型启用 loop 返回 400

- **WHEN** `PUT /v1/nodes/:id/media` 请求体为 `{"kind":"rtsp","path":"rtsp://x/live","loop":true}`
- **THEN** 响应 `400 Bad Request`，错误信息包含 `loop is only supported for file sources`
- **AND** 错误信息包含实际类型 `"rtsp"`
- **AND** profile 中不写入该配置

#### Scenario: file 类型启用 loop 正常保存

- **WHEN** `PUT /v1/nodes/:id/media` 请求体为 `{"kind":"file","path":"/media/a.ps","loop":true}`
- **THEN** 响应 `200 OK`，配置保存成功
- **AND** 文件播放至 EOF 后从头循环

#### Scenario: 非 file 类型 loop 为 false 正常保存

- **WHEN** `PUT /v1/nodes/:id/media` 请求体为 `{"kind":"rtsp","path":"rtsp://x/live","loop":false}`
- **THEN** 响应 `200 OK`，配置保存成功

#### Scenario: 通道级媒体配置同样校验

- **WHEN** `PUT /v1/nodes/:id/channels/:ch/media` 请求体为 `{"kind":"hls","path":"http://x/index.m3u8","loop":true}`
- **THEN** 响应 `400 Bad Request`，错误信息与节点级校验一致

#### Scenario: Web 界面对不支持的组合给出提示

- **WHEN** 用户在 Web 界面媒体源面板选择非 file 类型
- **THEN** loop 开关 MUST 置为禁用态，并展示「仅 file 类型支持循环播放」提示
- **AND** 用户保存时后端若仍收到非法组合（如历史遗留数据），错误提示 MUST 可见

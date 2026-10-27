# channel-http-api Delta

## ADDED Requirements

### Requirement: 平台录像 HTTP 端点

系统 MUST 提供平台录像的查询与回放端点：

- `GET /v1/nodes/:id/channels/:ch/recordings?start=&end=` MUST 返回 `200 {recordings: [...]}`，每项含 `id, start_time, end_time, size, file_path`；时间参数为 RFC 3339，缺省表示不限。
- `GET /v1/nodes/:id/channels/:ch/recordings/:recording/flv` MUST 把该录像回放为 `video/x-flv` 流。

失败 MUST 可辨识：录像子系统未启用返回 `501`；录像不存在或不属于该节点/通道返回 `404`；时间参数不可解析返回 `400`。

#### Scenario: 列出录像

- **WHEN** 对平台节点的某通道 GET recordings
- **THEN** 返回该通道在窗口内的录像列表，按开始时间升序

#### Scenario: 回放录像

- **WHEN** GET 某条录像的 flv 端点
- **THEN** 返回 200 + `video/x-flv`，body 以 FLV 头开始

#### Scenario: 录像不存在

- **WHEN** 请求一个不存在的录像标识
- **THEN** 返回 404，而不是空 200

#### Scenario: 未启用录像

- **WHEN** 录像功能关闭
- **THEN** 列表与回放端点返回 501

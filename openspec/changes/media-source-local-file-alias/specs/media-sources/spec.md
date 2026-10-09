# Spec: media-sources delta (local_file alias)

## MODIFIED Requirements

### Requirement: 媒体源 kind 接受 `local_file` 别名

`MediaConfig.Normalize()` MUST 将 `Kind == "local_file"` 替换为 `SourceKindFile ("file")`，确保前端通过上传文件自动绑定时使用的可读 kind 值与后端 MediaSourceFactory 一致。该归一化发生在所有写入路径（`SetMedia()` / `SetChannelMedia()` / 数据库恢复）的最早入口，因此已有数据库中存储的 `local_file` 记录在重启后自然被规整。

#### Scenario: `local_file` 被归一化为 `file`

- **GIVEN** 一个 `MediaConfig{Kind: "local_file", Path: "/data/v.mp4"}`
- **WHEN** 调用 `cfg.Normalize()`
- **THEN** 返回值的 `Kind == "file"`（即 `SourceKindFile`）
- **AND** `Path` 保持原值不变
- **AND** `Normalize().Validate()` 不返回错误
- **AND** 通过 `MediaSourceFactory` 可以打开为 `FileSource`

#### Scenario: `file` 原样保留

- **GIVEN** 一个 `MediaConfig{Kind: "file", Path: "/data/v.mp4"}`
- **WHEN** 调用 `cfg.Normalize()`
- **THEN** 返回值的 `Kind` 仍为 `file`（无副作用）
- **AND** `Path` 保持原值

#### Scenario: 其他 kind 不被归一化影响

- **GIVEN** 任意 `MediaConfig{Kind: "rtsp"|"hls"|"synthetic"|"remote_file", ...}`
- **WHEN** 调用 `cfg.Normalize()`
- **THEN** `Kind` 保持原值
- **AND** 仅 MTU/FPS/Clock 默认值被填充

## ADDED Requirements

### Requirement: 上传结果可直接绑定为媒体源（跨接口一致性）

PUT `/v1/nodes/:id/channels/:ch/media` 与 PUT `/v1/nodes/:id/media` MUST 接受 `kind: "local_file"` 并将其归一化为 `file` 后存盘。下次启动节点时，`ChannelMediaConfig(ch)` / `MediaConfig()` 返回的 kind 字段值 MUST 为 `file`，确保 FLV 流媒体端点 `/v1/flv/:nodeID/:channelID` 可成功打开。

#### Scenario: 通道级 `local_file` 上传后绑定

- **GIVEN** 通过 `POST /v1/nodes/:id/upload` 上传得到 path
- **WHEN** 发送 `PUT /v1/nodes/:id/channels/:ch/media` body `{"kind":"local_file","path":<返回路径>}`
- **THEN** 响应 200，body 中 `kind` 字段返回 `"file"`（已归一化）
- **AND** `GET /v1/flv/:nodeID/:channelID` 返回 200 + `Content-Type: video/x-flv` 而非 404

#### Scenario: 节点级 `local_file` 绑定

- **GIVEN** 任意节点 ID
- **WHEN** 发送 `PUT /v1/nodes/:id/media` body `{"kind":"local_file","path":"/data/v.mp4"}`
- **THEN** 响应 200，body 中 `kind` 字段返回 `"file"`
- **AND** 重启节点后 `GET /v1/nodes/:id/media` 仍返回 `kind: "file"`

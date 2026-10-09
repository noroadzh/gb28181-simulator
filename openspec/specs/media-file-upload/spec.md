# media-file-upload Specification

## Purpose
提供媒体文件 multipart 上传能力：浏览器上传 MP4/TS 等文件到容器内 uploads 目录，返回可直接绑定为节点级或通道级媒体源的容器内路径。

## Requirements

### Requirement: 媒体文件上传端点

系统 MUST 提供 `POST /v1/nodes/:id/media/upload`（multipart/form-data）端点，将上传的文件流式写入节点数据目录下的 `uploads/` 子目录，返回 `{ "path": "<容器内绝对路径>" }` 供 MediaConfig 直接使用。

#### Scenario: 上传 mp4 返回可绑定路径

- **WHEN** 上传一个 `demo.mp4`（合法扩展名、大小在限额内）
- **THEN** 回 200，body 含 `path` 为容器内 `uploads/demo.mp4` 的绝对路径；文件真实落盘

#### Scenario: 非白名单扩展名拒绝

- **WHEN** 上传 `evil.exe`
- **THEN** 回 400 Bad Request；不落盘

#### Scenario: 超过大小上限拒绝

- **WHEN** 上传文件大小超过默认 2GB（或配置的上限）
- **THEN** 回 413 Payload Too Large；不落盘

#### Scenario: 防目录穿越

- **WHEN** 上传文件名含路径分隔符或 `..`（如 `../etc/passwd`）
- **THEN** 系统仅取 `filepath.Base` 作为落盘文件名，不写入 uploads 目录之外

### Requirement: 上传结果可直接绑定为媒体源

返回的 `path` MUST 可作为 `MediaConfig.kind = local_file` 的 `path` 字段值，对节点级与通道级媒体源配置均生效。

#### Scenario: 上传后绑定到节点级媒体源

- **WHEN** 上传得到 path 后，PUT `/v1/nodes/:id/media` 以 `{kind:"local_file", path:<返回值>}` 保存
- **THEN** 后续 INVITE 预览能正常推流该文件

#### Scenario: 上传后绑定到通道级媒体源

- **WHEN** 上传得到 path 后，PUT `/v1/nodes/:id/channels/:ch/media` 以 `{kind:"local_file", path:<返回值>}` 保存
- **THEN** 该通道预览推流该文件

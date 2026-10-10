# flv-media-gateway Specification

## Purpose
为浏览器提供可播放的实时视频流。该能力在 Go 进程内实现一个 HTTP-FLV 转封装网关，把现有 PS+RTP 管道输出转封装为浏览器友好的 FLV over HTTP，供前端 flv.js 直接拉流。

## Requirements

### Requirement: HTTP-FLV 拉流端点
系统 MUST 暴露 `GET /v1/flv/:nodeID/:channelID` HTTP 端点，返回 `Content-Type: video/x-flv`，body 为连续的 FLV 流。

#### Scenario: 单播放器订阅
- **WHEN** 浏览器请求 `/v1/flv/{device}/{ch01}`
- **THEN** 服务器响应 200 + `Content-Type: video/x-flv`；body 为 FLV 头 + 持续追加的 FLV tag

#### Scenario: 多个播放器同时订阅同一通道
- **WHEN** 两个浏览器同时请求同一通道
- **THEN** 两路 FLV 输出并行，每个订阅方独立获得流；上游媒体源不被重复拉取

#### Scenario: 通道不存在
- **WHEN** 请求的 nodeID 或 channelID 不存在
- **THEN** 返回 404 + JSON 错误体，不影响其他订阅者

### Requirement: FLV 转封装
HTTP-FLV 网关 MUST 将 PS 包的 PES 视频/音频帧按 FLV 规范封装为 Video/Audio tag，写入 `script` tag 中的 onMetaData（包含 duration=0、width、height、videocodecid、audiocodecid）。Video tag 头 MUST 携带 AVC/HEVC sequence header（AVCDecoderConfigurationRecord 或 HEVCDecoderConfigurationRecord）。

#### Scenario: 写入 FLV 头和 onMetaData
- **WHEN** 第一个订阅者接入
- **THEN** 服务器先写 `FLV` 头（9 字节）+ onMetaData script tag，再追加视频/音频 tag

#### Scenario: H.264 视频 tag
- **WHEN** 上游 PS 包解出 H.264 NALU
- **THEN** 网关生成 FLV Video tag，FrameType=1、AVCPacketType=1（NALU）携带原始 NALU

#### Scenario: H.265 视频 tag
- **WHEN** 上游 PS 包解出 H.265 NALU
- **THEN** 网关生成 FLV Video tag，Body 携带 HEVC NALU（AVCPacketType 复用 0/1）

#### Scenario: AAC/Audio tag
- **WHEN** 上游 PS 包解出音频帧
- **THEN** 网关生成 FLV Audio tag，包含 AudioSpecificConfig（首个 tag）

### Requirement: 订阅生命周期
订阅方关闭连接 MUST 触发取消订阅；上游媒体源仅在存在订阅者时保持推流。空闲超时后 MUST 释放底层 PS 解码器。

#### Scenario: 订阅者断开
- **WHEN** 浏览器关闭 tab 或 HTTP 连接断开
- **THEN** 网关在 1 秒内释放该订阅的资源；上游媒体源若已无订阅者则停止推流

#### Scenario: 空闲超时
- **WHEN** 订阅者已 0 个且超过 30 秒
- **THEN** 上游媒体源发送 BYE，SSRC 释放

### Requirement: 网关对 MPEG-4 Part 2 源明确降级

FLV 网关 MUST 将 MPEG-4 Part 2 起始码字节（`0xB0`/`0xB3`/`0xB5`/`0xB6`/`0x20`~`0x2F`）识别为独立编码类型（优先于 H.264 判定；其中 `0x20`~`0x2F` 区间需与 HEVC IDR/CRA 消歧），并在检测到该编码时：

1. MUST NOT 按 H.264/HEVC 封装 FLV tag 输出（禁止产生坏数据）。
2. MUST 写入一条明确的错误级日志，说明 FLV 不支持 MPEG-4 Part 2、应通过 GB28181 PS/RTP 通道收流验证。
3. MUST 终止该预览会话并关闭输出通道，使 HTTP 响应正常结束。

GB28181 PS/RTP 出流路径 MUST NOT 受此降级影响。

#### Scenario: MPEG-4 源不再被误判为 H.264
- **WHEN** 上游 PS 包解出的首帧为 MPEG-4 Part 2 码流（起始码 `00 00 01 B6`）
- **THEN** 编码识别结果为 MPEG-4 而非 AVC

#### Scenario: HEVC IDR 切片不被误判为 MPEG-4
- **WHEN** 上游解出的 NALU 首字节落在 `0x20`~`0x2F` 且第二字节满足 HEVC nuh_temporal_id_plus1 恒非 0 特征（如 IDR_W_RADL 0x26）
- **THEN** 编码识别结果为 HEVC 而非 MPEG-4

#### Scenario: MPEG-4 预览会话被明确终止
- **WHEN** FLV 网关订阅一个 MPEG-4 Part 2 媒体源
- **THEN** 网关输出 FLV 头后写错误日志并关闭流，不输出任何 AVC/HEVC 视频 tag

#### Scenario: H.264/HEVC 源行为不变
- **WHEN** FLV 网关订阅 H.264 或 H.265 媒体源
- **THEN** sequence header 与视频 tag 输出与降级能力加入前逐字节一致

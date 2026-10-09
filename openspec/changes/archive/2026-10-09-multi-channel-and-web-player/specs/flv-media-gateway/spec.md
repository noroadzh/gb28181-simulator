# Spec Delta: flv-media-gateway

## Purpose

为浏览器提供可播放的实时视频流。该能力在 Go 进程内实现一个 HTTP-FLV 转封装网关，把现有 PS+RTP 管道输出转封装为浏览器友好的 FLV over HTTP，供前端 flv.js 直接拉流。

## ADDED Requirements

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

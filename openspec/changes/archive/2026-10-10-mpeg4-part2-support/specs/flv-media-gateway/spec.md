# Spec Delta: flv-media-gateway

## ADDED Requirements

### Requirement: 网关对 MPEG-4 Part 2 源明确降级

FLV 网关 MUST 将 MPEG-4 Part 2 起始码字节（`0xB0`/`0xB3`/`0xB5`/`0xB6`/`0x20`~`0x2F`）识别为独立编码类型（优先于 H.264 判定），并在检测到该编码时：

1. MUST NOT 按 H.264/HEVC 封装 FLV tag 输出（禁止产生坏数据）。
2. MUST 写入一条明确的错误级日志，说明 FLV 不支持 MPEG-4 Part 2、应通过 GB28181 PS/RTP 通道收流验证。
3. MUST 终止该预览会话并关闭输出通道，使 HTTP 响应正常结束。

GB28181 PS/RTP 出流路径 MUST NOT 受此降级影响。

#### Scenario: MPEG-4 源不再被误判为 H.264

- **WHEN** 上游 PS 包解出的首帧为 MPEG-4 Part 2 码流（起始码 `00 00 01 B6`）
- **THEN** 编码识别结果为 MPEG-4 而非 AVC

#### Scenario: MPEG-4 预览会话被明确终止

- **WHEN** FLV 网关订阅一个 MPEG-4 Part 2 媒体源
- **THEN** 网关输出 FLV 头后写错误日志并关闭流，不输出任何 AVC/HEVC 视频 tag

#### Scenario: H.264/HEVC 源行为不变

- **WHEN** FLV 网关订阅 H.264 或 H.265 媒体源
- **THEN** sequence header 与视频 tag 输出与本次变更前逐字节一致

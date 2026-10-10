# Spec Delta: core-manscdp-and-ps

## ADDED Requirements

### Requirement: PS 封装器在流首携带 PSM 声明编码

`PSPacketizer` MUST 在每条输出 PS 流的首个视频帧前插入一次 Program Stream Map（PSM，起始码 `00 00 01 BC`），声明本次流的编码类型；编码按首个视频 ES 帧的字节特征惰性判定，MUST NOT 依赖调用方显式传入：

- 视频起始码字节为 `0xB0`/`0xB3`/`0xB5`/`0xB6`（或 VOL `0x20`~`0x2F`）→ stream_type `0x10`（ISO/IEC 14496-2 Visual）
- 识别为 H.264 → `0x1B`；识别为 H.265 → `0x24`
- 音频为 AAC → `0x0F`

PSM 结构 MUST 符合 ISO/IEC 13818-1 §2.5.4，携带合法的 CRC-32/MPEG-2 校验值。PSM 对 PS 解包端 MUST 透明：不携带 PSM 识别能力的既有解包路径行为不变。

#### Scenario: MPEG-4 源的 PS 流首携带 stream_type 0x10 的 PSM

- **WHEN** 一个 MPEG-4 Part 2 ES 帧（首起始码 `00 00 01 B6`）首次送入 PSPacketizer
- **THEN** 该帧的 PS 输出前出现一个 PSM，其 elementary_stream_map 声明 stream_id `0xE0`、stream_type `0x10`，且 CRC 校验通过

#### Scenario: H.264 源声明 0x1B，且 PSM 每流仅一次

- **WHEN** 一个 H.264 源连续输出多帧
- **THEN** 仅第一帧的 PS 输出前有 PSM（stream_type `0x1B`），后续帧无 PSM

#### Scenario: PSM 对既有解包端透明

- **WHEN** 带 PSM 的 PS 流送入既有 PS 解包器与 FLV 网关的 PS 解析路径
- **THEN** 帧切割与 ES 提取结果与无 PSM 时完全一致，PSM 被跳过不产出 ES 帧

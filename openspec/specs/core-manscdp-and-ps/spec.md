# core-manscdp-and-ps 规范

## Purpose
`core-manscdp-and-ps` 是 GB/T 28181 的协议内容层：MANSCDP XML 消息的编解码，以及 PS↔RTP 双向转封装。它让"能解析命令"成为"能执行命令"，为后续级联、动态目录、云台控制等能力提供可复用的协议基础。

## Requirements

### 需求：MANSCDP XML 全量命令集编解码往返

系统 MUST 无数据丢失地编解码以下 MANSCDP 命令类型：
- DeviceInfo 查询 / 响应
- RecordInfo 查询 / 响应
- Alarm 通知 / 确认
- ConfigDownload 命令 / 确认
- PTZControl / 遥测
- Preset 查询 / 设置

编解码结果必须通过字节级 golden test 锁定；未知命令类型必须返回可解析结果而不报错。

#### 场景：DeviceInfo 查询往返

- **WHEN** 一条 DeviceInfo 请求先编码再解码
- **THEN** 所有字段（DeviceID、SN、StartTime、EndTime 等）逐字节保留

#### 场景：RecordInfo 响应含可选字段

- **WHEN** 一条 RecordInfo 响应包含可选字段（Video、Audio、Summary）
- **THEN** 解码得到相同的可选字段集合，无丢失

#### 场景：未预先注册时收到 Alarm 通知

- **WHEN** 一条 Alarm 通知到达，其 DeviceID 未注册
- **THEN** 解码仍然成功并产出合法 Notify 消息

#### 场景：PTZ 控制保持速度与方向

- **WHEN** 一条 PTZ 命令携带 1–255 速度值与方向位
- **THEN** 编码字节保持精确的数值范围与位布局

#### 场景：Preset 设置响应带预置位索引

- **WHEN** 发送一条携带预置位索引 1–255 的 Preset set 命令
- **THEN** 响应在其 SN 或 Result 中包含相同的预置位索引

#### 场景：未知命令类型可被解析

- **WHEN** MANSCDP body 含未识别的 CmdType
- **THEN** 解码返回通用 Notify 且不报错

### 需求：PS 解包器按 pack header 切割帧

系统 MUST 通过定位 `00 00 01 BA` 起始码并利用 pack header 长度切割 ES 帧来解包 PS 流。MUST 在无需外部边界信息的情况下处理 MPEG-2 Video 与 MPEG-1/2 Audio 的 PES 有效载荷。

PS 解包以 `00 00 01 BA` 为帧边界，不依赖外部注入的 marker。

#### 场景：连续 PS 流正确解包

- **WHEN** 一条含多个 pack header 的 PS 流送入解包器
- **THEN** ES 帧恰好在每个 `00 00 01 BA` 边界被提取

#### 场景：过短 PS 包被拒绝

- **WHEN** 一条 PS 包的长度小于头部声明值
- **THEN** 解包器返回错误，不返回截断帧

#### 场景：单个 PS 包产出一个 ES 帧

- **WHEN** 一条 PS 包恰好包含一个完整 PES 有效载荷
- **THEN** 解包器恰好产出一个带正确时间戳的 ES 帧

### 需求：PS 封装器将 ES 帧打包进 pack header

系统 MUST 将 ES 帧封装为 PS 包，包含：
- 系统头部 `00 00 01 BA`
- 携带正确 SCR 与 mux rate 的 pack header
- PES 头部 `00 00 01 E0`（视频）或 `00 00 01 C0`（音频），带 PTS/DTS
- 以 ES 帧为内容的 PES 有效载荷

PS 封装以字节级 golden test 锁定：封包的每一层（系统、包、PES）都要能被解包端还原。

#### 场景：一个 ES 帧经 PS 往返后无损

- **WHEN** 一个 ES 帧先封装再解包
- **THEN** 产出 ES 字节与原帧一致，PTS 保留

#### 场景：视频与音频流使用不同流 ID

- **WHEN** 一个视频 ES 帧被封装
- **THEN** PES 头部使用流 ID `0xE0`
- **WHEN** 一个音频 ES 帧被封装
- **THEN** PES 头部使用流 ID `0xC0`

### 需求：RTP payloader 将 PS 帧切片为 MTU 大小包

系统 MUST 将 PS 帧切片为 RTP 包，大小不超过可配置的 MTU（默认 1400 字节），在最后一个切片上设置 `marker bit = 1`，递增 sequence number，并依据帧的 PTS 以 90 kHz 域打 RTP timestamp 戳。

#### 场景：PS 帧切片为 MTU 大小包

- **WHEN** 一个 PS 帧被分包为 RTP
- **THEN** 每个 payload 至多为 MTU 大小，sequence number 严格递增，且帧的最后一个包携带 marker bit

#### 场景：切片包重组还原原始帧

- **WHEN** 切片自同一 PS 帧的 RTP 包被接收，可能乱序或重复
- **THEN** 按 sequence 排序去重后重组出字节一致的 PS 帧

### 需求：RTP 解包器按 SSRC 与 sequence 重组帧

系统 MUST 将 RTP 包按照 SSRC 与 payload type 分组、按 sequence number 排序并去重后，重组为 PS 帧。MUST 检测丢包并报告，但不阻塞后续帧。

#### 场景：乱序包正确重组

- **WHEN** RTP 包按 sequence 100、102、101 到达
- **THEN** 解包器缓冲 101，在 102 到达后重组出完整帧

#### 场景：重复包被忽略

- **WHEN** 两条相同 sequence number 的 RTP 包到达
- **THEN** 第二条被静默丢弃，不破坏已重组帧

#### 场景：缺失 sequence 上报间隙

- **WHEN** sequence 100 之后缺失 101
- **THEN** 解包器上报一个包的间隙，并从 102 继续重组

### 需求：PS 封装器在流首携带 PSM 声明编码

`PSPacketizer` MUST 在每条输出 PS 流的首个视频帧前插入一次 Program Stream Map（PSM，起始码 `00 00 01 BC`），声明本次流的编码类型；编码按首个视频 ES 帧的字节特征惰性判定，MUST NOT 依赖调用方显式传入：

- 视频起始码字节为 `0xB0`/`0xB3`/`0xB5`/`0xB6`（或 VOL `0x20`~`0x2F`）→ stream_type `0x10`（ISO/IEC 14496-2 Visual）
- 识别为 H.264 → `0x1B`；识别为 H.265 → `0x24`
- 音频为 AAC → `0x0F`

PSM 结构 MUST 符合 ISO/IEC 13818-1 §2.5.4，携带合法的 CRC-32/MPEG-2 校验值（CRC 覆盖从 program_stream_map_length 字段之后的第一个字节起至最后一个条目字节止）。PSM 对现有 PS 解包端 MUST 透明：不携带 PSM 识别能力的既有解包路径行为不变。

#### 场景：MPEG-4 源的 PS 流首携带 stream_type 0x10 的 PSM

- **WHEN** 一个 MPEG-4 Part 2 ES 帧（首起始码 `00 00 01 B6`）首次送入 PSPacketizer
- **THEN** 该帧的 PS 输出前出现一个 PSM，其 elementary_stream_map 声明 stream_id `0xE0`、stream_type `0x10`，且 CRC 校验通过

#### 场景：H.264 源声明 0x1B，且 PSM 每流仅一次

- **WHEN** 一个 H.264 源连续输出多帧
- **THEN** 仅第一帧的 PS 输出前有 PSM（stream_type `0x1B`），后续帧无 PSM

#### 场景：PSM 对既有解包端透明

- **WHEN** 带 PSM 的 PS 流送入既有 PS 解包器与 FLV 网关的 PS 解析路径
- **THEN** 帧切割与 ES 提取结果与无 PSM 时完全一致，PSM 被跳过不产出 ES 帧

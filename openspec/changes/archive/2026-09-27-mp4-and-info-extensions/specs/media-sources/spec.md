# Spec Delta

## ADDED Requirements

### Requirement: 本地文件源支持 mp4 容器（魔数嗅探，字节级不变）

当 `MediaConfig.Kind` 为 `file` 且 `Path` 指向的文件以 `ftyp` box（offset 4，ASCII `ftyp`）开头时，`FileSource` 必须（MUST）走 mp4 容器 demux 路径而非裸 Annex-B 流路径。非 mp4 文件必须（MUST）保持当前行为（直接返回底层文件的 `io.ReadCloser`，由 `StreamESReader` 以 `00 00 00 01` 起始码切割 NAL）。`MediaConfig` 不引入新的 `SourceKindMP4`；同一个 `file` kind 自动适配。

#### Scenario: .mp4 文件触发容器 demux

- **WHEN** `MediaConfig{Kind: "file", Path: "/tmp/v.mp4"}` 打开的文件前 12 字节为 `[0,0,0,<len>,'f','t','y','p',...]`
- **THEN** `FileSource.Open` 返回 `MP4Demuxer`；`MediaService.OpenSource` 从该 reader 产出 `model.ESFrame`，每帧带真实容器 PTS
- **AND** 非 mp4 文件（如 `.ps`、`.h264`、`.bin`）的返回值与本次变更前字节级一致

#### Scenario: 非 mp4 文件不受影响

- **WHEN** `FileSource.Open` 打开的文件不包含 `ftyp` box
- **THEN** 返回原始文件流，`StreamESReader` 继续以 `00 00 00 01` 切割 NALU，PTS 按 FPS 合成递增

### Requirement: mp4 容器解析产出 Annex-B 帧与容器 PTS

`MP4Demuxer` 必须（MUST）完成以下功能：
1. 解析 `moov/trak/stbl/stsd`，识别 `avc1` / `hvc1`（视频）与 `mp4a`（音频）sample entry。
2. 视频 sample：将 AVCC/HVCC 的 4 字节长度前缀 NALU 转换为 Annex-B `00 00 00 01` start code，逐 NALU 作为一帧输出，帧内无粘连。
3. 音频 sample（AAC）：从 `esds` atom 解析 AudioSpecificConfig，逐帧生成 7 字节 ADTS 头，原封不动拼接原始 AAC frame。
4. 根据 `stts/stsc/stsz/stco` 计算每个 sample 的 PTS（90 kHz 域），经 `model.ESFrameWithPTS` 挂载。
5. EOF 后自动循环回到首个 sample（文件结束不是流结束）。
6. 遇到不支持的编码（如 VP9、HE-AAC v2）或损坏的 box 时返回清晰错误而非 panic。

#### Scenario: H.264 AVCC→Annex-B 转换

- **WHEN** mp4 文件中一个 video sample 的原始 NALU 长度为 5，内容为 `[0,1,2,3,4]`
- **THEN** demuxer 输出一帧，字节为 `[0,0,0,1,0,1,2,3,4]`

#### Scenario: AAC 帧带 ADTS 头

- **WHEN** 容器包含 AAC sample，且 `esds` 解析出 ASC = `[0x12,0x10]`
- **THEN** 该 sample 输出为 7 字节 ADTS 头 + 原始 AAC frame 字节；ADTS 头中的 `syncword=0xFFF`、`profile`、`sampling_freq_index`、`channel_configuration` 与 ASC 匹配

#### Scenario: 容器 PTS 转换为 90 kHz 域

- **WHEN** 容器 track timescale = 90000，sample 的 decode time = 180000
- **THEN** 对应 ESFrame 的 PTS = 180000
- **AND** 当 track timescale = 30000 时，同 sample 的 PTS = 540000（`90000 * decodeTime / 30000`）

#### Scenario: EOF 循环回到首个 sample

- **WHEN** mp4 文件被读到最后一个 sample 后再次 Read
- **THEN** 返回文件第一个 sample 的字节与 PTS，不返回 io.EOF

### Requirement: 逐帧 PTS 的可选接口（port 层）

`port` 包必须（MUST）提供可选接口 `ESFrameReader`，其 `Read(ctx context.Context) (model.ESFrame, error)` 签名与 `ESReader` 相同。`MediaService.OpenSource` 必须（MUST）在包装 `StreamESReader` 前对 `src.Open` 返回的 reader 做类型断言；若 reader 实现 `ESFrameReader`，则直接作为出站 ES 帧流使用；否则走既有 FPS 合成 PTS 路径。该接口由 mp4 adapter 实现，domain/app 层不感知 mp4。

#### Scenario: mp4 source 直接供给 PS packetizer

- **WHEN** 打开一个 mp4 文件，`MP4Demuxer` 实现 `port.ESFrameReader`，且 `MediaService.OpenSource` 断言通过
- **THEN** 返回的 ESReader 即 demuxer 本身；后续 PS packetizer 读到的 ESFrame 的 PTS 与容器 sample 时间戳一致（90 kHz 域）

#### Scenario: 裸流文件照旧使用 FPS PTS

- **WHEN** 打开一个 `.h264` 裸流文件，reader 不实现 `ESFrameReader`
- **THEN** `MediaService.OpenSource` 返回 `StreamESReader`；PTS 按 `Clock / FPS` 步进递增，与本次变更前行为一致

### Requirement: golden test 锁定 mp4 的 Annex-B 输出与 PTS

系统 MUST 包含一个 golden test fixture：一个硬编码的最小 mp4 文件（至少包含一个 H.264 sample 与一个 AAC sample），断言 `MP4Demuxer` 产出的视频帧起始码为 Annex-B、AAC 帧带 ADTS 头、PTS 在 90 kHz 域为预期值。测试数据以 `testdata/minimal.mp4` 或硬编码 `[]byte` 存放，不依赖外部资源。

#### Scenario: 字节级 golden test 通过

- **WHEN** 测试读取 `testdata/minimal.mp4` 并通过 `MP4Demuxer` 产出所有帧
- **THEN** 视频帧第一个字节为 `0x00 0x00 0x00 0x01`；最后一帧 PTS 等于容器中最后一个 video sample 的 90 kHz 时间戳
- **AND** 修改 Annex-B 转换逻辑会导致 golden 断言失败，保护 demux 字节级精确性

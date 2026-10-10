# Spec Delta: media-sources

## ADDED Requirements

### Requirement: mp4 容器支持 MPEG-4 Part 2 视频轨道

`MP4Demuxer` MUST 支持 `mp4v`（MPEG-4 Part 2 Visual）sample entry，不再将其作为不支持的编码拒绝：

1. 解析 `mp4v` sample entry 的 `esds` box，提取 DecoderSpecificInfo（tag 0x05）作为轨道 config；config 内容为 VOS/VO/VOL 头序列，自带 `00 00 01` 起始码，原样作为参数集使用。
2. 视频样本是完整 VOP byte-stream（非 length-prefixed NALU），MUST 原样输出，不做 AVCC→Annex-B 长度前缀重写。
3. 每个循环的第一个视频样本前 MUST 前置一次轨道 config。
4. 样本首部若不含 `00 00 01` 起始码，MUST 在其前补 VOP 起始码 `00 00 01 B6`。
5. 循环回卷（`Loop`）语义与既有视频轨道一致：回卷后重新从带 config 前置的首样本开始。
6. 无 `esds` box 或 DecoderSpecificInfo 为空的 `mp4v` 轨道 MUST 产生清晰错误（跳过该轨道），不 panic。

#### Scenario: mp4v 轨道正常解复用

- **WHEN** 打开一个含 `mp4v` 视频轨道（带 esds DecoderSpecificInfo）的 mp4 文件
- **THEN** 第一个视频帧以 VOS/VO/VOL config 字节开头，后续帧为各样本原始字节；每帧 PTS 为容器 sample 的 90 kHz 时间戳

#### Scenario: 样本缺失 VOP 起始码被补齐

- **WHEN** 某视频样本首 4 字节不是 `00 00 01` 起始码
- **THEN** 输出帧在该样本前补 `00 00 01 B6`，其余字节不变

#### Scenario: 循环回卷重新前置 config

- **WHEN** mp4v 文件读到最后一个样本后再次 Read（Loop 开启）
- **THEN** 新一轮首帧再次以 config 字节开头，与第一轮逐字节一致

#### Scenario: 缺失 esds 的 mp4v 轨道被拒绝

- **WHEN** `mp4v` sample entry 不含 `esds` box
- **THEN** 解析返回清晰错误信息并跳过该轨道，进程不崩溃

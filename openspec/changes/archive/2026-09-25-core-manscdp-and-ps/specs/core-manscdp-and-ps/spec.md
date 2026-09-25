# core-manscdp-and-ps Specification

## Purpose
`core-manscdp-and-ps` 是 GB/T 28181 的协议内容层：MANSCDP XML 消息的编解码，以及 PS↔RTP 双向转封装。它让"能解析命令"成为"能执行命令"，为后续级联、动态目录、云台控制等能力提供可复用的协议基础。

## ADDED Requirements

### Requirement: MANSCDP XML encode/decode round trips for full command set

The system SHALL encode and decode the following MANSCDP command types without data loss:
- DeviceInfo Query / Response
- RecordInfo Query / Response
- Alarm Notify / Ack
- ConfigDownload Command / Ack
- PTZControl / Telemetry
- Preset Query / Set

编解码结果必须通过字节级 golden test 锁定；未知命令类型必须返回可解析结果而不报错。

#### Scenario: DeviceInfo query round trip

- **WHEN** a DeviceInfo request is encoded then decoded
- **THEN** every field (DeviceID, SN, StartTime, EndTime, etc.) survives exactly

#### Scenario: RecordInfo response with optional fields

- **WHEN** a RecordInfo response contains optional fields (Video, Audio, Summary)
- **THEN** decoding yields the same set of optional fields without loss

#### Scenario: Alarm notify without prior registration

- **WHEN** an Alarm notify arrives for an unregistered DeviceID
- **THEN** decoding still succeeds and yields a valid Notify message

#### Scenario: PTZ control preserves speed and direction

- **WHEN** a PTZ command carries speed values in 1–255 and direction bits
- **THEN** encoded bytes preserve the exact numeric range and bit layout

#### Scenario: Preset set acknowledges with the preset index

- **WHEN** a Preset set command is sent with preset index 1–255
- **THEN** the response contains the same preset index in its SN or Result

#### Scenario: Unknown command type is parseable

- **WHEN** a MANSCDP body contains an unrecognized CmdType
- **THEN** decoding returns a generic Notify without error

### Requirement: PS depacketizer splits frames by pack header

The system SHALL depacketize a PS stream by locating `00 00 01 BA` start codes and using the pack header length to cut ES frames. It SHALL handle MPEG-2 Video and MPEG-1/2 Audio PES payloads without external boundaries.

PS 解包以 `00 00 01 BA` 为帧边界，不依赖外部注入的 marker。

#### Scenario: A contiguous PS stream is depacketized correctly

- **WHEN** a PS stream containing multiple pack headers is fed to the depacketizer
- **THEN** ES frames are extracted exactly at each `00 00 01 BA` boundary

#### Scenario: A short PS packet is rejected

- **WHEN** a PS packet is shorter than the header declares
- **THEN** the depacketizer returns an error and does not return truncated frames

#### Scenario: A single PS packet yields one ES frame

- **WHEN** one PS packet contains exactly one complete PES payload
- **THEN** the depacketizer yields exactly one ES frame with the correct timestamp

### Requirement: PS packetizer wraps ES frames into pack headers

The system SHALL encapsulate ES frames into PS packets with:
- System header `00 00 01 BA`
- Pack header with correct SCR and mux rate
- PES header `00 00 01 E0` (video) or `00 00 01 C0` (audio) with PTS/DTS
- PES payload as the ES frame

PS 封装以字节级 golden test 锁定：封包的每一层（系统、包、PES）都要能被解包端还原。

#### Scenario: One ES frame survives a PS round trip

- **WHEN** an ES frame is packetized then depacketized
- **THEN** the resulting ES bytes equal the original frame and the PTS survives

#### Scenario: Video and audio streams use different stream IDs

- **WHEN** a video ES frame is packetized
- **THEN** the PES header uses stream ID `0xE0`
- **WHEN** an audio ES frame is packetized
- **THEN** the PES header uses stream ID `0xC0`

### Requirement: RTP payloader splits PS frames into MTU-sized packets

The system SHALL slice PS frames into RTP packets no larger than a configurable MTU (default 1400 bytes), set `marker bit = 1` on the last slice, increment sequence numbers, and stamp the RTP timestamp from the frame's PTS in 90 kHz domain.

#### Scenario: A PS frame slices into MTU-sized packets

- **WHEN** a PS frame is packetized into RTP
- **THEN** each payload is at most the MTU, sequence numbers are strictly increasing, and the last packet of the frame carries the marker bit

#### Scenario: Sliced packets reassemble to the original frame

- **WHEN** RTP packets that sliced one PS frame are received, possibly out of order or with duplicates
- **THEN** they reassemble to the byte-identical PS frame, ordered and deduplicated by sequence

### Requirement: RTP depayloader reassembles frames by SSRC and sequence

The system SHALL reassemble RTP packets back into PS frames grouped by SSRC and payload type, ordered by sequence with deduplication. It SHALL detect packet loss and report it without blocking subsequent frames.

#### Scenario: Out-of-order packets reassemble correctly

- **WHEN** RTP packets arrive with sequence numbers 100, 102, 101
- **THEN** the depayloader buffers 101 and reassembles the frame after 102 arrives

#### Scenario: Duplicate packets are ignored

- **WHEN** two RTP packets with the same sequence number arrive
- **THEN** the second is silently dropped and does not corrupt the reassembled frame

#### Scenario: Missing sequence reports a gap

- **WHEN** sequence number 101 is missing after 100
- **THEN** the depayloader reports a one-packet gap and continues reassembling from 102

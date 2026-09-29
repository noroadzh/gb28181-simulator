# Spec Delta

## Purpose

`media-sources` 是 GB28181 的媒体承载层：四种媒体源（本地文件、RTSP、HLS、
合成图）的统一抽象，以及 PS↔RTP 双向转封装管道。它让"能描述媒体"成为
"能产出媒体"——为后续的点播（INVITE）、目录媒体信息提供可消费的流。

## ADDED Requirements

### Requirement: Four media sources share one abstraction

The system SHALL expose a single `MediaSource` interface whose purpose is to
continuously produce elementary-stream (ES) frames carrying presentation
timestamps, independent of the downstream transport. The system SHALL provide
four implementations of that interface — local file, RTSP, HLS and a synthetic
source — each an isolated adapter with no knowledge of PS or RTP.

媒体源只负责产出"带时间戳的 ES 帧流"，四种来源靠同一个接口暴露，和承载层
（PS/RTP）正交。

#### Scenario: Each source opens and yields ES frames

- **WHEN** a configured media source is opened
- **THEN** it produces ES frames in order, each with a presentation timestamp
- **AND** a source that cannot be read reports a clear error rather than
  silently hanging

#### Scenario: A source closes once

- **WHEN** a media source is closed
- **THEN** its underlying resource (file handle, RTSP session, HLS segment
  reader) is released and further reads return an error

#### Scenario: Sources differ only in origin, not in shape

- **WHEN** a file, an RTSP session, an HLS playlist and the synthetic source
  are each opened
- **THEN** each yields the same `ESFrame` shape; the consumer does not need
  to know which source produced a frame

#### Scenario: The synthetic source is reproducible

- **WHEN** the synthetic source is opened with the same seed twice
- **THEN** it produces byte-identical ES output both times, so e2e and golden
  tests are deterministic and offline

### Requirement: PS encapsulation and depacketization are byte-exact

The system SHALL encapsulate ES frames into program-stream (PS) packets —
system start code `00 00 01 BA`, pack header, pack start code `00 00 01 BB`
or `00 00 01 E0`, PES header and payload — and SHALL depacketize a PS stream
back to ES by locating `00 00 01 BA` and using the pack header length to cut
frames, without relying on an external boundary. Both directions SHALL be
covered by byte-level golden tests.

PS 封装/解封装以字节级 golden test 锁定，封包的每一层（系统、包、PES）都要能
被解包端还原。

#### Scenario: A frame survives a round trip

- **WHEN** an ES frame is packetized then depacketized
- **THEN** the resulting ES bytes equal the original frame, and the
  presentation timestamp survives

#### Scenario: A PS stream is cut by its own headers

- **WHEN** a contiguous PS stream is depacketized
- **THEN** each frame boundary is derived from the `00 00 01 BA` start code
  and the pack header length, not from any injected marker

### Requirement: RTP packetization and reassembly are byte-exact

The system SHALL slice a PS frame into RTP packets no larger than a configurable
MTU (default 1400 bytes), set `marker bit = 1` on the last slice of a frame,
increment sequence numbers, and stamp the RTP timestamp from the frame's PTS
(90 kHz domain). It SHALL reassemble RTP packets back into a PS stream grouped
by SSRC and payload type, ordered by sequence with deduplication. Both
directions SHALL be covered by byte-level golden tests.

RTP 分包/收包以字节级 golden test 锁定：分包时的 marker、sequence、timestamp
与收包重组的顺序、去重都要可复现。

#### Scenario: A PS frame slices into MTU-sized packets

- **WHEN** a PS frame is packetized into RTP
- **THEN** each payload is at most the MTU, sequence numbers are strictly
  increasing, and the last packet of the frame carries the marker bit

#### Scenario: Sliced packets reassemble to the original frame

- **WHEN** RTP packets that sliced one PS frame are received, possibly out of
  order or with duplicates
- **THEN** they reassemble to the byte-identical PS frame, ordered and
  deduplicated by sequence

### Requirement: MediaService provides an outbound pipeline entry

The system SHALL provide a `MediaService` that composes a media source with the
PS packetizer and the RTP packetizer into an outbound pipeline, and exposes
`Open/Close/Read` semantics so a caller can pull RTP packets from a live media
source. A media-source failure SHALL be reported through the node lifecycle
without halting signalling.

`MediaService` 是出站管道的入口：把源、PS 封装、RTP 分包串起来，对外暴露
开/关/读；源失败只报错到节点生命周期，不中断信令。

#### Scenario: Open wires a configured source into the pipeline

- **WHEN** `Open` is called with a source name and a transport config
- **THEN** a `MediaService` pipeline is created: source → PS → RTP
- **AND** `Read` yields RTP packets with increasing sequence numbers and the
  source's PTS as the RTP timestamp

#### Scenario: A failing source does not kill the node

- **WHEN** the underlying media source cannot be opened or errors on read
- **THEN** `Read` returns the error and the node is reported as having a media
  fault, while signalling lifecycle is unaffected

#### Scenario: Close releases the pipeline exactly once

- **WHEN** `Close` is called on an open `MediaService`
- **THEN** the source is closed, the pipeline is torn down, and further reads
  return a closed error

## MODIFIED Requirements

（无。既有 `node-abstraction`、`device-node`、`platform-large-node`、
`platform-small-node` 的能力均不与媒体承载层相交。）

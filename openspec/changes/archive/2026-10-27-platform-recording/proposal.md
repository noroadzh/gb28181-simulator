# Proposal

## Why

平台现在能拉取下级设备的实时流（#41），但拉下来的画面流过即忘：没有落盘，没有录像库，也没有任何「查录像」「看回放」的平台能力。真实平台的价值一半在录像——上级平台会向它发 RecordInfo 查询与回放 INVITE。本 change 让模拟平台把拉到的流按时间分段写入磁盘，并能以 HTTP 与 SIP 两种方式查询和回放这些录像。

路线图编号：#42 `platform-recording`（#41 归档后的第一项）。

## What Changes

- **录制**：平台实时会话建立时自动开始录制，把 PS 流按时间段写入分段文件；会话结束、BYE、节点停止时封段。
- **录像库**：按 (节点, 通道, 时间窗) 查询分段；分段元数据由文件名携带（通道 + 起止时间），启动时扫描目录重建索引。
- **HTTP 查询与回放**：新增录像列表端点；新增按录像回放为 FLV 的端点（PS 分段 → FLV，复用现有管线）。
- **SIP 查询**：平台节点收到指向自身通道的 RecordInfo 查询时，从录像库应答（设备节点保持现有的固定录像行为不变）。
- **SIP 回放**：平台节点收到 `s=Playback` 的 INVITE 时，从录像库取对应时间窗的录像，以 PS/RTP（UDP 或附录 D TCP）发给请求方，并按窗口结束停流。
- **配置**：`recording:` 段（enabled、目录、分段时长），默认关闭？——默认开启但目录取自 storage 路径，避免静默丢数据。

## Capabilities

### New Capabilities

- `platform-recording`: 平台拉流的分段录制、录像库索引与时间窗查询，以及面向 HTTP 与 SIP 的回放数据源。

### Modified Capabilities

- `channel-http-api`: 新增平台录像的列表与 FLV 回放端点。
- `platform-large-node`: 平台作为 UAS 时从录像库应答 RecordInfo 查询与回放 INVITE。

## Impact

- `internal/domain/port`：新增录像库端口（写入、查询、打开读数）。
- `internal/adapter/recording`：文件系统实现（分段写、目录扫描、PS 分段读取）。
- `internal/app`：录制挂到实时会话；平台 RecordInfo / 回放 INVITE 的分支；HTTP 服务方法。
- `internal/app/streaming`：从 PS 文件生成 FLV（复用 ParsePS 管线）。
- `internal/interface/http`：两个新端点与错误语义。
- 配置：`recording.enabled` / `recording.dir` / `recording.segment_seconds`。

## Non-goals

- 不录制「上级平台推给本平台的流」（入站 INVITE 的媒体仍丢弃）；录像来源只有平台自己的拉流会话。
- 不做转码：录制内容就是设备发来的 PS 原码流。
- 不做自动清理/容量上限（保留策略留待后续）。
- 不做报警录像、录像下载（文件下载）与倍速回放的 SIP 侧协商增强。
- 不改设备节点的录像查询行为（仍返回既有的固定条目）。

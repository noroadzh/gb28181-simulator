# Proposal

对应路线图阶段编号：**#8 `media-sources`（共 15 阶段）**。本 change
交付"四种媒体源统一抽象 + PS↔RTP 双向转封装管道"，该阶段没有拆分计划，
一次 change 完成。

## Non-goals

- SIP INVITE 点播信令处理——路线图 #9。本 change 只产出媒体承载层（管道），
  不涉及会话协商或信令路由。
- H.264/H.265 编解码——只做透传（ES 帧直接封装进 PS，不做解码或转码），
  不引入 ffmpeg 等重型依赖。
- Web 播放器——#14。
- 抓包导出（pcap）——#13 `exception-and-capture`。
- 级联转发头（`X-RoutePath` / `X-PreferredPath`）——#9。
- 录制/回放业务语义（录像文件管理、时间范围查询）——后续能力，
  本次只提供"出站管道 + 入站落盘"最小闭环。

## Why

现有代码在媒体层是真空的：

- `internal/domain/model/session.go` 的 `MediaStream` 只描述 SDP 的 `m=` 行
  （media type / port / protocol / formats），没有任何帧、包、时间戳或编解码
  信息。
- `internal/adapter/sdp/` 能解析/序列化 SDP，但 SDP 一旦协商完，就没有东西
  去消费 `m=video 0 RTP/AVP 96` 所约定的承载。
- 没有 `internal/adapter/media/` 目录，没有 PS、RTP、媒体源的任何实现。

这导致 GB28181 点播（INVITE）无法真正发出媒体流，上级平台查询目录后
看到设备列表却拉不到画面。本次 change 补齐承载层：从"能描述"到"能产出"。

## What Changes

- **四种媒体源统一抽象。** 新增 `domain/port/media.go` 的 `MediaSource` 接口，
  提供 `Open/Close/Read` 语义；四种实现（本地文件、RTSP、HLS、合成图）各自
  是独立的 `internal/adapter/media/` 包，不依赖具体下游。
- **PS 封装/解封装。** 新增 `PSPacketizer` / `PSDepacketizer` 端口族。
  出站：ES 帧（H.264/H.265）按系统层/包层/ PES 层封装进 PS 包；
  入站：PS 流按 `00 00 01 BA` 起始码 + pack header 长度截帧，还原 ES。
- **RTP 分包/收包重组。** 新增 `RTPizer` / `RTPDeizer` 端口族。
  出站：PS 包按 MTU（默认 1400 bytes）切 RTP payload，设置 marker bit
  表示帧边界；入站：RTP 包按 SSRC + sequence 重组，还原 PS 流。
- **domain model 扩展。** 新增 `PSFrame`、`RTPPacket`、`ESFrame`、
  `MediaConfig`，与 SDP 模型正交。
- **app 层编排。** 新增 `MediaService`，连接 `NodeService` 生命周期，
  提供 `Open/Close/Read` 语义，作为出站管道的入口。
- **字节级 golden test。** PS 封包/解包、RTP 分包/收包均含预录样本的
  字节级断言；出站 + 入站 e2e 闭环均做端到端字节校验。

## Capabilities

### New Capabilities

- `media-sources`: 四种媒体源统一抽象 + PS↔RTP 双向转封装管道。

### Modified Capabilities

（无。现有 `node-abstraction`、`device-node`、`platform-large-node`、
`platform-small-node` 的需求均不涉及媒体承载层。）

## Impact

- `internal/domain/model/`：新增 `media.go`（PSFrame / RTPPacket / ESFrame）。
- `internal/domain/port/`：新增 `media.go`（MediaSource / PSPacketizer /
  PSDepacketizer / RTPizer / RTPDeizer）。
- `internal/adapter/media/`：新增八个 adapter 包
  （file / rtsp / hls / synthetic + ps_packetizer / ps_depacketizer /
  rtpizer / rtp_deizer）。
- `internal/app/`：新增 `media_service.go`（编排层）。
- `cmd/gb28181-simulator/main.go`：在节点启动时装配媒体管道。
- `README.md`、`docs/architecture.md`：补媒体源与转封装管道章节。

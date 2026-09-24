# Design

## Context

见 `proposal.md` 的 Why。实现前的相关现状：

- `internal/domain/model/session.go` 只有 `MediaStream`（SDP `m=` 行语义），
  无帧/包/时间戳概念。
- `internal/adapter/sdp/` 能解析/序列化 SDP，含 GB28181 `y=`（SSRC）与
  `f=` 扩展，但只到会话描述为止。
- `media` 相关实现为零：无 `internal/adapter/media/` 目录，无 PS/RTP/源。
- 分层纪律：`app` 只依赖 `domain/port`，绝不 import `internal/adapter`；
  媒体实现必须全部落在 `internal/adapter/media/`。
- 纯 Go、禁止 CGO；PS/RTP 字节格式任务必须含 golden test（字节级断言）。

## Goals / Non-Goals

**Goals:**

- 一次性交付四种媒体源抽象 + 双向转封装管道，让"能描述媒体"变成"能产出媒体"。
- 出站与入站都形成闭环，并有字节级 golden test。

**Non-Goals:**

- 编解码（H.264/H.265 只透传）。
- SIP INVITE 信令处理（#9）。
- Web 播放（#14）、抓包导出（#13）。

## Decisions

### 1. 单 change，不拆分

媒体承载层是后续所有媒体能力的地基，四种源与 PS/RTP 强耦合——合成图源产出
一个 ES 帧，必须能立即封进 PS、切成 RTP，否则无从验证。拆开会导致每个小
change 交付的都是无法独立验证的半成品。

- 结论：`media-sources` 一个 change 完成。

### 2. 端口族放在 domain/port，模型放 domain/model

新增 `domain/port/media.go`（MediaSource / PSPacketizer / PSDepacketizer /
RTPizer / RTPDeizer）与 `domain/model/media.go`（PSFrame / RTPPacket /
ESFrame / MediaConfig）。`app` 的 `MediaService` 只依赖这些接口与值类型，
通用实现全部在 `internal/adapter/media/`。

- 理由：与既有 `transport.go` / `codec.go` / `manscdp.go` 端口族一致，
  且遵守"app 只依赖 domain/port"的分层纪律。

### 3. 源与管道正交：MediaSource 只产出 ES 帧流

`MediaSource` 的职责是"持续产出 ES 帧（含时间戳/PTS）"，不关心下游是
PS 还是 RTP。四种源都实现同一个接口，上游管道（PSPacketizer → RTPizer）
对源类型无感知。

```go
type MediaSource interface {
    Open(ctx context.Context) (io.ReadCloser, error)
    Close() error
    Config() MediaConfig
}
```

- 备选：源直接产 RTP。取舍：源与承载层职责耦合，且入站解封装后用不上。
- 结论：源只产 ES。

### 4. PS 帧边界检测：系统起始码 + pack header 长度

出站封装：每个 PS 包 = 系统层起始码 `00 00 01 BA` + pack header（含
`program_mux_rate` 与长度字段）+ 包层起始码 `00 00 01 BB/E0` + PES 头 +
ES payload。入站解封装：扫描 `00 00 01 BA`，读 pack header 中的高 22 位
`program_mux_rate` 求出本包长度，据此截帧，不依赖上层提供边界。

- 理由：O(n) 单次扫描，固定小常数，纯字节操作，满足"PS 字节级 golden"约束。
- 结论：起始码 + header 长度截帧。

### 5. RTP 打包：一个 PS 包切分 + marker bit + SSRC

出站：每个 PS 帧按 MTU（默认 1400 bytes）切分为多个 RTP payload，
`marker bit = 1` 表示最后一个分片（PS 帧边界），sequence 递增，SSRC 固定
（来自 SDP `y=` 行，或源配置）。入站：按 (SSRC, payload type) 分组，
按 sequence 重组出 PS 流。

- 结论：MTU 分包 + marker + 固定 SSRC。

### 6. 合成图源默认测试源，文件/RTSP/HLS 各带最小可行实现

合成图源（`synthetic_source.go`）产出可复现的递进 H.264 ES 样本，不依赖
外部文件，是 e2e 与 golden 的默认源。文件源读取预录制 `testdata/*.ps` /
`.264` 样本；RTSP 与 HLS 源用内置客户端（纯 Go）拉取最小流。四种源共用
同一个 `MediaSource` 接口。

- 理由：保证 e2e 可离线、可重复，CI 不依赖外网。
- 结论：合成图为主测源，其余三种最小可行实现。

### 7. MediaService 作为出站入口，NodeService 生命周期连接

`app.MediaService` 持有 domain/port 依赖，暴露 `Open/Close/Read`；
`NodeService` 启动某节点时根据其媒体配置创建对应源并挂到管道。
`MediaService` 只依赖 `domain/port`，不感知是哪个 adapter。

### 8. 时间戳：ES 帧自带 PTS，RTP timestamp 由它换算

ES 帧携带 `PTS`（90kHz 时钟域），PS PES 头写 PTS，RTP payload 头用该
PTS 作为 timestamp，保证出站与入站时间对齐。合成图源按固定帧率（默认
25fps）注入 PTS。

## Risks

- RTSP / HLS 源依赖外部实现质量（bluenviron/gortsplib、bluenviron/m3u8）。
  缓解：本次源实现只做最小拉流与解析，失败时明确报错交给上层，不做
  重连/纠错（后续 #10 dynamic-sim-features 再补）。
- PS/RTP 字节级细节易错。缓解：每种编解码各配 golden test，用预录样本
  锁字节。

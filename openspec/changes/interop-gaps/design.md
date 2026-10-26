# Design

## Context

设备侧已有对讲（音频）与回放（时间窗）两条 INVITE 路径，都以「每对话一个 UDP 端口 + BYE 释放」实现；媒体管道 `MediaService.PacketizeOutbound` 能把任意媒体源打成 PS/RTP。本项只是把**实时视频**接到同一套模式上。

## Decisions

### Decision 1: 设备分支按 SDP 分派

`p.isDevice` 时：body 含 `s=Playback` → 回放；`m=audio` → 对讲；`m=video` → 实时视频；其余 488。顺序固定，避免回放（也是 video）被当成直播。

### Decision 2: 通道来自 To 的 user 部分

实时 INVITE 没有 `u=` 行；国标约定 `To: <sip:<channelID>@…>`。取 To 的 user，为空则 400。媒体源仍按「通道级优先、节点级兜底」解析，无源 404。

### Decision 3: 复用 PacketizeOutbound 与每对话端口

应答里给出本节点新开的 UDP 端口（`a=sendonly`、`PS/90000`），随后把源持续打到 offer 的 `c=` + `m=video` 端点。BYE 关闭该端口，写失败即结束发送。

### Decision 4: 文件源需要有 loop

实时语义要求源不结束；`file` 源未开 `loop` 时会在文件末尾自然停流（端口关闭、对话留在应用层）。这是配置问题而非错误，文档写明；合成源永不结束。

## Risks / Trade-offs

- **R1 真实平台可能要求 UDP 之外的传输**（TCP/TLS 媒体）→ 本项只做 UDP；附录 D 与 SRTP 在 #40 的后续任务。
- **R2 编码必须是平台可解的 PS/96** → 设备发送的是源的原编码，不做转码；与现有直播 FLV 一致。
- **R3 同一通道被多路拉流** → 每对话独立端口，互不影响；源被多次打开（文件源会重复读），可接受。

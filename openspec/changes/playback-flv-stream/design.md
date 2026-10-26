# Design

## Context

见 `proposal.md`。可复用的现有件：INVITE 事务（`talk_transactions.go`：应答表 + 超时）、FLV muxer/gateway（`internal/app/streaming`）、本地文件媒体源与 PS/RTP 打包（`internal/adapter/media`）、`port.PlaybackPort` 的会话模型。

## Decisions

### Decision 1: 回放 INVITE 用标准 SDP 时间窗语义

请求体：`s=Playback`、`u=<deviceID>:<sn>`、`t=<start> <end>`、一条 `m=video`（PS/96）、`c=` 为平台地址、平台 RTP 端口。设备从 `t=` 读取时间窗，而不是从 MANSCDP body 里再解析一遍——这是 GB/T 28181 对历史回放的约定，也让抓包一眼能看出是回放。

### Decision 2: 设备侧按时间窗定位，用现有文件源 + PS/RTP

设备把通道媒体源（本地文件）定位到 `start` 对应的关键帧，然后按 `end` 停止。定位精度取决于 demuxer 是否暴露时间戳：现有 `file` 源顺序读取，若无法按时间 seek，则退化为「从头开始并快速丢弃到 start」（正确但慢），并在日志与文档中写明。

### Decision 3: 平台侧以 sn 为键复用 FLV 网关

回放会话与直播共用 `streaming.Gateway` 的 muxer，但订阅键改成 `node|channel|sn`，避免与直播流互相覆盖。`/playback/flv?sn=` 只在该键存在且接收到首帧后返回 200；首帧未到时 404（与「未知会话」区分：文档写明先 play 再取流）。

### Decision 4: 结束条件三选一

`end` 到达、收到 BYE/TEARDOWN、节点停止——任一满足即停止发送、释放 RTP 端口、删除会话。与对讲的回收语义保持一致。

## Risks / Trade-offs

- **R1 文件源无法按时间 seek** → 退化为顺序丢弃，回放启动可能变慢；文档写明。
- **R2 首帧未到时取流** → 404 会被理解为「会话不存在」；端点文档明确「play 成功后再取流」，并考虑首帧等待上限（5s）。
- **R3 FLV 会话与直播键冲突** → 键里带 sn，互不影响。

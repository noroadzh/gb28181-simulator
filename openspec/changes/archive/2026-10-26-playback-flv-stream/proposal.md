# Proposal

## Why

回放目前只有控制面：`Playback` 下发 PlaybackControl MANSCDP，FLV 网关只服务直播（`/v1/flv/:id/:ch`）。`channel-http-api` 早已写明 `GET /playback/flv?sn=` 必须返回历史流，但该端点不存在，设备侧也没有任何「按时间段把录像变成 RTP」的实现。现场排查录像问题时，操作员能查到列表、能按播放键，却看不到画面。

## What Changes

- **回放 INVITE**：`play` 动作在拿到录像条目后向设备发 INVITE，SDP 使用 GB/T 28181 历史回放语义（`s=Playback`、`u=<deviceID>:<sn>`、`t=<start> <end>`），并带上平台侧的 RTP 接收端口。
- **设备侧回放源**：`device` 节点受理回放 INVITE，按 `t=` 的时间窗从通道媒体源（本地文件）定位并从该偏移开始，把 ES 打成 PS/RTP 发往平台端口；到达 `end` 或 TEARDOWN 时停止。
- **平台侧接流**：回放 INVITE 的 200 OK 之后，平台在协商端口上收 RTP，复用现有 FLV 网关与 muxer 转成 FLV。
- **HTTP 端点**：`GET /v1/nodes/:id/channels/:ch/playback/flv?sn=<sn>` 返回该回放会话的 FLV；未知/已结束会话 404。

**BREAKING**：无。`play` 不带时间窗时保持现有「只下发控制、不起流」的行为。

## Capabilities

### Modified Capabilities

- `channel-http-api`：新增回放 FLV 端点的契约（会话绑定、404、流格式）。
- `device-node`：新增「device 受理回放 INVITE 并按时间窗供流」要求。

对应路线图编号：#37 `playback-flv-stream`。

## Impact

- `internal/app/talk_transactions.go` 同款事务表复用（INVITE/应答）
- `internal/app/talk_call.go` 的会话模式（订阅表、回收）可借鉴
- `internal/app/streaming`（FLV muxer / gateway 需支持按 sn 的会话）
- `internal/adapter/media`（文件源按时间偏移读取、PS/RTP 复用）
- `internal/interface/http`（回放 FLV 路由）
- 测试：app 单测 + HTTP 契约 + e2e

## Non-goals

- 不做跨设备/跨平台的录像定位（只服务本进程节点的本地文件源）。
- 不做倍速转码（scale 只影响 PlaybackControl，不改变发送速率）。
- 不实现 GB35114 加密回放。
- 不实现边下边存（download）与断点续传。

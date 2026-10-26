# Proposal

## Why

模拟器已经能和真实平台交换注册、心跳、目录与级联信令，但在**真正要画面**的那一步断了：设备节点对非音频 INVITE 一律回 488（`acceptor_invite.go`），而真实国标平台看视频正是向设备发 video INVITE。这使「模拟出来的摄像头」无法被任何真实平台点播，与项目定位「与真实平台/设备无差异互通」直接冲突。同一轮盘点还发现 GB35114 未接线、TCP 媒体与 CANCEL 未实现——本 change 先解决最关键的设备视频 INVITE，其余登记为后续任务。

## What Changes

- **设备受理实时视频 INVITE**：`device` 节点对带 `m=video` 的 INVITE 以 200 OK + SDP（`c=` 设备地址、`m=video` 设备端口、`a=sendonly`、`PS/90000`）应答，并从该通道的媒体源持续发送 PS/RTP 到 offer 声明的平台端点，直到 BYE 或节点停止。
- **通道解析**：直播 INVITE 的通道取自 `To` 头的 user 部分（`sip:<channelID>@…`）。
- **无源语义**：通道没有媒体源时 MUST 回 404，而不是 200 后静默。
- **不再 488**：带 video m-line 的 INVITE MUST NOT 被 405/488 拒绝。

**BREAKING**：无。此前 video INVITE 一律 488，现在开始服务。

## Capabilities

### Modified Capabilities

- `device-node`：新增「device 受理实时视频 INVITE」。

对应路线图编号：#40 `interop-gaps`（首项：设备视频 INVITE；GB35114 接线、TCP 媒体、CANCEL 为同 change 的后续任务）。

## Impact

- `internal/app/acceptor_invite.go`（设备分支增加视频路径；SDP offer 解析）
- 复用 `MediaService.PacketizeOutbound`（源 → PS → RTP）与对讲/回放同一套「每对话一个 UDP 端口 + BYE 释放」模式
- 测试：app 单测（200/404/流到达/BYE 停）

## Non-goals

- 不在本项内做 GB35114、TCP 媒体、CANCEL（同 change 后续任务）。
- 不做按需转码：设备发送的是通道媒体源的原编码（PS/96）。
- 不支持视频 INVITE 的时间窗（那是历史回放，已有独立路径）。

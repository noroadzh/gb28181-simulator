# Proposal

## Why

`channel-http-api` 要求语音对讲建立 SIP INVITE（PCMU）会话并返回 WSS 地址、要求快照返回最近一帧 JPEG（或 503），但两者当前分别返回 `ErrTalkUnsupported` / `ErrSnapshotUnsupported`。它们是 WebUI 通道详情页最后两个不可用的操作。

## What Changes

- **语音对讲**：实现 `TalkStart` / `TalkStop`，经 SIP INVITE/SDP 建立音频会话，返回 session id 与浏览器 WebSocket 地址；对端 BYE 或超时后会话回收。
- **快照抓图**：实现 `Snapshot`，优先从通道媒体源的最近关键帧解码 JPEG；无媒体源返回 503，平台无抓拍子系统时保持现有 202 排队语义。
- **错误语义**：对讲与抓拍在能力缺失/对端不可达时返回可辨识错误，不再一律 501。

**BREAKING**：无（现有行为本就是 501）。

## Capabilities

### Modified Capabilities

- `channel-http-api`：补充对讲会话生命周期与快照来源选择的错误语义。

对应的路线图编号：#35（原 `channel-control-signalling` 的另一半）。

## Impact

- `internal/app/node_service.go`（TalkStart/TalkStop/Snapshot）
- `internal/adapter/media`（关键帧提取/JPEG 编码）
- `internal/interface/http`（WSS 音频通道与状态码）
- 测试：app 单测 + HTTP 契约 + e2e

## Non-goals

- 不实现 PTZ / 录像 / 回放（见 `channel-ptz-and-playback`）。
- 不实现 GB35114 加密对讲。
- 不实现多路并发对讲的混音。

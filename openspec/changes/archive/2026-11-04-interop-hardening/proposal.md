# Proposal

## Why

覆盖分析与真实平台联调风险表明：核心章节「表面齐全」，但若干控制面只有 ACK/日志、没有会话或状态副作用——入站 `PlaybackControl` 不驱动回放、设备侧 PTZ 不更新姿态、光圈/预置位与 RecordCmd/IFrame 等子类型缺失或 501。这会导致对接方判定「半通」。在 #45 文档同步之后（或并行），用本 change 加深控制语义，而不是扩展新的媒体栈。

路线图编号：#46 `interop-hardening`。

## What Changes

- **入站 PlaybackControl**：解析 Play/Pause/Teardown/Scale，驱动与 MANSRTSP INFO 共用的回放会话状态；非法参数 `400`，无会话的 pause/teardown 可辨识失败（与现有 HTTP 501/会话语义对齐）。
- **设备侧 PTZ 状态**：受理 DeviceControl PTZ 后更新节点/通道的 pan/tilt/zoom（及可选 iris）模拟状态；可供后续 DeviceStatus / 2022 精确位置读取。
- **光圈与预置位**：补齐可模拟的编码或独立 DeviceControl 元素路径；HTTP `irisin`/`irisout`/`preset_call` 不再因「位布局未定义」一律失败——能编码则下发并更新状态，仍无法表示的子类型保持明确 ERROR/501。
- **DeviceControl 子类型**：支持 `RecordCmd`（启停录像记录状态）、`IFrameCmd`（记录强制关键帧请求）；`DragZoom`/`TargetTrack` 若本阶段无法可靠模拟则明确应答 ERROR（不静默 200）。
- **规格对齐**：`playback-control-handling` 从「仅 200」提升为「必须影响会话」；`channel-http-api` 预置位/光圈场景与实现一致。

## Capabilities

### New Capabilities

- `device-control-semantics`：设备侧 DeviceControl / PlaybackControl 的状态与会话语义（PTZ 姿态、录像控制标志、I 帧请求、预置位/光圈）。

### Modified Capabilities

- `dynamic-catalog-alarm-and-playback`：加强 `playback-control-handling`——入站控制必须驱动回放会话。
- `channel-http-api`：光圈/预置位从「名义枚举 + 实际 501」改为可实现路径或明确失败契约。
- `device-node`：设备节点必须维护并应答上述控制带来的状态变化。
- `core-sip-stack`：确认 PlaybackPort Scale / Play/Pause 与 MESSAGE 入站路径接通（若需求文本已足够则仅 delta 引用场景）。

## Impact

- `internal/domain/model`：PTZ 状态、RecordCmd/IFrame 值对象；可能扩展 `EncodePTZCmd` 或并列元素编码。
- `internal/adapter/manscdp`：解码 PlaybackControl 完整字段；DeviceControl 子元素编解码。
- `internal/app`：`handlePlaybackControl`、`handleDeviceControl`、NodeService PTZ/playback。
- `internal/interface/http`：PTZ 光圈/预置位错误映射调整。
- 测试：acceptor 单测 + 通道 HTTP 测 + 可选 e2e（MESSAGE PlaybackControl → 会话状态）。

## Non-goals

- 不做真实云台物理仿真、真实强制 I 帧对编码器的副作用（只维护模拟状态 / 日志可观测）。
- 不做 DragZoom/TargetTrack 的完整几何算法（本阶段允许 ERROR）。
- 不做网络校时、媒体 RTCP/附录 M、SRTP/TLS、完整 DeviceConfig 矩阵。
- 不做 Web UI 平台拉流/录像库一等入口（另立 UX change）。
- 不重构拆分 `NodeService`/`Acceptor` 上帝文件（可小幅抽取，非本 change 目标）。
- 不改变级联 MESSAGE 中继与媒体 B2BUA 的分工。

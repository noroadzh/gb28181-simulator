# Design

## Context

See proposal.md — Why。现状：`handlePlaybackControl` 只打日志后 200（`acceptor_device_cmds.go`）；INFO/MANSRTSP 路径已调 `PlaybackPort.SetScale`；`EncodePTZCmd` 明确拒绝 iris/preset；`handleDeviceControl` 对 PTZ 只 ACK。`core-sip-stack` 规格已要求 MESSAGE PlaybackControl 接通 PlaybackPort，属实现欠债。

## Goals / Non-Goals

**Goals:**

- MESSAGE PlaybackControl ↔ 与 INFO 相同的会话控制入口。
- Device 侧可观测控制状态（PTZ/光圈/预置位/Record/IFrame）。
- HTTP ptz 的 iris/preset_call 与规格场景一致。

**Non-Goals:**

- 真实编码器 I 帧、真实录像落盘（RecordCmd 只改标志；落盘仍走既有 recording 管道）。
- DragZoom/TargetTrack 几何实现。
- 拆分 NodeService 大文件（允许小幅抽取 helper）。

## Decisions

1. **PlaybackControl 解码后复用 INFO 控制入口**  
   - 在 manscdp 增加 `DecodePlaybackControl(body) → (cmd, scale, mediaID, …)`。  
   - `handlePlaybackControl`：Play→`SetScale(1)` 或会话 Play；Pause→`SetScale(0)`；Teardown→`Stop`；Scale 命令→`SetScale`。会话键与现有回放 Call-ID / MediaID 策略对齐（优先 MediaID，否则节点当前唯一会话）。  
   - 备选：单独 playback 状态机 —— 拒绝，避免与 INFO 双轨。

2. **预置位用 DeviceControl `<PresetID>`，不用 PTZCmd 位**  
   - 国标常用独立元素；避免猜 PTZCmd 预置位比特。  
   - HTTP `preset_call` → 组 XML 含 PresetID；设备侧更新 `currentPreset`。

3. **光圈用扩展 FI 指令字节（业界常用映射）并写入 EncodePTZCmd**  
   - 在 `ptzcmd.go` 增加 `irisin`/`irisout` 映射（文档化所选 bit，例如 FI 指令字），拒绝「为了过测试随便 OR 方向位」。  
   - 备选：仅 HTTP 改状态不下发 —— 拒绝，抓包应对齐。

4. **控制状态存在 Acceptor/Node 运行时内存，不强制落库**  
   - 模拟器重启丢失可接受；测试通过 port 或 NodeService 只读访问器观测。  
   - 备选：SQLite —— 过重。

5. **RecordCmd / IFrameCmd：XML 子元素检测 + 状态标志**  
   - 与 GuardCmd/TeleBoot 相同的 `strings.Contains` / 专用 decode 模式。  
   - DragZoom/TargetTrack：decode 到则回 ERROR 应答体（若当前只回 SIP 200 无 body，则对 Control 类回带 Result=ERROR 的 MESSAGE 应答，或 400——与现有 DeviceControl 应答风格对齐，优先与 GuardCmd 同形）。

6. **不在本 change 改级联媒体路径**  
   - 控制语义仅本节点与向下转发既有路径。

## Risks / Trade-offs

- [会话键歧义：多路回放] → 要求 PlaybackControl 带 MediaID/ChannelID；缺省时仅当单会话才成功，否则 400。  
- [FI 光圈 bit 与某厂商不一致] → 文档与覆盖表注明映射；可配置映射留待后续。  
- [状态仅内存] → 联调重启需重新布防/预置；可接受。  
- [PlaybackPort stub 仍空操作] → 生产 wiring 使用真实会话实现；单测用 fake 断言调用。

## Migration Plan

- 滚动升级：仅加强语义，旧客户端多得到「真暂停」而非「假 200」。  
- 回滚：回退本 change 提交即可。  
- 归档后更新 `docs/gb28181-coverage.md` 设备控制/回放行（依赖 #45 已引入该文件）。

## Open Questions

无影响规格的未决项；FI 具体字节在实现任务中选定并写进 `ptzcmd.go` 注释与单测 golden。

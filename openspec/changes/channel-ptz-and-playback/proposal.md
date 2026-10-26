# Proposal

## Why

`channel-http-api` 早已写明 PTZ 必须通过 MANSCDP DeviceControl 下发、录像查询必须触发 RecordInfo 查询、回放必须发 PlaybackControl 并起流，但实现是空壳且**静默成功**：`NodeService.PTZ` 丢弃全部参数返回 nil，`NodeService.Records` 返回空列表，`Playback` 返回 501。WebUI 的 PTZ 按钮因此会提示成功而设备毫无动作——这是当前最影响可信度的缺陷。

## What Changes

- **先归零**：PTZ 与录像查询在未实现前改为返回 501（与 Playback/Talk 一致），任何时刻 UI 不再说谎。
- **PTZ 下发**：实现 `PTZ`，把方向/速度/时长编译成 MANSCDP DeviceControl（PTZCmd）并经 SIP MESSAGE 发往下级设备；非 device 节点、无在线下级等场景返回可辨识错误。
- **录像查询**：实现 `Records`，向下级设备发 RecordInfo 查询并解析应答，返回 `sn/start/end/type/size` 列表；设备不可达时返回明确错误而非空列表。
- **回放控制**：实现 `Playback`（play/pause/teardown/speed），经 PlaybackControl MANSCDP 下发；回放会话与 FLV 历史流复用现有媒体管道。
- **清理死桩**：删除无调用方的 `SendPTZ` / `ListRecords` / `StartPlayback` / `ControlPlayback` 四个旧桩。

**BREAKING**：PTZ / 录像查询在实现完成前由 200 改为 501；实现完成后恢复 200 但带真实数据。

## Capabilities

### Modified Capabilities

- `channel-http-api`：补充三组错误语义与回放会话生命周期（现有要求只写了成功路径）。

对应的路线图编号：#34（原 `channel-control-signalling` 拆分为本 change 与 `channel-talk-and-snapshot`）。

## Impact

- `internal/app/node_service.go`（PTZ/Records/Playback 实现 + 删除死桩）
- `internal/interface/http/channels.go`（501 映射）
- `internal/adapter/manscdp`（PTZCmd / RecordInfo / PlaybackControl 编解码，若缺）
- `internal/adapter/flv` / 媒体管道（历史流复用）
- 测试：app 单测 + HTTP 契约 + e2e（真实 SIP 报文断言）

## Non-goals

- 不实现语音对讲与抓拍（见 `channel-talk-and-snapshot`）。
- 不实现 GB/T 28181-2022 的 PTZ 精确位置/看守位（属 2022 扩展，已有独立能力）。
- 不实现跨平台级联的 PTZ 转发路由（沿用本进程内的下级解析）。
- 不改数据库 schema。

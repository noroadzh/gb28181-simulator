# Tasks

## 1. PlaybackControl 入站接通

- [x] 1.1 在 `manscdp` 增加 `DecodePlaybackControl`（Cmd/Scale/MediaID 等），golden 字节用例覆盖 Play/Pause/Teardown/Scale；验收：`go test ./internal/adapter/manscdp -run PlaybackControl`
- [x] 1.2 改写 `handlePlaybackControl`：按 Cmd 调用与 INFO 共用的 `PlaybackPort`（SetScale/Stop 等）；无会话 Pause/Teardown → 可辨识失败；验收：acceptor 单测断言调用了 fake PlaybackPort，且不再「仅 200」
- [x] 1.3 补充 MESSAGE PlaybackControl → 暂停/结束会话的单测（可与现有 INFO 测对照）；验收：相关 `go test ./internal/app -run PlaybackControl` 通过

## 2. PTZ 状态与光圈/预置位

- [x] 2.1 扩展 `EncodePTZCmd`（或并列编码器）支持 `irisin`/`irisout`，注释写明 FI 字节映射，并加 golden；验收：`go test ./internal/domain/model -run PTZ`
- [x] 2.2 HTTP/平台下发 `preset_call` 走 DeviceControl `<PresetID>`（非 PTZCmd 瞎猜）；验收：发出的 XML golden 或单测含 PresetID
- [x] 2.3 `handleDeviceControl` 更新设备内存姿态/光圈/当前预置位；提供可测只读访问；验收：app 单测「上视后姿态变化 / preset_call 更新索引」
- [x] 2.4 HTTP PTZ `irisin`/`preset_call` 对可达 device 返回 200（不再因「位布局未定义」501）；验收：`go test ./internal/interface/http -run PTZ` 或等价

## 3. RecordCmd / IFrameCmd / 显式 ERROR

- [x] 3.1 解码并处理 RecordCmd（启停 → 正在录像标志）、IFrameCmd（计数/时间戳）；golden + acceptor 单测；验收：对应 `go test` 通过
- [x] 3.2 DragZoom/TargetTrack 未实现时回 ERROR（非 200 空操作）；验收：单测覆盖

## 4. 文档与校验

- [x] 4.1 若 `docs/gb28181-coverage.md` 已存在，更新设备控制/回放行至本 change 语义；同步 `docs/web-ui-guide.md` §5.6 中光圈/预置位/PlaybackControl 描述；验收：文档不再写「光圈/预置位一律 501」作为最终行为
- [x] 4.2 `openspec validate --change interop-hardening --strict`、`make lint`、相关包 `go test`（含 `-race` 若可行）通过；验收：退出码 0

## Workflow follow-up

- 归档时将路线图 `#46` 标为 ✅，并按 `docs-roadmap-sync` 更新覆盖表
- 后续候选（不在本 change）：Web UI 平台拉流/录像入口、校时、媒体保活、SRTP

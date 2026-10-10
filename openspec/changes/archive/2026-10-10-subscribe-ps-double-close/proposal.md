# Proposal

## Why

`MediaService.SubscribePS`（media-sources 能力核心管道）在并发场景下存在 channel 双重关闭 bug：reader goroutine 的 `defer close(psChRaw)` 与 cleanup 函数的 `sync.Once` 保护的 `close(psChRaw)` 可同时触发，导致 `panic: close of closed channel`。该 panic 使 HTTP-FLV 流媒体端点无法持续推送帧数据（仅返回 12 字节 FLV 文件头后断开），浏览器播放器报 404/IOException 黑屏。

影响范围：所有通过 Web UI 绑定本地文件媒体源的通道播放请求（路线图阶段 14 web-management-ui 依赖阶段 8 media-sources 管道）。

## What Changes

- **修复** `internal/app/media_service.go` 的 `SubscribePS` 方法：将 reader goroutine 内的 `defer close(psChRaw)` 改为与 cleanup 共用同一个 `sync.Once`，保证 channel 只被关闭一次。
- **新增** `internal/app/media_service_test.go`：编写并发单元测试覆盖 EOF / ctx cancel / cleanup 主动调用三种关闭路径（`go test -race` 无 panic）。
- **非破坏性变更**：不修改 `MediaService` 的公开接口签名，caller 的 cleanup 调用行为不变。

## Capabilities

### Modified Capabilities

- `media-sources`：增加并发安全要求（PS 管道 channel 关闭路径唯一性）

## Impact

- 受影响代码：`internal/app/media_service.go`（SubscribePS 方法）
- 新增测试：`internal/app/media_service_test.go`
- 受影响 API：`GET /v1/flv/:nodeID/:channelID`（HTTP-FLV 流媒体端点）
- 依赖关系：web-management-ui（阶段 14）的 FLV 播放器依赖 media-sources（阶段 8）的 SubscribePS 管道稳定性

## Non-goals

- 不引入新的媒体源类型或 factory 注册机制
- 不修改 HTTP-FLV 网关（`streaming/adapter.go` / `streaming/gateway.go`）的调用方逻辑
- 不改变 `MediaConfig` 的序列化格式或 API 请求体结构
- 不重构现有的 `sync.Once` 为 `sync.Mutex` 或其他并发原语

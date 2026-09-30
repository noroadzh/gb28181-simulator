# Proposal

## Why

媒体热循环（PS 打包、RTP 打包）每秒处理数千个 RTP 包。若对每个可恢复的 per-packet 错误都记日志，会淹没文件 sink 与 Hub 订阅者（Web UI 不可用）。`media.ErrorAggregator` 已在 `enhance-logging-coverage` 中落地，但尚无任何调用方接入——`ps_packetizer.go` 与 `rtpizer.go` 的错误路径仍是裸 `return err`，重复失败不会留下任何痕迹。

## What Changes

- `PSPacketizer.Packetize` 校验失败与打包失败路径接入 `ErrorAggregator.Record("ps-mux", err)`，返回行为不变
- `RTPizer.Packetize` 校验失败与分片失败路径接入 `ErrorAggregator.Record("rtp-send", err)`，返回行为不变
- 两个构造函数 `NewPSPacketizer` / `NewRTPizer` 增加可选 `*ErrorAggregator` 参数（nil-safe：nil 时退化为无聚合，保持 `cmd/genps` 等现有调用点零改动）
- `cmd/gb28181-simulator/main.go` 创建进程级共享 aggregator，工厂闭包注入两个 packetizer，并由 MediaService 会话 goroutine 或专用 ticker 驱动 `Sweep()`；进程退出时 `Flush()`
- 聚合摘要日志使用 `component=internal/adapter/media` 标签，使 `log.modules` 覆盖生效

## Capabilities

### New Capabilities
- `media-aggregator-integration`: 媒体热循环错误聚合接入契约——packetizer 错误路径的聚合记录、共享实例生命周期（创建/注入/Sweep/Flush）、nil 退化语义

### Modified Capabilities
- 无（`logging-coverage` spec 未被修改；本 change 只新增消费方）

## Impact

- 代码：`internal/adapter/media/ps_packetizer.go`、`internal/adapter/media/rtpizer.go`、`cmd/gb28181-simulator/main.go`
- 测试：`ps_packetizer` / `rtpizer` 聚合行为单测（注入 fake aggregator 或短窗口真实实例）；`media_service` 装配不受影响
- 风险：低——只增不改返回路径；nil aggregator 语义保证向后兼容
- Deferred 无遗留：本 change 完成后 roadmap #17 关闭

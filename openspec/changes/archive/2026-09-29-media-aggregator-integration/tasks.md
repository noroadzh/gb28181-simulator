# Tasks

## 1. PSPacketizer 聚合接入

- [x] 1.1 `PSPacketizer` 增加 `agg *ErrorAggregator` 字段；`NewPSPacketizer(agg *ErrorAggregator)` 构造（nil-safe：nil 时零开销）
- [x] 1.2 `Packetize` 的 `frame.Validate()` 失败路径调用 `p.agg.Record("ps-mux", err)`（Record 自身 nil-safe），返回行为不变

## 2. RTPizer 聚合接入

- [x] 2.1 `RTPizer` 增加 `agg *ErrorAggregator` 字段；`NewRTPizer(ssrc uint32, mtu int, agg *ErrorAggregator)` 构造（nil-safe）
- [x] 2.2 `Packetize` 的 `ps.Validate()` 失败路径调用 `r.agg.Record("rtp-send", err)`，返回行为不变

## 3. 主二进制生命周期

- [x] 3.1 `cmd/gb28181-simulator/main.go` 创建 `media.NewErrorAggregator()` 单例；`PSPacketizerFactory` / `RTPizerFactory` 闭包捕获并传入
- [x] 3.2 main 启动 `time.NewTicker(media.DefaultWindow)` 后台协程，循环 `agg.Sweep()`；`ctx` cancel 时退出 ticker
- [x] 3.3 main `defer agg.Flush()` 确保退出时摘要不丢

## 4. CLI 兼容性

- [x] 4.1 `cmd/genps/main.go` 调用 `media.NewPSPacketizer(nil)` 与 `media.NewRTPizer(0xABCDEF01, 1400, nil)`；零行为变化

## 5. 测试

- [x] 5.1 `ps_packetizer_aggregator_test.go`：注入真实 aggregator（短窗口）；非法 frame 触发 Record；通过公开状态或 Sweep+log hook 验证 signature = `"ps-mux"`
- [x] 5.2 `rtpizer_aggregator_test.go`：同上，signature = `"rtp-send"`
- [x] 5.3 现有 `media_test.go` / `media_source_test.go` / `mp4_demuxer_test.go` 等调用点更新为新构造签名（nil 或注入实例），全部通过

## 6. 验证

- [x] 6.1 `go vet ./...` 通过
- [x] 6.2 `go test -race ./internal/adapter/media/... ./cmd/...` 通过
- [x] 6.3 `openspec validate media-aggregator-integration --strict --type change --no-interactive` PASS

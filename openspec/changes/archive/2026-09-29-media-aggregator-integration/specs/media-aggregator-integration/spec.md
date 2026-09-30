# Spec Delta: media-aggregator-integration

## Purpose

媒体热循环（PS 打包与 RTP 分片）的可恢复错误按 signature 聚合，避免 per-packet 日志淹没文件 sink 与 Hub 订阅者。本 capability 覆盖接入位置、共享实例生命周期与 nil 退化语义。

## ADDED Requirements

### Requirement: PS packetizer error aggregation

`PSPacketizer.Packetize` MUST record every error from `frame.Validate()` on a process-wide shared `*media.ErrorAggregator` with signature `"ps-mux"`. The original return behaviour (error values and error chain) SHALL remain unchanged. (Header/stream assembly uses `append`/`binary.PutUint*` which cannot fail; those code paths return no error.)

#### Scenario: validation error aggregated
- **WHEN** 调用 `PSPacketizer.Packetize(frame)` 且 `frame.Validate()` 返回非 nil 错误
- **THEN** `ErrorAggregator.Record("ps-mux", err)` 被调用一次，`Packetize` 仍返回该错误（错误链与 message 不变）

#### Scenario: nil aggregator is a no-op
- **WHEN** `PSPacketizer` 构造时未提供 aggregator（`nil`）
- **THEN** `Packetize` 路径不调用任何聚合方法，行为与本 change 之前完全一致；返回的错误保持原样

### Requirement: RTP packetizer error aggregation

`RTPizer.Packetize` MUST record every error from `ps.Validate()` on a process-wide shared `*media.ErrorAggregator` with signature `"rtp-send"`. The original return behaviour SHALL remain unchanged. (Fragmentation uses slice expressions and struct literal construction, which cannot fail; those code paths return no error.)

#### Scenario: validation error aggregated
- **WHEN** 调用 `RTPizer.Packetize(ps)` 且 `ps.Validate()` 返回非 nil 错误
- **THEN** `ErrorAggregator.Record("rtp-send", err)` 被调用一次，`Packetize` 仍返回该错误

#### Scenario: nil aggregator is a no-op
- **WHEN** `RTPizer` 构造时未提供 aggregator（`nil`）
- **THEN** `Packetize` 路径不调用任何聚合方法，行为与本 change 之前完全一致

### Requirement: Shared aggregator lifetime

The main binary (`cmd/gb28181-simulator`) MUST create a process-wide shared `*media.ErrorAggregator` singleton and inject it into both the `PSPacketizerFactory` and `RTPizerFactory` closures. A background goroutine MUST call `Sweep()` at least once per minute. On process exit (main `defer` or signal handler) `Flush()` MUST be invoked.

#### Scenario: aggregator created once and shared
- **WHEN** 启动 `cmd/gb28181-simulator`
- **THEN** `main.go` 创建一个 `media.NewErrorAggregatorWithWindow(60s)` 实例，工厂闭包将其注入所有 `PSPacketizer` 与 `RTPizer` 实例

#### Scenario: sweep advances the aggregation window
- **WHEN** 后台 goroutine 调用 `Sweep()`
- **THEN** 过期窗口被丢弃，并按 signature 触发摘要记录（每 signature 一行）

#### Scenario: flush reports the final state
- **WHEN** 进程退出调用 `Flush()`
- **THEN** 所有未到期的窗口立即摘要输出，不丢失任何已记录错误

### Requirement: Aggregator log tagging

Aggregator summary records MUST carry the `component=internal/adapter/media` tag, so a `log.modules` configuration entry with the `internal/adapter/media` prefix takes effect for these lines.

#### Scenario: tag present on summary records
- **WHEN** 聚合摘要被写出
- **THEN** 记录的 `component` 属性值等于 `"internal/adapter/media"`

### Requirement: CLI tools keep zero-aggregator path

Standalone CLIs (such as `cmd/genps`) MUST NOT be forced to wire an aggregator. The aggregator parameter of `NewPSPacketizer` / `NewRTPizer` MUST be optional; passing `nil` SHALL preserve byte-identical behaviour to before this change.

#### Scenario: genps unaffected
- **WHEN** `cmd/genps` 调用 `media.NewPSPacketizer()`（无 aggregator 参数重载或 nil 参数）
- **THEN** `Packetize` 返回的错误与未引入本 change 时一致，零聚合开销
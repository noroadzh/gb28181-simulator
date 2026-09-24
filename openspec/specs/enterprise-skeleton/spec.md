# enterprise-skeleton Specification

## Purpose
本 capability 把 `gb28181-simulator` 的 `internal/` 树重塑为六边形架构（platform / domain / adapter / interface / app 五层），引入手写 ServiceContext 容器与 OpenTelemetry trace provider；为 Change 4 (`node-abstraction`) 及后续 12 个 change 提供清晰边界与可观测性基础。

本 change **不实现任何业务逻辑**，仅做结构改造与基础设施引入；现有 72 个测试用例 100% 保持通过。

## Requirements

### Requirement: Hexagonal layered structure enforced by directory layout

The system SHALL organize `internal/` into five layers — `platform/`, `domain/`, `adapter/`, `interface/`, `app/` — with the following dependency direction: `adapter → domain`, `interface → domain` (and `adapter`/`interface` may depend on `platform`), `domain` MAY depend on stdlib only, `platform` MAY depend on stdlib and well-known infrastructure libraries (slog, viper, OTel). `app/` is reserved for Change 4+ and SHALL be empty in this change.

#### Scenario: domain 包除 stdlib 外零依赖

- **WHEN** 执行 `go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...`
- **THEN** 输出为空（除标准库与 otel/api 公开类型外，domain 不依赖任何 adapter / interface / platform 实现）

#### Scenario: adapter 依赖 domain 而非反之

- **WHEN** 编译任一 adapter 包（`go build ./internal/adapter/...`）
- **THEN** 编译成功；adapter 包 import `internal/domain/port` 或 `internal/domain/model`

#### Scenario: 五层目录存在且命名严格

- **WHEN** `ls internal/`
- **THEN** 输出包含 `platform/`、`domain/`、`adapter/`、`interface/`（`app/` 允许为空目录或 .gitkeep 占位）

### Requirement: Domain ports define contracts as Go interfaces

The system SHALL define five port interfaces under `internal/domain/port/`: `SIPTransport`, `SDPCodec`, `Authenticator`, `AuditSink`, `Clock`. Each interface MUST operate on values from `internal/domain/model/` (not from any adapter type).

#### Scenario: SIPTransport 暴露 Send/Receive/Close

- **WHEN** 编译 `internal/domain/port/transport.go`
- **THEN** `SIPTransport` interface 含 `Send(ctx, Message, dst) error`、`Receive(ctx) (Message, string, error)`、`Close() error` 三个方法；参数类型全部来自 `internal/domain/model`

#### Scenario: Adapter 实现端口编译期断言

- **WHEN** 编译任一 adapter 包
- **THEN** adapter 文件中存在 `var _ port.SIPTransport = (*XxxAdapter)(nil)` 形式的编译期断言；若断言失败，编译失败并明确指出哪个类型未实现哪个方法

### Requirement: Hand-rolled ServiceContext provides type-safe service registry

The system SHALL provide `internal/platform/servicectx/` containing a `Container` struct with `Provide(key, factory)`、`Build(ctx)`、`Get(key)`、`MustGet[T](key)` methods. The container MUST NOT depend on any third-party DI library (wire/fx).

#### Scenario: 注册后 Build 顺序与 Close 反序

- **WHEN** 测试按 key=A → key=B → key=C 顺序 Provide 后调用 `Build(ctx)`
- **THEN** 三个 provider 按 A→B→C 顺序执行；调用返回的 cancel 函数时，Close 反序 C→B→A 执行

#### Scenario: MustGet[T] 泛型类型安全

- **WHEN** 调用 `MustGet[*config.Config](sc, "config")` 且 key 不存在
- **THEN** panic 信息含 key 名称；类型不匹配时 panic 信息含期望类型与实际类型

#### Scenario: Provide 重复 key 覆盖

- **WHEN** 同一 key 被 Provide 两次
- **THEN** 后注册的 factory 覆盖前者；Build 时使用最后一次注册的版本

### Requirement: OpenTelemetry trace provider integrated into platform

The system SHALL provide `internal/platform/observability/tracing/` with a `Provider` that wraps `sdktrace.TracerProvider`, configured via `internal/platform/config` to support stdout exporter (default) and OTLP gRPC exporter (optional). Sampling ratio is configurable via `config.Tracing.SampleRatio` (default 0.01).

#### Scenario: 启动后全局 tracer 可用

- **WHEN** main.go 调用 `tracing.NewProvider(cfg.Tracing)` 并 `Build`
- **THEN** 全局 `otel.Tracer("domain.node")` 返回非 nil 且 span 会被 stdout exporter 输出

#### Scenario: 采样率 0.0 不输出任何 span

- **WHEN** `config.Tracing.SampleRatio=0.0`
- **THEN** 启动后任意业务调用 `otel.Tracer(...).Start(ctx, "test")` 产生的 span 不会被 stdout exporter 输出

#### Scenario: Close 关闭 provider 与 exporter

- **WHEN** 调用 `tracing.Provider.Close()`
- **THEN** TracerProvider.Shutdown 被调用；stdout exporter 不再接收新 span
- **AND** 在 `-race` 下无数据竞争，exporter goroutine 随 `Shutdown` 退出（由
  `TestProvider_CloseFlushesAndStopsNewSpans` 覆盖；本 change 未引入 goleak 依赖）

### Requirement: cmd main.go rewritten as assembly entry point

The system SHALL rewrite `cmd/gb28181-simulator/main.go` and `cmd/sipprobe/main.go` to delegate all wiring to `ServiceContext.Provide()` calls and end with `Build(ctx) + MustGet[T](server).Start(ctx)`. No business logic SHALL remain in main.go.

#### Scenario: main.go 不含业务逻辑，仅做装配

- **WHEN** 检查 `cmd/gb28181-simulator/main.go` 的 import 列表与函数体
- **THEN** import 中除 `platform` / `servicectx` 外，仅允许出现装配所必需的具体类型包
  （`internal/storage`、`internal/interface/http`，以及 `internal/sipprobe` 用于 `sipprobe`
  子命令分发）
- **AND** 函数体内只出现 `Provide` / `Build` / `MustGet` 与信号处理，不含协议解析或业务分支

#### Scenario: 启动顺序与日志一致

- **WHEN** 启动 `bin/gb28181-simulator` 并加 `-v`
- **THEN** 日志按 "config loaded" → "logger initialized" → "tracing provider started" → "http server listening" 顺序输出

### Requirement: CGO-free build preserved across the skeleton refactor

The system SHALL maintain `CGO_ENABLED=0 go build ./...` success on Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 after the refactor. New OTel dependencies SHALL NOT introduce CGO.

#### Scenario: 搬迁后五平台编译通过

- **WHEN** 在五平台分别执行 `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build ./cmd/sipprobe`
- **THEN** 产物为静态链接二进制，无 `cgo` 警告

#### Scenario: OTel 依赖纯 Go

- **WHEN** 执行 `go list -deps ./internal/platform/observability/tracing/... | xargs go list -f '{{.ImportPath}}{{if .CgoFiles}} CGO{{end}}' | grep CGO`
- **THEN** 输出为空

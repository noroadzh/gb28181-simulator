# enterprise-skeleton 规范

## Purpose
本能力将 `gb28181-simulator` 的 `internal/` 树重塑为六边形架构（platform / domain / adapter / interface / app 五层），引入手写 ServiceContext 容器与 OpenTelemetry trace provider；为 Change 4 (`node-abstraction`) 及后续 12 个 change 提供清晰边界与可观测性基础。

本 change **不实现任何业务逻辑**，仅做结构改造与基础设施引入；现有 72 个测试用例 100% 保持通过。

## Requirements

### 需求：通过目录布局强制执行六边形分层结构

系统必须将 `internal/` 组织为五个层次——`platform/`、`domain/`、`adapter/`、`interface/`、`app/`——依赖方向如下：`adapter → domain`、`interface → domain`（`adapter`/`interface` 可依赖 `platform`）；`domain` 除 stdlib 外无其他依赖；`platform` 可依赖 stdlib 及常用基础设施库（slog、viper、OTel）。`app/` 为 Change 4+ 预留，本 change 中必须为空目录。

#### 场景：domain 包除 stdlib 外零依赖

- **WHEN** 执行 `go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...`
- **THEN** 输出为空（除标准库与 otel/api 公开类型外，domain 不依赖任何 adapter / interface / platform 实现）

#### 场景：adapter 依赖 domain 而非反之

- **WHEN** 编译任一 adapter 包（`go build ./internal/adapter/...`）
- **THEN** 编译成功；adapter 包 import `internal/domain/port` 或 `internal/domain/model`

#### 场景：五层目录存在且命名严格

- **WHEN** `ls internal/`
- **THEN** 输出包含 `platform/`、`domain/`、`adapter/`、`interface/`（`app/` 允许为空目录或 .gitkeep 占位）

### 需求：Domain ports 将契约定义为 Go 接口

系统必须在 `internal/domain/port/` 下定义五个 port 接口：`SIPTransport`、`SDPCodec`、`Authenticator`、`AuditSink`、`Clock`。每个接口必须仅操作 `internal/domain/model/` 中的值（不得依赖任何 adapter 类型）。

#### 场景：SIPTransport 暴露 Send/Receive/Close

- **WHEN** 编译 `internal/domain/port/transport.go`
- **THEN** `SIPTransport` interface 含 `Send(ctx, Message, dst) error`、`Receive(ctx) (Message, string, error)`、`Close() error` 三个方法；参数类型全部来自 `internal/domain/model`

#### 场景：Adapter 实现端口编译期断言

- **WHEN** 编译任一 adapter 包
- **THEN** adapter 文件中存在 `var _ port.SIPTransport = (*XxxAdapter)(nil)` 形式的编译期断言；若断言失败，编译失败并明确指出哪个类型未实现哪个方法

### 需求：手写 ServiceContext 提供类型安全的服务注册表

系统必须提供 `internal/platform/servicectx/` 包含 `Container` 结构体，其方法为 `Provide(key, factory)`、`Build(ctx)`、`Get(key)`、`MustGet[T](key)`。该容器不得依赖任何第三方 DI 库（wire/fx）。

#### 场景：注册后 Build 顺序与 Close 反序

- **WHEN** 测试按 key=A → key=B → key=C 顺序 Provide 后调用 `Build(ctx)`
- **THEN** 三个 provider 按 A→B→C 顺序执行；调用返回的 cancel 函数时，Close 反序 C→B→A 执行

#### 场景：MustGet[T] 泛型类型安全

- **WHEN** 调用 `MustGet[*config.Config](sc, "config")` 且 key 不存在
- **THEN** panic 信息含 key 名称；类型不匹配时 panic 信息含期望类型与实际类型

#### 场景：Provide 重复 key 覆盖

- **WHEN** 同一 key 被 Provide 两次
- **THEN** 后注册的 factory 覆盖前者；Build 时使用最后一次注册的版本

### 需求：OpenTelemetry trace provider 集成到 platform

系统必须提供 `internal/platform/observability/tracing/`，其 `Provider` 封装 `sdktrace.TracerProvider`，通过 `internal/platform/config` 配置，支持 stdout exporter（默认）与 OTLP gRPC exporter（可选）。采样比例通过 `config.Tracing.SampleRatio` 配置（默认 0.01）。

#### 场景：启动后全局 tracer 可用

- **WHEN** main.go 调用 `tracing.NewProvider(cfg.Tracing)` 并 `Build`
- **THEN** 全局 `otel.Tracer("domain.node")` 返回非 nil 且 span 会被 stdout exporter 输出

#### 场景：采样率 0.0 不输出任何 span

- **WHEN** `config.Tracing.SampleRatio=0.0`
- **THEN** 启动后任意业务调用 `otel.Tracer(...).Start(ctx, "test")` 产生的 span 不会被 stdout exporter 输出

#### 场景：Close 关闭 provider 与 exporter

- **WHEN** 调用 `tracing.Provider.Close()`
- **THEN** TracerProvider.Shutdown 被调用；stdout exporter 不再接收新 span
- **AND** 在 `-race` 下无数据竞争，exporter goroutine 随 `Shutdown` 退出（由
  `TestProvider_CloseFlushesAndStopsNewSpans` 覆盖；本 change 未引入 goleak 依赖）

### 需求：cmd/main.go 重构为装配入口

系统必须重构 `cmd/gb28181-simulator/main.go` 与 `cmd/sipprobe/main.go`，将所有 wiring 委托给 `ServiceContext.Provide()` 调用，并以 `Build(ctx) + MustGet[T](server).Start(ctx)` 收尾。main.go 中不得残留任何业务逻辑。

#### 场景：main.go 不含业务逻辑，仅做装配

- **WHEN** 检查 `cmd/gb28181-simulator/main.go` 的 import 列表与函数体
- **THEN** import 中除 `platform` / `servicectx` 外，仅允许出现装配所必需的具体类型包
  （`internal/storage`、`internal/interface/http`，以及 `internal/sipprobe` 用于 `sipprobe`
  子命令分发）
- **AND** 函数体内只出现 `Provide` / `Build` / `MustGet` 与信号处理，不含协议解析或业务分支

#### 场景：启动顺序与日志一致

- **WHEN** 启动 `bin/gb28181-simulator` 并加 `-v`
- **THEN** 日志按 "config loaded" → "logger initialized" → "tracing provider started" → "http server listening" 顺序输出

### 需求：骨架重构中保持 CGO-free 构建

重构后，系统必须在 Linux amd64/arm64、macOS amd64/arm64、Windows amd64 五个平台保持 `CGO_ENABLED=0 go build ./...` 成功。新增的 OTel 依赖不得引入 CGO。

#### 场景：搬迁后五平台编译通过

- **WHEN** 在五平台分别执行 `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build ./cmd/sipprobe`
- **THEN** 产物为静态链接二进制，无 `cgo` 警告

#### 场景：OTel 依赖纯 Go

- **WHEN** 执行 `go 列出 -deps ./internal/platform/observability/tracing/... | xargs go 列出 -f '{{.ImportPath}}{{if .CgoFiles}} CGO{{end}}' | grep CGO`
- **THEN** 输出为空

# Design

## Context

仓库已完成 Change 1（骨架 + Web 空壳）与 Change 2（节点无关 SIP/SDP/Digest/transport）。本 change 在不动业务逻辑的前提下，把现有 `internal/` 树重塑为六边形架构，为 Change 4 (`node-abstraction`) 及后续 12 个 change 提供：

- **端口契约**（`internal/domain/port/`）：业务能力的接口边界
- **横切关注点**（`internal/platform/`）：logger / config / clock / tracing / ServiceContext
- **明确分层**（`internal/adapter/` 实现端口；`internal/interface/` 对外协议；`internal/app/` 用例编排）

技术约束（来自 `openspec/config.yaml` project context）：

- 纯 Go，无 CGO
- 复用现有 4 个核心包，不重写
- 不引 wire/fx
- logger 保留 `log/slog`
- 可观测性仅 OTel trace，不引 metric

## Goals / Non-Goals

**Goals:**

- 把现有 `internal/` 树重排为 `platform / domain / adapter / interface / app` 五层
- `domain/port/` 定义 5 个端口接口：`SIPTransport`、`SDPCodec`、`Authenticator`、`AuditSink`、`Clock`
- `platform/servicectx/` 手写最小容器，类型安全 `Get<T>(key)`
- `platform/observability/tracing/` 引入 OTel trace provider，stdout exporter 默认启用，OTLP gRPC exporter 可选启用
- `cmd/<name>/main.go` 重写为装配入口
- 72 个现有测试 100% 保持通过
- 新增 ≥ 30 个针对 ServiceContext 与端口契约的测试

**Non-Goals:**

- 不实现 Node 抽象
- 不实现任何业务用例（`app/` 仅留 ServiceContext 装配模板）
- 不重写 4 个核心包业务逻辑
- 不引 wire/fx/prometheus
- 不改 `openspec/specs/core-sip-stack/spec.md` 的内容

## Decisions

### D1. 端口接口由 `domain/port/` 拥有，零外部依赖

```go
// internal/domain/port/transport.go
package port

import (
    "context"
    "github.com/your-org/gb28181-simulator/internal/domain/model"
)

type SIPTransport interface {
    Send(ctx context.Context, msg model.Message, dst string) error
    Receive(ctx context.Context) (model.Message, string, error)
    Close() error
}
```

**理由：**
- 六边形架构的核心是"依赖向内"——adapter 依赖 domain，domain 不依赖 adapter
- 接口定义放 domain 才能保证编译期类型安全（adapter 必须实现接口，否则无法被 ServiceContext 装配）
- Go 的隐式接口实现让 adapter 不必 import "port"（仅需 import model）；但建议显式 `_ port.SIPTransport = (*SIPAdapter)(nil)` 编译期断言

**权衡：**
- 接口粒度：5 个接口分别对应"传输/编解码/认证/审计/时钟"，不细化到"每种 SIP 方法一个接口"——避免过度设计，Change 4 之后按需扩展
- 返回类型：用 `model.Message` 而非 `sip.Request`——保证 domain 不依赖 adapter

### D2. ServiceContext 手写最小容器

```go
// internal/platform/servicectx/container.go
package servicectx

import (
    "context"
    "fmt"
    "sync"
)

type Key string

type Container struct {
    mu        sync.RWMutex
    providers map[Key]func() any
    instances map[Key]any
    order     []Key  // Build 顺序
}

func New() *Container { ... }

func (c *Container) Provide(key Key, p func() any) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.providers[key] = p
    c.order = append(c.order, key)
}

func (c *Container) Build(ctx context.Context) (context.Context, func()) {
    for _, k := range c.order {
        c.instances[k] = c.providers[k]()
    }
    return ctx, func() {
        for _, k := range c.order {
            if c, ok := c.instances[k].(io.Closer); ok {
                _ = c.Close()
            }
        }
    }
}

func (c *Container) Get(key Key) (any, bool) { ... }
func MustGet[T any](c *Container, key Key) T { ... }  // Go 1.22+ 泛型
```

**理由：**
- 5-10 行装配代码 vs wire 的 ~200 行配置——规模不匹配
- 编译期类型安全通过 Go 泛型 `MustGet[T]` 保证
- 启动顺序明确（`order` slice），Close 反序
- 容器本身不引任何外部依赖（除 stdlib）

**权衡：**
- 无自动依赖图——main.go 必须按正确顺序 `Provide`
- 无 scope 概念（每个 key 一个 singleton）——Change 4 引入 Node 实例时再决定是否扩展

### D3. OTel trace provider 接入方式

```go
// internal/platform/observability/tracing/provider.go
package tracing

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Provider struct {
    tp     *sdktrace.TracerProvider
    closer func() error
}

func NewProvider(cfg Config) (*Provider, error) {
    exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
    if err != nil { return nil, err }
    res := resource.NewWithAttributes(
        semconv.SchemaURL,
        semconv.ServiceName("gb28181-simulator"),
        semconv.ServiceVersion(version.Version),
    )
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exp),
        sdktrace.WithResource(res),
        sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
    )
    otel.SetTracerProvider(tp)
    return &Provider{tp: tp, closer: tp.Shutdown}, nil
}

func (p *Provider) Close() error { return p.closer() }

func (p *Provider) Tracer(name string) trace.Tracer { return p.tp.Tracer(name) }
```

**理由：**
- OTel 是 CNCF 毕业项目，Go SDK 是 Go 生态事实标准
- 一次 `otel.SetTracerProvider(tp)` 全局生效；后续 Change 4 在 domain 层通过 `otel.Tracer("...")` 直接拿 tracer，无需 import provider
- stdout exporter 默认启用（开发友好），OTLP gRPC exporter 可选启用（生产友好）

**权衡：**
- 默认采样率 `0.01`（1%）——开发时全采样可改 `1.0`，生产避免 OOM
- 不引 OTLP HTTP exporter（gRPC 已覆盖）
- 不引 metric provider（推 Change 14）

### D4. 现有 4 个核心包的迁移策略

**仅搬迁路径，不重写逻辑：**

| 原路径 | 新路径 | 改动 |
|---|---|---|
| `internal/sip` | `internal/adapter/sip` | import 路径更新；文件末尾加 `var _ port.SIPTransport = (*Transport)(nil)` 编译期断言 |
| `internal/sdp` | `internal/adapter/sdp` | 同上 |
| `internal/auth` | `internal/adapter/auth` | 同上 |
| `internal/siptransport` | `internal/adapter/siptransport` | 同上 |
| `internal/sip/audit` | `internal/adapter/audit` | 同上 |
| `internal/logger` | `internal/platform/observability/logging` | 仅路径 |
| `internal/config` | `internal/platform/config` | 仅路径 |
| `internal/api` | `internal/interface/http` | 仅路径 |
| `internal/webui` | `internal/interface/webui` | 仅路径 |
| `internal/storage` | `internal/storage`（不变） | 加 `var _ port.Storage = (*Store)(nil)` |

**理由：**
- 业务逻辑 0 改动 = 风险最低
- 编译期断言保证 adapter 实现了 port 接口（CI 报警）
- 测试用例保持原样——`go test ./...` 自动覆盖新路径

### D5. cmd 装配入口模板

```go
// cmd/gb28181-simulator/main.go
package main

import (
    "context"
    "log"
    "github.com/your-org/gb28181-simulator/internal/adapter/auth"
    "github.com/your-org/gb28181-simulator/internal/adapter/sip"
    "github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
    "github.com/your-org/gb28181-simulator/internal/interface/http"
    "github.com/your-org/gb28181-simulator/internal/platform/config"
    "github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
    "github.com/your-org/gb28181-simulator/internal/platform/observability/tracing"
    "github.com/your-org/gb28181-simulator/internal/platform/servicectx"
)

func main() {
    sc := servicectx.New()

    sc.Provide("config", func() any {
        cfg, err := config.Load("config.yaml")
        if err != nil { log.Fatal(err) }
        return cfg
    })
    sc.Provide("logger", func() any {
        cfg := servicectx.MustGet[*config.Config](sc, "config")
        return logging.New(cfg.Log)
    })
    sc.Provide("tracer", func() any {
        cfg := servicectx.MustGet[*config.Config](sc, "config")
        tp, err := tracing.NewProvider(cfg.Tracing)
        if err != nil { log.Fatal(err) }
        return tp
    })
    sc.Provide("sip-transport", func() any {
        return sip.NewTransport()
    })
    sc.Provide("auth", func() any {
        return auth.New()
    })
    sc.Provide("http-server", func() any {
        return httpapi.NewServer(
            servicectx.MustGet[logging.Logger](sc, "logger"),
        )
    })

    ctx, shutdown := sc.Build(context.Background())
    defer shutdown()

    httpSrv := servicectx.MustGet[httpapi.Server](sc, "http-server")
    if err := httpSrv.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

**理由：**
- main.go 只做装配，无业务
- 类型安全通过泛型 `MustGet[T]` 保证
- 启动顺序明确（`sc.order`），优雅关闭反序
- ~50 行 vs 之前的 ~150 行（之前的散落各包 init）

## Risks / Trade-offs

- **R1**：搬迁路径后 git blame 历史断裂。**Mitigation**：用 `git log --follow` 跟踪；README 加 CHANGELOG 标注。
- **R2**：OTel SDK 升级可能破坏 API。**Mitigation**：在 `go.mod` 锁定 `v1.28.0`（2026-09 LTS）。
- **R3**：ServiceContext 无自动依赖图，main.go 写错顺序编译期发现不了。**Mitigation**：每个 provider 函数内显式 `MustGet` 依赖；CI 加 lint 规则检查 `MustGet` 在 `Build` 前不调用。
- **R4**：端口接口粒度不当导致 Change 4 还要再调。**Mitigation**：在 design 阶段参考 Change 4 proposal 草案（若已存在），否则保守 5 接口、Change 4 扩展。
- **R5**：现有测试 3500 行搬迁后 import 路径更新漏改。**Mitigation**：用 `gofmt -r 'internal/sip -> internal/adapter/sip' -w ./...` 一次性替换 + 编译验证。

## Migration Plan

1. 创建新目录骨架（空目录 + 占位 doc.go）
2. `gofmt` + 文本替换更新所有 import 路径
3. 编译验证：`go build ./...`
4. 测试验证：`go test ./...`
5. 加端口接口编译期断言
6. 加 OTel tracing provider
7. 加 ServiceContext 容器
8. 重写 cmd main.go
9. 跑全包 race + smoke 等价测试
10. 写报告 `reports/enterprise-skeleton-verify.md`

**回滚**：git revert 即可。纯结构改动，无运行时影响。

## Open Questions

- **Q1**：`internal/storage` 是 adapter 还是独立 domain 依赖？（倾向 adapter，因为 SQLite 是具体技术）
- **Q2**：`internal/interface/webui` 是否需要端口化？目前只是 embed FS，没调用其它包（倾向暂不端口化）
- **Q3**：OTel trace 配置放 `config.Tracing` 还是单独 `tracing.yaml`？（倾向 `config.Tracing` 子节，复用 viper）

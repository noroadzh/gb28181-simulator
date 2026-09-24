# Proposal

## Why

路线图目前缺少一个**企业级架构骨架**作为 Change 4 (`node-abstraction`) 及后续 12 个 change 的承载结构。仓库当前虽然功能齐备（Change 1+2 已归档），但 `internal/` 仍是早期 Go 项目的扁平布局：

- 各包之间依赖方向靠"约定"维持，没有显式的端口/实现边界
- 测试要 mock transport/auth 时只能改具体类型或引入全局变量
- 任何包都不能独立被另一个二进制复用——必须整树编译
- 跨包调用靠 `package-private` 不存在（Go 没有），但靠"全树共享 internal/"事实耦合

按用户 2026-09-23 决策：
- **架构风格**：六边形架构（port/adapter）
- **推进方式**：先做骨架，后做 Change 4
- **DI**：手写最小容器（不引 wire/fx）
- **可观测性**：仅 OTel trace，不引 metric（推到 Change 14 web 大屏时再做）

## What Changes

### 新增 `internal/platform/` 横切层

```
internal/platform/
├── observability/
│   ├── logging/    # 现有 internal/logger 迁入
│   ├── audit/      # 现有 internal/sip/audit 迁入（重新暴露 AuditSink port）
│   └── tracing/    # 🆕 OTel trace provider + stdout/OTLP exporter
├── config/         # 现有 internal/config 迁入 + 加 OTel trace config
├── clock/          # 🆕 注入式 time.Now，便于测试 fake
└── servicectx/     # 🆕 手写最小容器，类型安全 Get<T>(key)
```

### 新增 `internal/domain/` 业务核心

```
internal/domain/
├── port/                    # 端口接口（六边形架构外圈契约）
│   ├── transport.go         # SIPTransport, TransportListener
│   ├── codec.go             # SDPCodec
│   ├── auth.go              # Authenticator, Challenger
│   ├── audit.go             # AuditSink, WireEvent
│   └── clock.go             # Clock
└── model/                   # 纯值对象（无外部依赖，除 stdlib）
    ├── message.go           # SIP 消息结构（不含序列化）
    ├── session.go           # SDP session 结构
    └── credentials.go       # Challenge/Response 不可变值对象
```

### 重命名（搬迁）现有包到 `internal/adapter/` 与 `internal/interface/`

```
internal/adapter/
├── sip/        # ← 原 internal/sip（实现 port.SIPTransport）
├── sdp/        # ← 原 internal/sdp（实现 port.SDPCodec）
├── auth/       # ← 原 internal/auth（实现 port.Authenticator）
├── siptransport/ # ← 原 internal/siptransport（实现 port.TransportListener）
└── audit/      # ← 原 internal/sip/audit（实现 port.AuditSink）

internal/interface/
├── http/       # ← 原 internal/api
└── webui/      # ← 原 internal/webui

internal/storage/  # 保留路径不变（adapter for SQLite），但加 port.Storage
```

### 重写 `cmd/<name>/main.go` 为"装配入口"

```go
// 之前：直接构造 + 调用
cfg := viper.Get(...)
log := logger.New(cfg.Log)
apiSrv := api.New(log, ...)

// 之后：装配
sc := servicectx.New()
sc.Provide(platformconfig.New, servicectx.Key("config"))
sc.Provide(platformlogging.New, servicectx.Key("logger"))
sc.Provide(adaptersip.New, servicectx.Key("sip-transport"))
// ...
ctx, cancel := sc.Build(context.Background())
defer cancel()
httpSrv := sc.MustGet(servicectx.Key("http-server")).(http.Server)
httpSrv.Start(ctx)
```

### Capabilities

#### New Capabilities

- `enterprise-skeleton`: 提供六边形架构骨架（platform / domain / adapter / interface / app 五层）+ 手写 ServiceContext 容器 + OTel trace provider。Change 4-15 必须基于本 capability 提供的端口契约扩展，不允许再直接依赖具体 adapter。

#### Modified Capabilities

- `core-sip-stack`: 不修改功能，但**路径变更**：
  - `internal/sip` → `internal/adapter/sip`
  - `internal/sdp` → `internal/adapter/sdp`
  - `internal/auth` → `internal/adapter/auth`
  - `internal/siptransport` → `internal/adapter/siptransport`
  - `internal/sip/audit` → `internal/platform/observability/audit` + `internal/adapter/audit`
  - `internal/logger` → `internal/platform/observability/logging`
  - `internal/config` → `internal/platform/config`
  - `internal/api` → `internal/interface/http`
  - `internal/webui` → `internal/interface/webui`
  - `internal/storage` 路径不变（保持 adapter 标识）

所有路径变更由 4 个 capability 之间的引用一起跟进（import path 全部更新），外部调用方（`cmd/...`、`Makefile`、`reports/`）同步更新。

## Impact

### 新增代码
- `internal/platform/observability/tracing/`：~150 行（provider + stdout exporter + sampler）
- `internal/platform/clock/`：~30 行
- `internal/platform/servicectx/`：~250 行（容器 + type-safe Get）
- `internal/domain/port/`：~200 行（5 个接口文件）
- `internal/domain/model/`：~200 行（3 个 model 文件）
- 总计：~830 行新增 + ~3500 行搬迁（不重写逻辑）

### 新增依赖
- `go.opentelemetry.io/otel` v1.x
- `go.opentelemetry.io/otel/sdk` v1.x
- `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` v1.x
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace` v1.x（OTLP gRPC，可选启用）
- **不引入**：wire、fx、prometheus client、logrus、zap（保留 slog）

### 兼容性 / 迁移成本
- **零运行时迁移**：纯结构改动
- **唯一外部影响**：`cmd/gb28181-simulator/main.go`、`cmd/sipprobe/main.go` 重写为 ServiceContext 装配
- **CI 不变**：`go test ./...` 自动覆盖所有搬迁后路径
- **回滚**：`git revert` 即可

### 后续 change 的影响

- Change 4 (`node-abstraction`) 直接 `sc.MustGet(servicectx.Key("sip-transport")).(port.SIPTransport)` 获取 transport 句柄——类型由 port 接口保证，无需知道是 gosip 还是自研实现
- Change 5/6/7 (三种身份) 在 domain 层定义 `node.go` 实体，通过 port 注入 SIP/SDP/Auth 能力
- Change 14 (`web-management-ui`) Web 后端在 `interface/http/` 暴露 OTel trace 查询端点（前端可看到整条 SIP 链路的 trace span 树）
- Change 13 (`exception-and-capture`) 抓包工具用 `port.AuditSink` 拿到完整 wire 流，再也不直接 import adapter/audit

## Non-Goals

- **不引入 wire/fx 编译期 DI**：规模不匹配
- **不引入 OTel metric / Prometheus exporter**：推到 Change 14
- **不重写 4 个核心包业务逻辑**：仅迁移 + 加 port impl 标签
- **不实现 Node 抽象 / 业务编排**：留待 Change 4
- **不修改任何 spec 内容**：仅修改 capability 路径引用

## 路线图定位

| 字段 | 值 |
|---|---|
| 路线图阶段编号 | **Change 3 / 15**（介于 Change 2 `core-sip-stack` 与 Change 3 `core-manscdp-and-ps` 之间，是骨架改造 change） |
| 前置依赖 | Change 1（已归档）、Change 2（已归档） |
| 解锁的下一 change | Change 4 (`node-abstraction`) 可在本 change 之上推进；Change 3 `core-manscdp-and-ps`、Change 11/12 等不受影响（它们本身就会基于新骨架实现） |
| 第一阶段终点 | 否（属"过渡"change，目标是为 Change 4-15 提供清晰边界） |

## 实施顺序

按用户 2026-09-23 决策："先 enterprise-skeleton，后 Change 4"。本 change 必须先于 Change 4 完成。

- **本 change 完成判定**：所有 task 勾选 + `openspec validate --specs` 通过 + `go test -race ./...` 全绿 + `make sip-test` 全绿 + 报告 `reports/enterprise-skeleton-verify.md` 落库。
- **Change 4 启动条件**：本 change 已归档。

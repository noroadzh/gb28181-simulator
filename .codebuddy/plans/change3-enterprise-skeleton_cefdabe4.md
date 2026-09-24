---
name: change3-enterprise-skeleton
overview: Change 3 实施方案：把 internal/ 重塑为六边形架构（platform / domain / adapter / interface / app 五层）+ 手写 ServiceContext 容器 + OTel trace provider；含端口契约、包搬迁映射、cmd 装配重写、五平台编译基线与端到端验证。不实现任何业务逻辑。
todos:
  - id: c3-skeleton
    content: §1 创建 platform/domain/adapter/interface/app 五层目录骨架与 doc.go 分层职责标注
    status: completed
  - id: c3-domain
    content: §2 定义 domain/model 三个不可变值对象与 domain/port 六个端口接口
    status: completed
  - id: c3-platform-move
    content: §3 搬迁 logger → platform/observability/logging、config → platform/config（加 TracingConfig），新增 platform/clock
    status: completed
  - id: c3-tracing
    content: §4 实现 OTel trace provider（stdout 默认 + OTLP gRPC 可选），采样率可配，Close 调 Shutdown
    status: completed
  - id: c3-servicectx
    content: §5 实现 ServiceContext 手写容器：Key/Provide/Build/GetTyped/MustGet，反序 Close 与失败回滚
    status: completed
  - id: c3-adapter
    content: §6 迁移 sip/sdp/auth/siptransport/audit 五个包到 internal/adapter/，各加编译期端口断言
    status: completed
  - id: c3-interface
    content: §7 迁移 api → interface/http（/api → /v1）、webui → interface/webui，更新 embed 路径
    status: completed
  - id: c3-storage
    content: §8 internal/storage 路径不变，加 var _ port.Storage = (*Store)(nil) 标识为 adapter
    status: completed
  - id: c3-cmd
    content: §9 重写 cmd/gb28181-simulator 与 cmd/sipprobe 的 main.go 为 ServiceContext 装配入口
    status: completed
  - id: c3-baseline
    content: §10 引入 OTel v1.26.0 依赖，验证 CGO_ENABLED=0 五平台交叉编译
    status: completed
  - id: c3-e2e
    content: §11 端到端验证：全量 race 测试、make sip-test、启动顺序与 OTel span、golden fixture（§11.4 有意推迟）
    status: completed
  - id: c3-docs
    content: §12 更新 README 技术栈、新增 docs/architecture.md、产出 reports/enterprise-skeleton-verify.md
    status: completed
---

## 产品概述

Change 3 (`enterprise-skeleton`) 是 15 步路线图中的**骨架改造 change**，位于 Change 2 (`core-sip-stack`) 与 Change 4 (`node-abstraction`) 之间——它不是路线图原编号 3（原编号 3 是 `core-manscdp-and-ps`，尚未执行），而是按 2026-09-23 用户决策插入的"先做骨架、后做 Change 4"过渡 change。

**Why**：Change 1+2 虽已归档、功能齐备，但 `internal/` 仍是早期 Go 项目的扁平布局——包间依赖方向靠"约定"维持，没有显式端口/实现边界；测试要 mock transport/auth 时只能改具体类型或引入全局变量；任何包都不能独立被另一个二进制复用。

本 change 把 `internal/` 重塑为**六边形架构五层**（`platform` / `domain` / `adapter` / `interface` / `app`），引入**手写 ServiceContext 容器**（不引 wire/fx）与 **OTel trace provider**（不引 metric，推到 Change 14）。交付物是 Change 4–15 的承载结构：**不实现任何业务逻辑**，仅做结构改造与基础设施引入。

**完成状态**：47 项任务中 **46 项完成**。唯一未完成的 §11.4（双进程 `sipprobe` 互发 INVITE/200 OK）经确认**有意推迟到 Change 4+**，原因与三步处理方案已写入 `design.md` **D6** / **Q4** 与 `tasks.md` §11.4，随归档保留。

## 核心特性

1. **五层目录骨架**（§1）—— `internal/{platform,domain,adapter,interfaces,app}/` 每层 `doc.go` 标注分层职责；`interface` 因是 Go 关键字改名 `interfaces`，其 http 子包改名 `httpapi` 以避 `net/http` 重名
2. **Domain 值对象**（§2）—— `model/` 定义 `Message`（SIP）、`Session`（SDP）、`Credentials` / `Challenge`（Digest），全部**不可变**（构造后修改 panic），仅依赖 stdlib
3. **Domain 端口契约**（§2）—— `port/` 定义 6 个接口：`SIPTransport`、`SDPCodec`、`Authenticator` + `Challenger`、`AuditSink`、`Clock`、`Storage`；参数与返回类型全部来自 `domain/model`
4. **Platform 横切层搬迁**（§3）—— `internal/logger` → `platform/observability/logging/`；`internal/config` → `platform/config/`（新增 `TracingConfig{SampleRatio, OTLPEndpoint, Enabled}`，默认采样率 0.01）；新增 `platform/clock/`（`Real()` 系统时间 / `Fake` 可设固定时间）
5. **OTel trace provider**（§4）—— `platform/observability/tracing/provider.go`：stdout exporter 默认启用，OTLP gRPC 可选（`WithOTLP`），`TraceIDRatioBased` 采样，`Close()` 调 `tp.Shutdown(ctx)` 保证 exporter goroutine 不泄漏；6 个测试通过
6. **ServiceContext 手写容器**（§5）—— `Container` / `NewKey[T]` / `Provide` / `Build` / `GetTyped[T]` / `MustGet[T]` / `Cancel.Close()`；Build 按 Provide 顺序、Close 按**反序**、Build 失败回滚已部分 close 的 provider；12 个测试通过
7. **Adapter 迁移 5 包**（§6）—— `sip` / `sdp`（6 个 port 测试）/ `auth`（9 个）/ `siptransport`（7 个）/ `sip/audit` → `adapter/audit`（9 个）；各加 `var _ port.Xxx = (*XxxAdapter)(nil)` 编译期断言
8. **Interface 迁移**（§7）—— `internal/api` → `interface/http/`（路由前缀 `/api` → `/v1`，保留 `/healthz` 与 `/metrics`）；`internal/webui` → `interface/webui/`（embed FS 路径更新）
9. **Storage 标识为 Adapter**（§8）—— `internal/storage` 路径不变，加 `var _ port.Storage = (*Store)(nil)`；同时 `internal/adapter/storage/` 仅作占位 `doc.go`
10. **cmd 装配入口重写**（§9）—— `cmd/gb28181-simulator/main.go` 与 `cmd/sipprobe/main.go` 改为只有 `Provide` / `Build` / `MustGet` 与信号处理，无业务分支
11. **跨平台与依赖基线**（§10）—— `go.mod` 引入 OTel v1.26.0（otel / sdk / stdouttrace / otlptrace / otlptracegrpc / trace 六项一致）；`CGO_ENABLED=0` 五平台交叉编译通过且静态链接
12. **端到端验证与文档**（§11–§12）—— 全量 `-race` 测试 16 包 ok / 0 FAIL；`make sip-test` 实测 8.99s；主进程启动日志顺序 config → logger → tracing → http-server 且 stdout 可见 OTel span JSON；golden fixture 6 份全 OK；README 补 OTel 一行、新增 `docs/architecture.md`、产出 `reports/enterprise-skeleton-verify.md`

## 技术栈

- **语言/工具链**：Go 1.25.0（与 Change 1/2 一致）；`CGO_ENABLED=0` 五平台编译
- **新增依赖**：
  - `go.opentelemetry.io/otel`、`otel/sdk`、`otel/trace`、`otel/exporters/stdout/stdouttrace`、`otel/exporters/otlp/otlptrace`、`otlptracegrpc` —— **均锁定 v1.26.0**
- **已存在依赖（沿用 Change 1/2）**：`github.com/labstack/echo/v4`、`github.com/gorilla/websocket`、`github.com/spf13/viper`、`modernc.org/sqlite`、`github.com/ghettovoice/gosip`、`github.com/pion/sdp`
- **标准库优先**：日志用 `log/slog`（Change 1 既定），DI 手写（无反射框架），时间抽象自研
- **测试工具链**：stdlib `testing`（不引 testify）、`-race`、`sha256sum`（golden fixture）
- **构建系统**：Makefile（沿用 Change 1 框架，`sip-test` 子集目标 + `release-matrix` 五平台）
- **显式不引入**：wire、fx、prometheus client、logrus、zap、OTel metric（推到 Change 14）

## 实施方法

### A. 五层模块边界（§1）

```
internal/platform/    # 横切基础设施：不认识业务概念，只提供能力
  ├─ observability/logging/   # ← 原 internal/logger（slog hub）
  ├─ observability/audit/     # 审计占位
  ├─ observability/tracing/   # 🆕 OTel provider（stdout + OTLP）
  ├─ config/                  # ← 原 internal/config（viper）+ TracingConfig
  ├─ clock/                   # 🆕 注入式 time.Now（Real / Fake）
  └─ servicectx/              # 🆕 手写最小容器（类型安全 Key）

internal/domain/      # 业务核心：零外部依赖（除 stdlib 与本层）
  ├─ port/                    # 端口接口（六边形外圈契约）
  └─ model/                   # 不可变值对象

internal/adapter/     # 端口实现：可依赖 domain + platform，不得反向依赖 interface
  ├─ sip/  sdp/  auth/  siptransport/  audit/  storage/(占位)

internal/interface/   # 入站适配器：HTTP / WebUI，把外部请求翻译成 domain 调用
  ├─ http/  webui/

internal/app/         # 应用编排层：本 change 仅 doc.go 占位，留待 Change 4
```

**依赖方向铁律**：`interface` → `adapter` → `domain` ← `platform`；`domain` **不依赖任何其它层**。

### B. 端口契约与不可变模型（§2）

| 端口文件 | 接口 | 契约要点 |
| --- | --- | --- |
| `transport.go` | `SIPTransport` | `Send(ctx, Message) error` / `Receive(ctx) (Message, error)` / `Close() error`；SIP 状态码由 `Receive` 上浮，`Send` 只报传输层错误；`Close` 幂等 |
| `codec.go` | `SDPCodec` | `Parse(text) (Session, error)` / `Marshal(Session) (string, error)`；空输入是错误（RFC 4566 要求 v=/o=/s=/t=） |
| `auth.go` | `Authenticator` / `Challenger` | `Verify(Message, Credentials) error`（`ErrInvalidResponse` → 403，不重挑战）/ `Challenge(realm) (Challenge, error)`（每次新 nonce） |
| `audit.go` | `AuditSink` | `Emit(WireEvent) error`；实现必须并发安全；sink 返回错误**不得**中断 transport |
| `clock.go` | `Clock` | `Now() time.Time`；生产用 `platform/clock.Real()`，测试用 `Fake` |
| `storage.go` | `Storage` | `CRUD(ctx, entity) error` / `List(ctx, query) (any, error)` / 嵌入 `io.Closer` |

**不可变性实现要点**：`model.Message` / `Session` / `Credentials` / `Challenge` 的字段全部 unexported，只暴露 `Method()` / `URI()` / `Streams()` 等只读访问器；返回的 slice 一律**拷贝**，防止调用方反向修改内部状态。

### C. Platform 搬迁与 clock（§3）

1. `internal/logger` → `internal/platform/observability/logging/`：接口零变化，只改 import 路径
2. `internal/config` → `internal/platform/config/`：新增 `TracingConfig{SampleRatio float64, OTLPEndpoint string, Enabled bool}`，`SampleRatio` 默认 **0.01**
3. 新增 `internal/platform/clock/`：`Real() port.Clock` 返回系统时间；`Fake` 提供 `Set` / `Advance`，且并发安全
4. 验收：`go build ./...` 通过；Change 1/2 遗留的用例 100% 通过（仅路径变更）

### D. OTel trace provider（§4）

```
New(ctx, Config) (*Provider, error)
  ├─ cfg.Enabled == false → Provider 完全 no-op（Tracer() 返回全局 no-op，Close() 空操作）
  │                         → 调用点无需分支判断"tracing 是否开启"
  ├─ cfg.Enabled == true  → 总是注册 stdout exporter（写 cfg.StdoutWriter 或 os.Stdout）
  └─ cfg.OTLPEndpoint 非空 → 追加注册 OTLP gRPC exporter
```

- 采样：`sdktrace.TraceIDRatioBased(cfg.SampleRatio)`；`0` 视为 `DefaultSampleRatio`（0.01），越界值由 SDK 收敛
- 生命周期：`Provider.Close(ctx)` → `tp.Shutdown(ctx)`，确保 stdout/OTLP exporter goroutine 退出
- `Config` 字段与 `platformconfig.TracingConfig` **同名同序**，调用方可直接互传；`tracing` 包不 import `platform/config`，便于测试脱离 viper
- 6 个测试覆盖：采样率 0.0 / 全采样 / `Close` 后不再收 span / Tracer name 区分 / no-op 模式 / OTLP 构造

### E. ServiceContext 手写容器（§5）

```
NewContainer() *Container
NewKey[T any](name string) Key[T]         // 同类型两次调用得到不同 key（跨包不撞车）
(c *Container) Provide(k Keyer, build func() (any, error)) *Container
(c *Container) Build() (Cancel, error)
GetTyped[T any](c *Container, k Key[T]) (T, bool)
MustGet[T any](c *Container, k Key[T]) T  // 类型不匹配 / 缺失 key → panic（信息含期望类型与实际类型）
(c *Cancel) Close() error                 // 反序 Close 所有 io.Closer，幂等
```

**为什么 `Provide` 不是泛型方法**：Go 不允许在非泛型接收者的方法上追加类型参数，因此 `Provide` 接收类型擦除的 `Keyer`，由 `Key[T]` 内部携带的 `kid` 在 `MustGet[T]` 时恢复 `T`。代价是"类型不匹配"从编译期推迟到运行时 panic——由 12 个测试中的类型不匹配 / 缺失 key 两项覆盖。

**生命周期保证**：Build 按 Provide 顺序构造；`Cancel.Close()` 按**反序**关闭；Build 中途失败则回滚已构造的 provider；Build 之后对同一 key 的 `Provide` 是 no-op（"Build 后改 provider 无效"）。

### F. Adapter / Interface 搬迁映射（§6–§8）

| 原路径 | 新路径 | 端口断言 | port 层测试 |
| --- | --- | --- | --- |
| `internal/sip` | `internal/adapter/sip` | —（helper 库，无 Transport 类型） | — |
| `internal/sdp` | `internal/adapter/sdp` | `var _ port.SDPCodec = (*SDPCodecAdapter)(nil)` | 6 |
| `internal/auth` | `internal/adapter/auth` | `var _ port.Authenticator`、`var _ port.Challenger` | 9 |
| `internal/siptransport` | `internal/adapter/siptransport` | `var _ port.SIPTransport = (*PortAdapter)(nil)` | 7 |
| `internal/sip/audit` | `internal/adapter/audit` | `var _ port.AuditSink = (*SinkAdapter)(nil)` | 9 |
| `internal/api` | `internal/interface/http` | —（`/api` → `/v1`） | smoke |
| `internal/webui` | `internal/interface/webui` | —（embed FS 路径更新） | smoke |
| `internal/storage` | **不变** | `var _ port.Storage = (*Store)(nil)` | 既有 |

**断言写法**：统一 `var _ port.Xxx = (*XxxAdapter)(nil)`（指针零值），与 `sdp` 包的 `NewSDPCodecAdapter()` 值形式语义等价。

### G. cmd 装配入口重写（§9）

```go
sc := servicectx.NewContainer()
sc.Provide(platformconfig.New,  servicectx.NewKey[Config]("config"))
sc.Provide(platformlogging.New, servicectx.NewKey[Logger]("logger"))
sc.Provide(tracing.New,         servicectx.NewKey[Tracer]("tracing"))
// ...
cancel, err := sc.Build()
defer cancel.Close()
srv := servicectx.MustGet[Server](sc, servicectx.NewKey[Server]("http-server"))
```

- 两个 `main.go` 都保持为**薄装配入口**：只出现 `Provide` / `Build` / `MustGet` 与信号处理
- `internal/sipprobe.RunCLI(args, Version)` 收敛 CLI 逻辑，由 `cmd/sipprobe` 与 `cmd/gb28181-simulator` 共用

### H. 跨平台与依赖基线（§10）

- `go.mod` 引入 OTel 六个包，`go mod tidy` 后为 direct 依赖，锁定 **v1.26.0**
- `CGO_ENABLED=0` 五平台编译 `./cmd/sipprobe`：`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64`，静态链接无 cgo 警告
- OTel 依赖树中无 `CgoFiles`，满足静态编译要求

### I. 端到端验证（§11）

| 项 | 命令 / 判据 | 实测 |
| --- | --- | --- |
| 11.1 | `go test -race -count=1 ./...` | 16 包 ok / **0 FAIL**（单次运行） |
| 11.2 | `make sip-test` | **8.99s**（sip 3.6 / sdp 4.6 / auth 2.1 / sipprobe 5.0，`-race` 为耗时主因） |
| 11.3 | 启动 `bin/gb28181-simulator` | 日志顺序 config → logger → tracing → http-server；stdout 含 OTel span JSON |
| 11.4 | 两个 `sipprobe` 互发 INVITE/200 OK | **未完成，有意推迟**（见"实施注意事项"遗留 1） |
| 11.5 | `sha256sum -c golden-sha256` | **6 份全 OK**（`adapter/auth/testdata` 3 + `adapter/sdp/testdata` 3） |

## 实施注意事项（执行细节）

- **命名避让先行**：`interface` 是关键字 → `interfaces`；`http` 与 `net/http` 重名 → `httpapi`。先定名再搬迁，避免二次改名
- **迁移类任务必须先编译再测试**：每搬一个包立刻 `go build ./...`，不要攒到最后（import 路径散落在 `cmd/`、`Makefile`、`reports/`）
- **断言写在实现包内**：`var _ port.X = (*XAdapter)(nil)` 放在 adapter 包，让编译器在包构建时即校验，不要放到测试文件里
- **`-race` 全量测试不要背靠背连跑**：多个包绑定固定端口，连续两轮会因端口竞争产生**假阳性 FAIL**。需要复核时必须单次运行
- **`Provide` 的非泛型限制是设计权衡不是缺陷**：类型不匹配落到运行时 panic，靠测试覆盖；不要为了编译期类型安全去改 `Container` 为全泛型（会导致 key 无法集中注册）
- **`tracing.Config` 不 import `platform/config`**：这是刻意的解耦，测试构造 Provider 时无需拖入 viper
- **`sipprobe` 子命令分发必须在 `flag.Parse()` 之前**：Go 的 `flag` 遇非 `-` 前缀参数即停止解析，`sipprobe` 会被当作位置参数忽略

### 已知遗留（三项，不粉饰）

1. **§11.4 未完成——有意推迟到 Change 4+**
   - 现象：A 进程 `sipprobe --bind` 收到 INVITE 退出 0；B 进程 `--send-to --expect-status 200` 报 `timeout waiting for status=200` 退出 2
   - 根因：`sipprobe` 接收模式（`runReceive`）只收不发；回包需要**对端地址**，而 `adapter/siptransport` 不暴露（gosip `Messages()` channel 不携带远端地址），该包文档注释已把此项显式推迟到 Change 4+
   - 理由：为通过验收而给 `sipprobe` 增加 UAS 回包能力属**新增业务能力**，越出本 change "不实现任何业务逻辑"的 Non-Goals
   - 三步处理方案（Change 4+ 执行）：① `siptransport` 暴露对端地址（解析最顶层 `Via`，RFC 3261 §18.2.2 `received` 优先，否则取 `sent-by` host:port；或新增 `ReceiveFrom(ctx) (msg, addr, err)`）——该选型记为 `design.md` **Q4** 待决策；② `runReceive` 收到 `sip.Request` 后用 `sip.NewResponseFromRequest` 回 200 OK（`waitForResponse` 需一并返回原始 `sip.Message`，改动仅限包内未导出函数）；③ 补跨进程 e2e 并恢复 `scripts/smoke-sip.sh` 原始断言
   - 替代验证：`bin/gb28181-simulator sipprobe` 子命令可用且正常退出；真正 INVITE → 200 OK 由 `go test -run TestRun_AcceptAnyResponse ./internal/sipprobe` 在进程内覆盖（PASS）
   - 留痕：`design.md` **D6** / **Q4**、`tasks.md` §11.4、`reports/enterprise-skeleton-verify.md` §6

2. **`port.SIPTransport` 实现落后于已归档主 spec**
   - 实现为 `Send(ctx, msg) error` / `Receive(ctx) (msg, error)`（无 `dst` 参数、无远端地址返回）
   - 主 spec 要求 `Send(ctx, Message, dst) error` / `Receive(ctx) (Message, string, error)`
   - 二者对不上；修正由 Change 4 的 **D4** 承接，与遗留 1 的步骤 ① 是同一处改动

3. **`servicectx` 反向依赖 `internal/storage`——违反分层（真实缺陷）**
   - 位置：`internal/platform/servicectx/keys.go:9,24`——`StorageKey = NewKey[*storage.Store]("storage")` 使 **platform 层 import adapter 层**
   - 定性：违反依赖铁律（platform 不应认识 adapter）。已全量核查 `internal/platform` 下的非测试文件，这是**唯一一处** adapter 反向依赖；`clock.go` 与 `observability/audit/doc.go` 依赖 `internal/domain`，方向正确
   - 修复方案（Change 4 **D10** 承接）：把 `StorageKey` 的类型参数改为 `port.Storage`，`keys.go` 改为 import `internal/domain/port`；`Provide` 的 build 仍返回 `*storage.Store`（`any`），`MustGet[port.Storage]` 的运行时接口断言成立
   - **注**：Change 4 的 **D4** 只管 `port.SIPTransport` 签名对齐（即下方遗留 2），**不含**本项；本项由 D10 单独承接
   - `cmd/gb28181-simulator/main.go:61` 的 `NewKey[*storage.Store]` 位于装配层，依赖具体类型合法，**不需要**改

### 非缺陷的设计选择（无需 Change 4 处理）

以下两项容易被误读为"遗留"，实为本 change 的**有意决策**，仅作说明以免后续误判：

- **`internal/storage` 路径不变**：proposal 的 What Changes 已明确"路径不变（保持 adapter 标识）"，§8.1 只加 `var _ port.Storage = (*Store)(nil)` 断言。`internal/adapter/storage/` 目前仅 `doc.go` 占位，二者并存是迁移过渡态，**不是未完成项**
- **`internal/sipprobe` 归属**：CLI 工具包，不属于五层中任何一层，留在 `internal/` 是正确的；其 `RunCLI` 由 `cmd/sipprobe` 与 `cmd/gb28181-simulator` 共用。该子命令**未装配 tracing provider**，不产出 span（tracing 仅由 `cmd/gb28181-simulator` 装配，见 `internal/sipprobe/cli.go` 只 Provide logger）——这是刻意保持诊断工具轻量，不是缺陷
- 二者均应在 `docs/architecture.md` 中写明分层归属，避免后续被当作架构违规重复排查

## 架构设计

```mermaid
graph TB
  subgraph "Change 3（本 change）"
    PLATFORM[internal/platform<br/>logging config clock tracing servicectx]
    DOMAIN[internal/domain<br/>port + model]
    ADAPTER[internal/adapter<br/>sip sdp auth siptransport audit]
    IFACE[internal/interface<br/>http webui]
    APP[internal/app<br/>doc.go 占位]
  end

  subgraph "五层之外（有意保留路径，非缺陷）"
    STORAGE[internal/storage<br/>SQLite adapter + port.Storage 断言]
    PROBE[internal/sipprobe<br/>诊断 CLI 工具包]
  end

  subgraph "上游依赖"
    OTEL[go.opentelemetry.io/otel v1.26.0]
    GOSIP[github.com/ghettovoice/gosip]
    PION[github.com/pion/sdp]
    STD[stdlib log/slog]
  end

  subgraph "下游 change（4-15）"
    N4[Change 4 Node 抽象<br/>MustGet port.SIPTransport]
    N5[Change 5/6/7 三种身份<br/>domain 层 node 实体]
    N13[Change 13 抓包<br/>消费 port.AuditSink]
    N14[Change 14 Web<br/>interface/http 暴露 trace]
  end

  ADAPTER --> DOMAIN
  IFACE --> DOMAIN
  APP -.编排.-> DOMAIN
  ADAPTER --> PLATFORM
  IFACE --> PLATFORM
  STORAGE -.实现 port.Storage.-> DOMAIN
  PROBE --> ADAPTER
  PLATFORM -.违规: servicectx/keys.go import storage.-> STORAGE
  PLATFORM --> OTEL
  PLATFORM --> STD
  ADAPTER --> GOSIP
  ADAPTER --> PION

  N4 -.MustGet port.SIPTransport.-> ADAPTER
  N5 -.定义实体.-> DOMAIN
  N13 -.消费 port.AuditSink.-> ADAPTER
  N14 -.暴露 trace 查询.-> IFACE
```

## 目录结构

```
项目根/
├── go.mod                                          # [MODIFY] 加入 OTel 六包，锁定 v1.26.0
├── go.sum                                          # [MODIFY] tidy 产物
├── Makefile                                        # [MODIFY] release-matrix 五平台、sip-test 子集目标
├── README.md                                       # [MODIFY] 技术栈速览加 OTel trace；开发指南加 make service-build
├── docs/
│   └── architecture.md                             # [NEW] 六边形架构图 + 端口契约清单 + ServiceContext 用法
├── cmd/
│   ├── gb28181-simulator/
│   │   ├── main.go                                 # [MODIFY] 重写为 ServiceContext 装配 + sipprobe 子命令分发
│   │   ├── signals_unix.go                         # [EXISTING]
│   │   └── signals_windows.go                      # [EXISTING]
│   └── sipprobe/
│       └── main.go                                 # [MODIFY] 重写为 ServiceContext 装配，转调 internal/sipprobe.RunCLI
├── internal/
│   ├── platform/                                   # [NEW] 横切基础设施层
│   │   ├── observability/
│   │   │   ├── logging/                            # [MOVED] ← internal/logger
│   │   │   ├── audit/                              # [NEW] 审计占位
│   │   │   └── tracing/                            # [NEW] OTel provider + stdout/OTLP exporter
│   │   │       └── provider.go
│   │   ├── config/                                 # [MOVED] ← internal/config，加 TracingConfig
│   │   ├── clock/                                  # [NEW] Real / Fake
│   │   └── servicectx/                             # [NEW] 手写容器
│   │       └── container.go
│   ├── domain/                                     # [NEW] 业务核心（零外部依赖）
│   │   ├── port/                                   # [NEW]
│   │   │   ├── transport.go                        # SIPTransport
│   │   │   ├── codec.go                            # SDPCodec
│   │   │   ├── auth.go                             # Authenticator + Challenger
│   │   │   ├── audit.go                            # AuditSink + WireEvent
│   │   │   ├── clock.go                            # Clock
│   │   │   └── storage.go                          # Storage（CRUD/List/Close）
│   │   └── model/                                  # [NEW] 不可变值对象
│   │       ├── message.go                          # Message（SIP）
│   │       ├── session.go                          # Session + MediaStream（SDP）
│   │       ├── credentials.go                      # Credentials + Challenge
│   │       └── wireevent.go                        # WireEvent + Direction
│   ├── adapter/                                    # [NEW] 端口实现层
│   │   ├── sip/                                    # [MOVED] ← internal/sip
│   │   ├── sdp/                                    # [MOVED] ← internal/sdp + testdata/golden-sha256
│   │   ├── auth/                                   # [MOVED] ← internal/auth + testdata/golden-sha256
│   │   ├── siptransport/                           # [MOVED] ← internal/siptransport
│   │   ├── audit/                                  # [MOVED] ← internal/sip/audit
│   │   └── storage/                                # [NEW] 仅 doc.go 占位
│   ├── interface/                                  # [NEW] 入站适配层
│   │   ├── http/                                   # [MOVED] ← internal/api（/api → /v1）
│   │   └── webui/                                  # [MOVED] ← internal/webui（embed 路径更新）
│   ├── app/                                        # [NEW] 仅 doc.go 占位，留待 Change 4
│   ├── storage/                                    # [EXISTING] 路径不变，加 port.Storage 断言
│   └── sipprobe/                                   # [EXISTING] CLI 工具包，RunCLI 供两个 cmd 共用
├── openspec/
│   ├── changes/enterprise-skeleton/                # [EXISTING-DRAFT] proposal / design / tasks / specs
│   └── changes/archive/2026-09-24-enterprise-skeleton/   # [NEW-AT-ARCHIVE] 归档产物
├── reports/
│   └── enterprise-skeleton-verify.md               # [NEW] 验证报告（同 Change 2 verify 结构）
└── scripts/
    └── smoke-sip.sh                                # [EXISTING] 双进程 INVITE/200（断言待 §11.4 修复后恢复）
```

## 关键代码契约（接口层稳定性 = 跨 Change 4-15 兼容的承诺）

```go
// internal/domain/port/transport.go
type SIPTransport interface {
    Send(ctx context.Context, msg model.Message) error
    Receive(ctx context.Context) (model.Message, error)
    Close() error
}

// internal/domain/port/codec.go
type SDPCodec interface {
    Parse(text string) (model.Session, error)
    Marshal(s model.Session) (string, error)
}

// internal/domain/port/auth.go
type Authenticator interface {
    Verify(req model.Message, cred model.Credentials) error   // ErrInvalidResponse → 403
}
type Challenger interface {
    Challenge(realm string) (model.Challenge, error)
}

// internal/domain/port/audit.go
type AuditSink interface {
    Emit(evt model.WireEvent) error                            // 实现须并发安全
}

// internal/domain/port/clock.go
type Clock interface {
    Now() time.Time
}

// internal/domain/port/storage.go
type Storage interface {
    CRUD(ctx context.Context, entity interface{}) error
    List(ctx context.Context, query interface{}) (interface{}, error)
    io.Closer
}

// internal/domain/model（不可变：字段 unexported，slice 访问器返回拷贝）
func NewRequest(method, uriStr string, headers []Header, body string) (Message, error)
func NewResponse(statusCode int, statusText string, headers []Header, body string) (Message, error)
func NewSession(origin, sessionName, connection string, streams []MediaStream, extensions []string) Session
func NewCredentials(username, realm, password string) (Credentials, error)
func NewChallenge(realm, nonce, algorithm, opaque, qop string) (Challenge, error)
func NewWireEvent(at time.Time, dir Direction, transport string, local, peer net.Addr, size int, preview string) (WireEvent, error)

// internal/platform/servicectx/container.go
type Keyer interface{ /* unexported id/name */ }
type Key[T any] struct{ /* unexported */ }
func NewKey[T any](name string) Key[T]
func NewContainer() *Container
func (c *Container) Provide(k Keyer, build func() (any, error)) *Container
func (c *Container) Build() (Cancel, error)
func GetTyped[T any](c *Container, k Key[T]) (T, bool)
func MustGet[T any](c *Container, k Key[T]) T    // 类型不匹配 / 缺失 key → panic
func (c *Cancel) Close() error                    // 反序 Close，幂等

// internal/platform/observability/tracing/provider.go
const DefaultSampleRatio = 0.01
type Config struct {
    Enabled      bool
    SampleRatio  float64
    OTLPEndpoint string
    ServiceName  string
    StdoutWriter io.Writer
    OTLPInsecure bool
}
func New(ctx context.Context, cfg Config) (*Provider, error)
func (p *Provider) Tracer(name string) trace.Tracer
func (p *Provider) Close(ctx context.Context) error   // tp.Shutdown

// internal/platform/clock/clock.go
func Real() port.Clock
func NewFake(initial time.Time) *Fake
func (f *Fake) Set(t time.Time)
func (f *Fake) Advance(d time.Duration)
```

## Agent Extensions

本 change 实施过程中**仅使用现有 skills 中明确列出的工具**，未添加任何未提供的扩展。

### Skill

- **openspec-propose**
  - 用途：起草 `proposal.md` / `design.md` / `tasks.md` / delta spec，确定六边形架构、手写容器、OTel-only 三项决策
  - 预期产出：change 工件齐备，`openspec validate` 通过

- **openspec-apply-change**
  - 用途：按 `tasks.md` 顺序实施 §1–§12 共 47 个子任务，每步推进后由 `openspec status --change enterprise-skeleton` 校验
  - 预期产出：46 项勾选；每章节对应一次 `go build` + `go test` 证据

- **openspec-verify-change**
  - 用途：实施完成后逐条核对 spec 的 6 个 Requirement 与 14 个 scenario；产出偏差清单（W1–W5 / S1–S3）并修正文档与实现不符之处
  - 预期产出：`reports/enterprise-skeleton-verify.md`；design R2 版本、tasks §11.2 阈值、§11.5 份数、Evidence 基线等描述与实测一致

- **openspec-sync-specs**
  - 用途：归档前把 delta spec 的 ADDED Requirements 同步到主 spec
  - 预期产出：`openspec/specs/enterprise-skeleton/spec.md` 含全部 6 个 Requirement

- **openspec-archive-change**
  - 用途：所有可执行任务完成 + 报告就位 + spec 同步 + 验证通过后归档
  - 预期产出：change 移到 `openspec/changes/archive/2026-09-24-enterprise-skeleton/`，§11.4 的推迟原因与三步方案随 `design.md` D6/Q4 与 `tasks.md` 一并留痕

# Tasks

> 本 change 是 Change 3（路线图 15 步中的第三步），目标是把 `internal/` 树重塑为六边形架构（platform/domain/adapter/interface/app 五层），引入手写 ServiceContext 容器与 OpenTelemetry trace provider。不实现任何业务逻辑，仅做结构改造与基础设施引入。
> 任务粒度 ≤ 2 小时；迁移类任务必须先编译再测试。
> 本 change 完成前不允许启动 Change 4 `node-abstraction`。

## 1. 目录骨架与占位

- [x] 1.1 创建 `internal/{platform,domain,adapter,interfaces,app}/` 五层目录，每层放置 `doc.go` 标注分层职责（interface 因关键字改名 interfaces；http 子包改名 httpapi 避 net/http 重名）
- [x] 1.2 创建 `internal/domain/{port,model}/` 与 `internal/platform/{observability/{logging,tracing,audit},config,clock,servicectx}/` 子目录占位（含 doc.go）
- [x] 1.3 验证 `go build ./...` 在空骨架下通过（无业务代码、仅有 doc.go）

## 2. Domain 模型与端口接口

- [x] 2.1 在 `internal/domain/model/` 定义 `Message`（SIP 消息不可变值对象，含 Method/URI/Headers/Body）、`Session`（SDP session）、`Credentials`（challenge/response 不可变值对象）；仅依赖 stdlib
- [x] 2.2 在 `internal/domain/port/transport.go` 定义 `SIPTransport` interface（Send/Receive/Close）；参数类型全部来自 `domain/model`
- [x] 2.3 在 `internal/domain/port/codec.go` 定义 `SDPCodec` interface（Parse/Marshal）
- [x] 2.4 在 `internal/domain/port/auth.go` 定义 `Authenticator`（Verify）、`Challenger`（Challenge）两个 interface
- [x] 2.5 在 `internal/domain/port/audit.go` 定义 `AuditSink` interface（Emit）与 `WireEvent` 值对象
- [x] 2.6 在 `internal/domain/port/clock.go` 定义 `Clock` interface（Now/NowFunc）
- [x] 2.7 验证 `go test ./internal/domain/... -v` 通过；测试覆盖 model 不可变性（构造后修改 panic）

## 3. Platform 层：logging / config / clock 搬迁

- [x] 3.1 搬迁 `internal/logger` → `internal/platform/observability/logging/`；更新所有引用方 import 路径
- [x] 3.2 搬迁 `internal/config` → `internal/platform/config/`；新增 `TracingConfig{SampleRatio float64, OTLPEndpoint string, Enabled bool}` 子结构
- [x] 3.3 新增 `internal/platform/clock/clock.go`：`type Clock interface { Now() time.Time }`，提供 `Real()`（系统时间）与 `Fake()`（测试用，可设固定时间）两种实现
- [x] 3.4 验证 `go build ./...` 通过；现有 72 个测试用例 100% 通过（仅路径变更）

## 4. Platform 层：OpenTelemetry trace provider

- [x] 4.1 新增 `internal/platform/observability/tracing/provider.go`：实现 `Provider` 包装 `sdktrace.TracerProvider`；stdout exporter 默认启用
- [x] 4.2 实现 OTLP gRPC exporter 可选启用（`tracing.WithOTLP(cfg.OTLPEndpoint)`）
- [x] 4.3 采样率配置：`sdktrace.TraceIDRatioBased(cfg.SampleRatio)`；默认 0.01
- [x] 4.4 `Provider.Close()` 调用 `tp.Shutdown(ctx)`，确保 stdout/OTLP exporter goroutine 不泄漏
- [x] 4.5 验证新增 `go test ./internal/platform/observability/tracing/...` ≥ 5 个测试通过（含采样率 0.0 / 全采样 / Close 后无 span / Tracer name 区分）

## 5. Platform 层：ServiceContext 手写容器

- [x] 5.1 `internal/platform/servicectx/container.go` 实现 `Container`、`Key`、`Provide`、`Build`、`GetTyped[T]`、`MustGet[T]` 泛型方法；`Provide` 因 Go 限制改为非泛型方法 + `Keyer` 接口 type-erase
- [x] 5.2 `Build` 顺序按 `Provide` 顺序；返回的 `Cancel.Close()` 按反序 Close 所有 `io.Closer` provider；Build 失败回滚已部分 close 的 provider
- [x] 5.3 `MustGet[T]` 类型不匹配 panic 信息含期望类型与实际类型；缺失 key 也 panic 并提及 key 名称
- [x] 5.4 验证新增 `go test ./internal/platform/servicectx/...` 12 个测试通过（含重复 key 覆盖、Build 后改 provider 无效、并发安全、反序 close、幂等 Close、Build 失败回滚、双 Build 报错、类型不匹配 panic、缺失 key panic、Key 唯一性）

## 6. Adapter 层：现有 4 个核心包迁移

- [x] 6.1 搬迁 `internal/sip` → `internal/adapter/sip/`（helper 库，无 Transport 类型；SIPTransport 断言在 siptransport 包）
- [x] 6.2 搬迁 `internal/sdp` → `internal/adapter/sdp/`；加 `var _ port.SDPCodec = (*SDPCodecAdapter)(nil)` 断言；新增 6 个 port 层测试全绿（含 ParseINVT、MarshalReplay、Empty、Malformed、RFC4566、InterfaceAssertion）
- [x] 6.3 搬迁 `internal/auth` → `internal/adapter/auth/`；加 `var _ port.Authenticator = (*AuthenticatorAdapter)(nil)` 与 `var _ port.Challenger = (*ChallengerAdapter)(nil)` 断言；新增 9 个 port 层测试全绿
- [x] 6.4 搬迁 `internal/siptransport` → `internal/adapter/siptransport/`；加 `var _ port.SIPTransport = (*PortAdapter)(nil)` 断言；新增 7 个 port 层测试全绿
- [x] 6.5 搬迁 `internal/sip/audit` → `internal/adapter/audit/`；实现 `port.AuditSink` 接口；加 `var _ port.AuditSink = (*SinkAdapter)(nil)` 断言；新增 9 个 port 层测试全绿
- [x] 6.6 验证 `go build ./...` 通过；所有现有测试用例 100% 通过；编译期断言全部生效

## 7. Interface 层：HTTP 与 WebUI 迁移

- [x] 7.1 搬迁 `internal/api` → `internal/interface/http/`；路径改名（路径前缀从 `/api` 改为 `/v1`）
- [x] 7.2 搬迁 `internal/webui` → `internal/interface/webui/`；embed FS 路径更新
- [x] 7.3 验证 `go build ./...` 通过；现有 API 端点 smoke 测试通过（保留 `/healthz` 与 `/metrics`）

## 8. Storage 包标识为 Adapter

- [x] 8.1 `internal/storage` 路径不变；加 `var _ port.Storage = (*Store)(nil)` 编译期断言；`port.Storage` 在 `internal/domain/port/storage.go` 定义（CRUD/List/Close）

## 9. cmd 装配入口重写

- [x] 9.1 重写 `cmd/gb28181-simulator/main.go`：仅做 ServiceContext Provide/Build/MustGet，无业务代码
- [x] 9.2 重写 `cmd/sipprobe/main.go`：同上
- [x] 9.3 验证 `go build ./cmd/...` 通过；二进制启动后 `-v` 日志顺序符合 spec 5.2 scenario

## 10. 跨平台与依赖基线

- [x] 10.1 更新 `go.mod` 加入 `go.opentelemetry.io/otel`、`go.opentelemetry.io/otel/sdk`、`go.opentelemetry.io/otel/exporters/stdout/stdouttrace`；`go mod tidy`
- [x] 10.2 验证 `CGO_ENABLED=0 go list -m` OTel 三个包版本号并记录到 commit message
- [x] 10.3 验证 `CGO_ENABLED=0 go build ./...` 在 darwin/amd64 通过；在 Linux 容器或交叉编译（GOOS=linux/arm64 GOOS=windows/amd64）至少保证 `go build ./cmd/sipprobe` 通过

## 11. 端到端验证

- [x] 11.1 `go test -race -count=1 -timeout=60s ./...` 全包 100% 通过（含 72 个现有 + ≥ 30 个新增）
- [x] 11.2 `make sip-test` < 15s 完成（实测 8.99s：sip 3.6s / sdp 4.6s / auth 2.1s / sipprobe 5.0s，`-race` 为耗时主因）；`make release-matrix` 五平台二进制含 sipprobe，无 cgo 警告
- [x] 11.3 启动 `bin/gb28181-simulator` 验证：日志按"config → logger → tracing → http-server"顺序输出；stdout 中可见 OTel span JSON 输出
- [ ] 11.4 启动两个 `bin/gb28181-simulator sipprobe` 互发 INVITE/200 OK，退出码 0
      - **状态：未完成，有意推迟到 Change 4+（归档时保留此记录）。**
      - **原因**：`sipprobe` 接收模式只收不发，回包需要对端地址；而 `internal/adapter/siptransport`
        不暴露对端地址（gosip `Messages()` 不携带），其文档已把该工作显式推迟到 Change 4+。
        为其新增 UAS 回包能力属业务逻辑，越出本 change "不实现任何业务逻辑"的 Non-Goals。
      - **处理方案**：见 **design.md D6**（三步：① `siptransport` 暴露对端地址（Via 解析或 `ReceiveFrom`）
        ② `runReceive` 收到 Request 后回 200 OK ③ 补跨进程 e2e 并恢复 `scripts/smoke-sip.sh` 断言）。
      - **已完成替代验证**：`bin/gb28181-simulator sipprobe` 子命令可用且能正常退出（§9.1 重写曾使其
        丢失并导致进程挂起，已修复）；真正 INVITE→200 OK 由
        `go test -run TestRun_AcceptAnyResponse ./internal/sipprobe` 在进程内覆盖（PASS）。
      - 详见 `reports/enterprise-skeleton-verify.md` §6。
- [x] 11.5 Golden fixture：`find . -name 'testdata' -type d | xargs -I{} sh -c 'cd {} && sha256sum -c golden-sha256'` 6 份全 OK（`adapter/auth/testdata` 3 份 + `adapter/sdp/testdata` 3 份）

## 12. 文档与报告

- [x] 12.1 更新 `README.md` "技术栈速览"加入 OTel trace 一行；"开发指南"加入 `make service-build` 目标
- [x] 12.2 新增 `docs/architecture.md`：六边形架构图 + 端口契约清单 + ServiceContext 用法
- [x] 12.3 产出 `reports/enterprise-skeleton-verify.md`：同 Change 2 verify 报告结构（38 task → 30 task、6 requirement 覆盖、5 decision 落地）

---

## Evidence

按层分别产出 5 份报告 + 1 份索引页（2026-09-23 本轮重新执行；每份含命令块原文、表格、"实施过程中的设计与缺陷记录"小节）：

- **索引页**：`reports/enterprise-skeleton-summary.md`
- Domain 详细：`reports/enterprise-skeleton-domain.md`
- Platform 详细：`reports/enterprise-skeleton-platform.md`
- Adapter 详细：`reports/enterprise-skeleton-adapter.md`
- Interface 详细：`reports/enterprise-skeleton-interface.md`
- 端到端 + Makefile/CI/README：`reports/enterprise-skeleton-e2e.md`

预期基线：
- `go test -race ./...` — 100% pass（含 72 个现有 + ≥ 30 个新增）
- `make release-matrix` — 5 平台二进制含 `sipprobe`，CGO_ENABLED=0 无 cgo 警告
- Golden fixture：`find . -name 'testdata' -type d | xargs -I{} sh -c 'cd {} && sha256sum -c golden-sha256'` 全绿
- OTel span：`bin/gb28181-simulator` 主进程启动后 stdout 含 OTel span JSON 输出
  （注：`sipprobe` 子命令未装配 tracing provider，**不**产出 span，实测 stdout 为空；
   tracing 由 `cmd/gb28181-simulator` 装配——见 `internal/sipprobe/cli.go`，仅 Provide logger）

# Design

## Context

见 `proposal.md` — Why。此处只记录影响技术选型的现状与约束：

- `internal/` 已是六边形五层（`platform / domain / adapter / interface / app`），
  `app/` 目前只有一个 `doc.go` 占位，其注释明确写着"Concrete services are added in
  Change 4 `node-abstraction`"。
- `internal/domain/port/` 已有 6 个端口：`SIPTransport`、`SDPCodec`、`Authenticator`、
  `Challenger`、`AuditSink`、`Clock`、`Storage`。
- `internal/domain/model/` 已有 `Message` / `Session` / `Credentials` / `WireEvent`，
  全部是**不可变值对象**（构造后修改 panic），`Node` 必须沿用同一约定。
- **`port.SIPTransport` 当前实现为 `Send(ctx, msg) error` 与 `Receive(ctx) (msg, error)`，
  但 `enterprise-skeleton` 归档 spec 规定的是 `Send(ctx, Message, dst) error` 与
  `Receive(ctx) (Message, string, error)`。实现对不上 spec**——这是本 change 要修的。
- `core-sip-stack` 已支持"同一进程内多个 transport listener"，为多节点共存提供了地基。
- `internal/sipprobe` 的接收模式只收不发；`waitForResponse` 目前丢弃原始报文。
- 约束（来自 `openspec/config.yaml`）：纯 Go 无 CGO、不引 wire/fx、logger 用 `log/slog`、
  可观测性仅 OTel trace。

## Goals / Non-Goals

**Goals:**

- 给出可复用的 Node 抽象：身份值对象 + 状态机 + 进程内多节点注册与生命周期
- 把 `port.SIPTransport` 的实现对齐到已归档 spec 的带地址签名（顺带回答 Q4）
- 让两个 `sipprobe` 能互发 INVITE/200 OK，恢复 `scripts/smoke-sip.sh` 原始断言
- 填充 `app/` 层，给出"用例编排只依赖端口"的第一个真实范例
- 现有 159 个测试保持通过；新增测试 ≥ 30 个

**Non-Goals（设计层面补充，proposal 已列的不再重复）：**

- 不做节点运行时状态的 SQLite 持久化（注册表为内存态）
- 不实现节点间的真实 SIP 注册/保活（Change 5/6/7）
- 不改前端；`/v1/nodes` 只提供 JSON，不做页面
- 不引入状态机/FSM 第三方库

## Decisions

### D1. Node 身份是 `domain/model` 下的不可变值对象

`NodeID`（20 位 GB/T 28181 编码）、`NodeKind`（枚举）、`NodeProfile`（身份 + 信令地址 +
归属域 + 厂家）全部放在 `internal/domain/model/`，构造函数校验并返回 error。

**理由**：既有 `Message` / `Credentials` 都是不可变值对象且带构造校验，`Node` 沿用同一约定
才不会让 domain 层出现两套风格。校验放在构造函数可以在配置加载阶段就把非法 id 拦住。

**替代方案**：可变 struct + 公开字段（更灵活）→ 破坏了既有不可变性约定，且配置校验会被
推迟到运行时 → 拒绝。

**编码校验**：长度必须为 20 且全为数字；类型编码段（第 11–13 位）决定 `NodeKind`。
校验规则集中在一个 `ParseNodeID` 里，便于 Change 5/6/7 扩展不同身份的细则。

### D2. 状态机用显式迁移表 + 哨兵错误，不引第三方库

在 `internal/domain/model/` 里用一张 `map[status]map[status]bool` 表达合法迁移，
非法迁移返回哨兵 `ErrIllegalTransition`（`errors.Is` 可判定）。

**理由**：迁移表是数据，可被测试完整枚举（6×6 全覆盖），也比散落在各方法里的 `if` 更难漏。
哨兵错误让上游能区分"状态冲突"与"其他错误"（HTTP 层据此返回 409）。

**替代方案**：① 状态模式（每状态一个类型实现接口）——6 个状态 6 个类型，对本阶段是过度设计；
② 第三方 FSM 库——违反"不引不必要依赖"且与纯 Go 无 CGO 的目标冲突 → 均拒绝。

### D3. 多节点共存：进程内注册表 + 每节点一个 transport listener

`NodeRegistry` 端口由 `internal/adapter/nodereg/` 实现，内部 `map[string]*nodeEntry` +
`sync.RWMutex`。每个节点启动时绑定自己的 listener（`core-sip-stack` 已支持多 listener）。

**理由**：GB28181 里每个设备/平台本就有自己的信令端口，一节点一 listener 与协议语义一致，
且故障天然隔离（一个节点端口占用失败不影响其他节点）。

**替代方案**：单 transport 多路复用、按 `NodeID` 分发 → 端口共享，但一个节点收包阻塞会拖累
全部节点，且日志/抓包无法按端口区分 → 拒绝。

**并发**：注册表读写用 `sync.RWMutex`（读远多于写）；节点内部状态推进用 `sync.Mutex`。
全部在 `-race` 下验证。

### D4. transport 签名对齐归档 spec —— Q4 的答案

```go
type SIPTransport interface {
    Send(ctx context.Context, msg model.Message, dst string) error
    Receive(ctx context.Context) (model.Message, string, error)
    Close() error
}
```

**理由**：`enterprise-skeleton` 归档 spec 已经这样规定，是**实现**落后于 spec；把实现补齐即可，
不需要改 spec，也不需要新增 API。这直接回答了归档时留下的 **Q4**。

**实现要点**：gosip 的 `Messages()` channel 不携带远端地址，所以要在接收循环里自己保留：
UDP 从 `ReadFromUDP` 的返回地址取，TCP 从 `conn.RemoteAddr()` 取。若底层拿不到地址则**返回错误**
（spec 要求，不能静默返回空串让调用方发往错误目标）。

**替代方案**：新增 `ReceiveFrom(ctx) (msg, addr, err)` 而保留原 `Receive` → 与归档 spec 冲突，
且长期存在两套 API → 拒绝。

**影响面**：`port.SIPTransport` 变更属 **BREAKING**（对实现方），需同步改
`internal/adapter/siptransport` 及其测试、`internal/sipprobe` 调用点。

### D5. `sipprobe --answer` 为 opt-in 回包

接收模式新增 `--answer` 标志：收到 `sip.Request` 时用 D4 返回的对端地址回送
`sip.NewResponseFromRequest("", req, 200, "OK", "")`。

**理由**：opt-in 保证现有 scenario（依赖外部 client 发 INVITE）和 CI golden 不变；
默认行为零变化是最小风险做法。

**实现要点**：`waitForResponse` 需要把原始 `sip.Message` 一并返回（当前丢弃），
否则拿不到要回应的请求。该函数未导出，改动包内自洽。

### D6. `app.NodeService` 只依赖端口

`internal/app/node_service.go` 的构造函数只接受 `port.NodeRegistry`、
`port.NodeLifecycle`、`port.SIPTransport` 工厂、`port.Clock`、`port.AuditSink`；
不 import 任何 `internal/adapter/...`。

**理由**：spec 明确要求 `go list -deps ./internal/app/...` 不含 adapter。这是 `app/` 层第一个
真实用例，必须立好"依赖向内"的样板，否则 Change 5+ 会跟着跑偏。

### D7. 配置 `nodes:` 可选，缺省零节点

YAML 新增可选 `nodes:` 列表（每项 `id` / `kind` / `domain` / `addr`）。缺省时进程以零节点启动，
HTTP、日志、SQLite 行为与本 change 之前完全一致。条目非法时**加载失败并报错**（指出条目序号与字段），
不静默跳过。

**理由**：向后兼容是硬要求（现有部署无 `nodes:`）。静默跳过非法条目会导致"配了但没生效"这类
最难排查的问题，所以显式报错。

### D8. HTTP 错误码语义：404 未知 id，409 非法迁移

`GET /v1/nodes/{id}` 未知 id → 404；`/start` / `/stop` 触发非法迁移 → 409，响应体含当前状态。

**理由**：REST 语义上"资源不存在"与"状态冲突"是两类错误，混用会让调用方无法区分重试策略。

### D9. 生命周期与"注册"解耦：Start 只推进到 `Registering`

`Start()` 只负责资源（绑定 listener），状态 `Idle → Registering` 即返回；
`Registering → Registered → Online` 由**身份实现**（Change 5/6/7）推进。
本 change 提供显式的推进方法（如 `MarkRegistered` / `MarkOnline`）供测试与后续 change 调用，
自身**不自动推进**。

**理由**：Change 4 不实现注册协议，若让 `Start()` 直接跳到 `Online` 会伪造出"已注册"的假象，
Change 5 接入真实注册时还要回头改语义。提前解耦省一次返工。

**可观察结果**：本 change 启动后 `GET /v1/nodes` 显示状态为 `registering`，这是预期行为，
不是缺陷。

### D10. `servicectx.StorageKey` 改用端口类型，消除 platform → adapter 反向依赖

**承接 `enterprise-skeleton` 遗留 3**（该 change 归档后核查发现，未在其工件中记录）。

`internal/platform/servicectx/keys.go` 当前定义
`StorageKey = NewKey[*storage.Store]("storage")`，使 **platform 层 import 了
`internal/storage`（adapter 层）**，违反六边形依赖方向。已全量核查 `internal/platform`
下的非测试文件，这是**唯一一处** adapter 反向依赖；`clock.go` 与
`observability/audit/doc.go` 依赖 `internal/domain`，方向正确。

**决策**：把 `StorageKey` 的类型参数改为 `port.Storage`，`keys.go` 改为 import
`internal/domain/port`。

- `Provide` 的 build 仍返回 `*storage.Store`（`any`），`MustGet[port.Storage]` 的运行时接口
  断言成立，无需改动 `internal/storage`
- `cmd/gb28181-simulator/main.go` 当前自行定义的 `NewKey[*storage.Store]` 位于**装配层**，
  依赖具体类型本身合法；本次统一改用 `servicectx.StorageKey`，`MustGet` 返回接口
- 若后续装配确实需要 `*storage.Store` 的非接口方法，应在 `cmd/` 或 `app/` 层用类型断言取回，
  **不得**让 `servicectx` 重新 import adapter

**理由**：`servicectx` 是横切基础设施，一旦它认识具体 adapter，platform 就无法被其它二进制
独立复用，六边形边界形同虚设——而这正是 `enterprise-skeleton` 的核心目标。改动只涉及一个
类型参数与一行 import，代价极低。

**可观察结果**：`go list -deps ./internal/platform/servicectx` 输出中不再出现
`internal/storage`。

## Risks / Trade-offs

- **R1**：`port.SIPTransport` 签名变更会打断 `adapter/siptransport` 与其测试。
  → Mitigation：先改接口再改实现，编译驱动逐点修；改完跑全量 `-race`。
- **R2**：多节点配置了重复 `addr` 导致端口冲突。
  → Mitigation：注册前校验地址唯一，冲突时返回含节点 id 的明确错误，而不是让第二个节点
  静默绑到随机端口。
- **R3**：D9 的状态推进契约与 Change 5 的预期不一致。
  → Mitigation：本 design 明确写出契约；Change 5 提案时复核本节，如需调整在本 change 内改。
- **R4**：注册表为内存态，进程重启后节点状态丢失。
  → Mitigation：明确 Non-Goal；配置是声明式的，重启后按 `nodes:` 重建即可。
- **R5**：gosip 在部分传输（如已有 TCP 连接复用）下拿不到对端地址。
  → Mitigation：按 spec 返回显式错误而非空串；UDP 路径优先保证（§11.4 用 UDP）。
- **R6**：20 位编码的类型编码段到 `NodeKind` 的映射可能覆盖不全。
  → Mitigation：`ParseNodeID` 对无法映射的类型编码返回错误，不做猜测式默认。
- **R7**：`MustGet[port.Storage]`（D10）是**运行时**接口断言，若 provider 将来返回的不再是
  `port.Storage` 实现，panic 会推迟到 Build/Get 时而非编译期暴露。
  → Mitigation：`internal/storage` 已有 `var _ port.Storage = (*Store)(nil)` 编译期断言兜底；
  装配入口用一个最小启动用例覆盖 `MustGet[port.Storage]` 取回非 nil。

## Migration Plan

1. `internal/domain/model/`：新增 `node.go`（`NodeID` / `NodeKind` / `NodeProfile` / `Node`）与
   `node_status.go`（状态枚举 + 迁移表 + `ErrIllegalTransition`），配测试
2. `internal/domain/port/`：新增 `node.go`（`NodeRegistry` / `NodeLifecycle`）；改 `transport.go` 签名
3. `internal/adapter/siptransport/`：补齐目标地址与对端地址（D4），加编译期断言
4. `internal/adapter/nodereg/`：注册表实现（D3），加并发测试
5. `internal/app/`：新增 `node_service.go`（D6），验证不依赖 adapter
6. `internal/platform/config/`：新增 `nodes:` 结构（D7）
7. `internal/interface/http/`：新增 `/v1/nodes` 系列端点（D8）
8. `cmd/gb28181-simulator/main.go`：ServiceContext 装配 NodeService
9. `internal/platform/servicectx/keys.go`：`StorageKey` 类型参数改为 `port.Storage`（D10），
   消除 platform → adapter 反向依赖
10. `internal/sipprobe/`：新增 `--answer`（D5），恢复 `scripts/smoke-sip.sh` 原始断言
11. 全量 `-race` + 五平台 `CGO_ENABLED=0` 编译 + 报告

**回滚**：`git revert`。本 change 以新增为主，唯一 BREAKING 是 `port.SIPTransport` 签名，
回滚即恢复。

## Open Questions

- **Q1**：节点运行时状态是否需要落 SQLite？（倾向否——内存态够用，持久化留 Change 5+；
  若 Change 5 需要跨重启恢复会话再议）
- **Q2**：节点生命周期是否要发 `AuditSink` 事件？（倾向发——Change 13 抓包与审计能复用；
  但本 change spec 未要求，可作为后续增补）
- **Q3**：多节点是否各自持有一个 `Clock`？（倾向共享一个——`Fake` 时钟在测试里统一推进更简单）

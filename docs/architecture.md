# 架构

`gb28181-simulator` 在 `internal/` 下采用六边形（端口与适配器）布局，由 Change 3（`enterprise-skeleton`）引入。目标是让依赖方向清晰，使 Change 4+（`node-abstraction` 及之后）可以在层与层不相互坍塌的前提下持续叠加行为。

## 分层图

```
                    ┌─────────────────────────────────────┐
                    │  cmd/            组装入口           │
                    │  gb28181-simulator, sipprobe        │
                    └──────────────┬──────────────────────┘
                                   │ Provide / Build / MustGet
                    ┌──────────────▼──────────────────────┐
   入站             │  internal/interface/                │   出站
   HTTP / WS  ─────►│    http/    webui/                  │
                    └──────────────┬──────────────────────┘
                                   │
                    ┌──────────────▼──────────────────────┐
                    │  internal/app/                      │
                    │  用例编排（NodeService）            │
                    └──────────────┬──────────────────────┘
                                   │ 仅依赖端口
        ┌──────────────────────────▼──────────────────────────┐
        │  internal/domain/                                   │
        │    model/   不可变值对象                            │
        │    port/    契约（接口）                            │
        └──────────────▲──────────────────────────────────────┘
                       │ 实现
        ┌──────────────┴──────────────────────────────────────┐
        │  internal/adapter/                                  │
        │    sip/  sdp/  auth/  siptransport/  audit/         │
        │    nodereg/  节点注册表 + 生命周期                  │
        └─────────────────────────────────────────────────────┘

        ┌─────────────────────────────────────────────────────┐
        │  internal/platform/   横切基础设施                  │
        │    config/  clock/  servicectx/                     │
        │    observability/{logging,tracing,audit}            │
        └─────────────────────────────────────────────────────┘
```

## 依赖规则

| 来源层 | 可依赖 |
|--------|--------|
| `domain/` | 仅标准库 |
| `adapter/` | `domain/`、`platform/` |
| `interface/` | `domain/`、`platform/`、`adapter/` |
| `app/` | `domain/`、`platform/` |
| `platform/` | 标准库 + 基础设施库（slog、viper、OTel） |
| 任意层 | `platform/` |

任何层都不得向内依赖 `adapter/` 或 `interface/`（`domain/` 尤其如此）。该规则由机器检查：

```sh
# domain 不得有任何非标准库依赖
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...
```

## 端口契约

所有端口位于 `internal/domain/port/`，只操作 `internal/domain/model/` 中的值 —— 绝不操作 adapter 类型。

| 端口 | 文件 | 方法 | 适配器 |
|------|------|------|--------|
| `SIPTransport` | `transport.go` | `Send(ctx, Message, dst) error`、`Receive(ctx) (Message, string, error)`、`Close() error` | `internal/adapter/siptransport` |
| `SDPCodec` | `codec.go` | `Parse`、`Marshal` | `internal/adapter/sdp` |
| `Authenticator` | `auth.go` | `Verify` | `internal/adapter/auth` |
| `Challenger` | `auth.go` | `Challenge` | `internal/adapter/auth` |
| `AuditSink` | `audit.go` | `Emit` | `internal/adapter/audit` |
| `Clock` | `clock.go` | `Now()` | `internal/adapter/*`（经 `platform/clock`） |
| `Storage` | `storage.go` | `CRUD`、`List`、`io.Closer` | `internal/storage` |
| `NodeRegistry` | `node.go` | `Register`、`Unregister`、`Get`、`List`、`RecordRegistration` | `internal/adapter/nodereg` |
| `NodeLifecycle` | `node.go` | `Start`、`Stop`、`Status`、`Transport`、`Fail` | `internal/adapter/nodereg` |
| `NodeAdvancer` | `node.go` | `Advance(ctx, id, Status)` | `internal/adapter/nodereg` |
| `Authorizer` | `auth.go` | `Authorize(challenge, cred, method, uri)` | `internal/adapter/auth` |

`NodeAdvancer` 与 `NodeLifecycle` 刻意分离：`Start`/`Stop` 表达意图，而 `Advance` 是身份实现（如设备注册器，Change 5）在协议交互成功后把节点推进到 `registered` 或 `online` 的方式。`RecordRegistration` 记录注册结果但不改动状态，`Fail` 是成功启动的镜像：它使节点 fault 并释放监听口，保证没有“启动了一半”的残留。

每个 adapter 均带编译期断言，签名漂移在构建期即失败：

```go
var _ port.SIPTransport = (*PortAdapter)(nil)
```

`internal/adapter/sip` 是刻意留下的例外：它是报文构建/解析助手库，没有 `Transport` 类型，因此 `SIPTransport` 断言放在 `internal/adapter/siptransport`。

## ServiceContext

`internal/platform/servicectx` 是手写的类型安全服务注册表。之所以不用 `wire`/`fx`，是因为整个依赖图只有 5-10 行，代码生成器得不偿失。

```go
type Container struct{ ... }

func NewContainer() *Container
func (c *Container) Provide(k Key, f func() (any, error)) *Container
func (c *Container) Build() (io.Closer, error)
func MustGet[T any](c *Container, k Key[T]) T
func NewKey[T any](name string) Key[T]
```

语义：

- **声明顺序即构建顺序。** `Provide` 记录插入顺序；`Build` 按该顺序实例化。
- **逆序关闭。** `Build` 返回的 `io.Closer` 以逆序关闭所有 `io.Closer` provider，保证依赖方先于被依赖方关闭。
- **失败回滚。** 任一 provider 出错时，`Build` 会关闭已构造的全部对象并返回错误。
- **类型安全。** `MustGet[*config.Config](c, configKey)` 在类型不匹配时 panic 并给出期望与实际类型；缺失 key 时 panic 并给出 key 名。
- **重复 key** 覆盖，以最后一次注册为准。
- **无自动装配。** Provider 必须按依赖顺序声明并显式解析依赖。

### 用法

```go
configKey := servicectx.NewKey[*platformconfig.Config]("config")
loggerKey := servicectx.NewKey[*logging.Hub]("logger")

c := servicectx.NewContainer().
    Provide(configKey, func() (any, error) { return platformconfig.Load(path) }).
    Provide(loggerKey, func() (any, error) {
        // 依赖在 Build 之后才读取，因此 provider 保持自包含
        return logging.DefaultHub(), nil
    })

cancel, err := c.Build()
if err != nil { return err }
defer cancel.Close()

hub := servicectx.MustGet[*logging.Hub](c, loggerKey)
```

`cmd/gb28181-simulator` 与 `cmd/sipprobe` 均遵循该形态；两者都不含业务逻辑。

## 节点抽象（Change 4）

Change 4 在骨架之上加入第一批真实行为：节点 = 身份 + 生命周期，多个节点可共存于一个进程。

### 领域

`internal/domain/model` 持有价值对象；这里不知道 socket、HTTP 或配置。

- **`NodeID`** — 校验后的 20 位 GB/T 28181 编码：8 位中心 + 2 位行业 + 3 位类型 + 1 位网络 + 6 位序号。`ParseNodeID` 拒绝错误长度、任何非数字以及无法映射的类型码段，绝不猜默认值。`String()` 返回逐字编码。
- **`NodeKind`** — `device`、`platform-large`、`platform-small`，由类型码段推导，身份与 kind 永远一致。
- **`NodeProfile`** — 身份 + 信令地址 + 归属域 + 厂商。不可变；修改经 `With...`。
- **`Node`** — profile + `Status`。
- **`Status`** — `idle → registering → registered → online`，另有 `offline` 与 `fault`。转换放在显式 `map[Status]map[Status]bool` 表中；非法跳转返回哨兵 `model.ErrIllegalTransition` 且状态不变。`fault` 是可恢复的终态：其后只允许 `idle`（重置）或 `offline`（移除）。

### 适配器

`internal/adapter/nodereg` 基于 `sync.RWMutex` 保护的 `map[string]*entry` 实现全部三个节点端口：

- **每节点一个监听口。** `Lifecycle` 分别跟踪每个节点的传输，停止一个绝不影响另一个。
- **注册时地址唯一。** 第二个节点声称已占用的地址会被拒绝，错误中指出占用者，而不是静默绑定随机端口。
- **失败回滚。** `Start` 先推进状态（原子，防止并发双绑定），绑定失败则回滚：首次启动回 `idle`，重启回 `fault`。绝不留下启动了一半的节点。

### 应用层

`internal/app.NodeService` 是用例编排。它只依赖领域端口 —— 绝不 import `internal/adapter/...`：

```sh
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/app/...
# → internal/domain/model, internal/domain/port
```

`Start` 绑定监听口并到达 `registering`；携带 `Registration` 的 `device` 节点在 `Start` 返回前完成注册 —— 注册器调用 `MarkRegistered` 与 `MarkOnline`，平台拒绝时使节点 fault。没有注册配置的节点停在 `registering`，平台身份在各自阶段完成前同样停在这里。

保持在线是 keeper 的职责（`internal/app/keeper.go`）：每节点一个 goroutine，由注入的 `port.Ticker` 驱动，每个 `heartbeat_interval` 发送一次 MANSCDP `Keepalive`（SIP `MESSAGE`），并在授予 lifetime 的一半时续期（或到期前一分钟，取更早者）。连续 `heartbeat_max_failures` 次心跳无应答会使节点 fault；续期失败则退避（5s 翻倍至 60s）并保持在线，因为注册仍然有效。`Unregister` 先发送 `Expires: 0`，然后才停 goroutine 并释放监听口 —— 顺序颠倒会向已关闭的传输发心跳。keeper 由组装根在关机时关闭，其根 context 是进程级的，绝不使用请求级。

### 组装

`cmd/gb28181-simulator` 在启动时注册每个 `nodes:` 条目并把服务交给 HTTP 层。节点生命周期事件尚未写入 `port.AuditSink`：领域唯一的审计事件 `model.WireEvent` 描述线上传输，无法承载状态变化，因此该集成推迟到 Change 13。

## 平台侧接受（Change 6 上半）

`platform-large` 节点扮演 UAS。`app.Acceptor` 在该节点的传输上为每个平台节点运行一个 goroutine：`Receive` → 分发 REGISTER → 把应答 `Send` 回请求来源的对端。传输层无需改动 —— `model.NewResponse` 加 `PortAdapter.Send` 已覆盖响应发送。

```
device (UAC) ──REGISTER──▶ transport ──Receive(msg, peer)──▶ Acceptor
                                                              ├─ CredentialStore.Lookup(node, username)
                                                              ├─ Challenger.Challenge(realm)      → 401
                                                              ├─ Authenticator.Verify(msg, cred)  → 403 / 200
                                                              └─ DownstreamRegistry.Upsert/Remove
HTTP GET /v1/nodes/:id/devices ─▶ NodeService.Devices ─▶ Acceptor.Devices
```

- **凭据绝不落在节点上。** `model.PlatformServing`（realm、expires 窗口）挂在 `NodeProfile`，而账号在 `port.CredentialStore` 之后的 `adapter/credstore` 里按节点分区，密码永远不会出现在 HTTP、日志或错误体中。
- **失败模式决定应答。** `port.ErrMalformedCredentials`（头不可解析）可以重新挑战；按 §L.2，`port.ErrInvalidCredentials`（可解析但错误）不得再次挑战。哨兵放在 `port`，用例无需 import 产生它们的 adapter 即可分支。
- **lifetime 是策略而非算术。** `model.ExpiresPolicy` 把请求钳制到 `[min, max]`；`Expires: 0` 在其之前拦截，因为显式告别不等于请求默认值。
- **服务先于端口停止。** `NodeService.Stop` 先调 `Acceptor.Stop`（join goroutine 并清表），然后才让 lifecycle 释放监听口。

### MESSAGE 分发与清扫（Change 6 下半）

注册不是对话终点：保持在线的下游会发 MANSCDP `MESSAGE`，从不清理的平台会持续报告已沉默的设备。两者共用同一个服务循环。

```
device ──MESSAGE(Keepalive)──▶ Acceptor ──MANSCDPCodec.DecodeNotify──▶ refresh
                                                                        └─ DownstreamRegistry.Upsert(WithSeen)
device ──MESSAGE(Catalog)────▶ Acceptor ──MarshalCatalog──▶ 200 OK + body
sweeper goroutine ──Ticker──▶ sweep: 清除 now ≥ ExpiresAt 的行
```

- **读与写分离。** `port.MANSCDPCodec` 把下游发出的 notify 与平台回给它的 catalog 应答配对。它刻意不是 `port.KeepaliveCodec`（后者只负责渲染）：强迫每个设备节点携带它从不调用的解析器，是没人需要的依赖。
- **不可读的消息被忽略而非应答。** 其他厂商设备产生的 body 是数据；回应一个对端无法解读的错误 —— 或直接 panic —— 等于可被远程关机。循环 decode，失败时打 `debug` 日志并继续下一条。
- **心跳不是重新注册。** `refresh` 只移动 `last_seen_at`，不触碰被授予的 lifetime 与到期时间，设备无法靠更快的心跳延长注册。
- **目录按节点隔离。** 目录由该节点自己的在线表构建，共享进程的两个平台看不到彼此的下游。
- **清扫受策略约束。** 节拍是该节点授予的最短 lifetime 的一半，落在 `[1s, 30s]`，沉默设备会在半个 lifetime 内消失。sweeper 是服务节点的 goroutine，由 `Stop` / `Close` join，停止的平台不残留 goroutine。

## platform-small：一个节点，两半（Change 7 上半）

级联 `A → B → C` 需要 B 同时是两样东西：对下是平台，对上是下属。`platform-small` 就是这样的节点。它原样复用两个用例 —— 下半用 `Acceptor`，上半用 `Registrar` + `Keeper` —— 新东西都在 `NodeService` 里：两半建立的顺序、一个节点为两半只推进一次，以及共享监听口上到达报文的分拣。

```
Start ──▶ serve (Acceptor.Serve)  ──▶ advanceOnline ──▶ online
      └──▶ register (Registrar)   ──▶ advanceOnline ──▶ (已 online)
                                  └──▶ Keeper.Start
```

- **先服务，后注册。** 服务几乎从不失败；注册则每天都会遇到不可达或拒绝的对端。把易失败的一半放第二位，其回滚就只是“停掉服务”，与已经建立的 `Stop` 是同一条回收路径。先注册则回滚时要向上级注销，上级会短暂看到一个立即消失的下属。
- **一台状态机，只推进一次。** `serve` 与 `register` 都以上线收尾，但表里没有 `online → online` 边。`advanceOnline` 让推进幂等 —— 还缺的补上，已满足的跳过 —— 而不是为每种 kind 扩表。
- **任一半失败节点即 fault。** 半服务的平台不是值得汇报的状态，因此另一半被回收（服务 goroutine 停止、在线表清空），节点 fault。错误与日志都指明失败的一半。
- **两半皆可选且独立配置。** platform-small 上的 `platform:` 表示“对下服务”，`registration:` 表示“向上注册”；省略 `platform:` 仍以默认值服务，因为“是平台”本身就意味着服务。

### 一个监听口，两个读者

两半都运行在节点自己的监听口上，这是必须的：上下对端在同一个地址上看到的是同一个节点。但 socket 会把每个数据报交给先读的人，服务那一半一直在读 —— 它会抢走注册那一半正在等的 `401` 与 `200 OK`，判定不是请求后丢弃，注册永远完不成。

`splitTransport`（`internal/app/node_split_transport.go`）就是答案：一个读者持有 socket 并分拣读到的内容 —— 请求给服务半，响应给注册半 —— 两半都经同一 socket 发送。节点按 node id 各持一个 splitter，随节点启动创建，随节点停止或 fault 结束，因此不会有东西继续读一个已归还的监听口。不读的一半不会卡住另一半：没人要的消息被丢弃，而不是排在有人要的消息前面排队。

这只是对单个 socket 的分拣，不是 SIP 事务层。后续变更给 platform-small 带来需要从上游接收的请求（`INVITE`、`SUBSCRIBE`）时，分拣必须从按方向变为按事务匹配。

## 可观测性

### 日志

`log/slog` 经 `internal/platform/observability/logging`。级别：`trace`（SIP 线上字节）、`debug`、`info`、`warn`、`error`。输出写往配置的日志文件及仪表盘消费的 WebSocket 扇出 hub。敏感键（`password`、`authorization`）被脱敏。

### 追踪

`internal/platform/observability/tracing` 包装 `sdktrace.TracerProvider`：

- **stdout 导出器** 在 `tracing.enabled` 为 true 时始终注册，输出美化的 span JSON。
- **OTLP gRPC 导出器** 在 `tracing.otlp_endpoint` 非空时追加。
- **采样** 用 `sdktrace.TraceIDRatioBased(cfg.SampleRatio)`；默认 `0.01`，`sample_ratio: 1.0` 全采样（开发环境）。
- **`Close(ctx)`** 关闭所有 processor 与 provider 本身，导出 goroutine 不泄漏。

配置：

```yaml
tracing:
  enabled: true
  sample_ratio: 1.0
  otlp_endpoint: ""     # host:port；留空 = 仅 stdout
  service_name: ""      # 默认 "gb28181-simulator"
```

`otel.SetTracerProvider` 在启动时调用一次，领域代码可用 `otel.Tracer("...")` 获取 tracer 而无需 import provider 包。

## 已知限制

### Change 4 已解决：对端地址

`internal/adapter/siptransport` 现在会报告消息的来源对端。gosip 的 `Messages()` 通道不携带它，但其连接处理器在发布每条消息前用 `SetSource` 记录了来源，因此 `Receive` 返回 `(Message, addr, error)`，调用方可以把地址直接交回 `Send` 作答。缺失或缺端口的地址是显式错误 —— 绝不是空串 —— 调用方不会回错目的地。`sipprobe --answer` 依赖这一点回 200 OK，双进程 INVITE → 200 OK 握手已由 `scripts/smoke-sip.sh` 与 `TestAnswer_TwoProcessesExchange` 验证。

### 未决：`modelToGosip` 丢弃头部

`internal/adapter/siptransport` 经 `internal/adapter/sip` 的 builder 为 gosip 重建 `model.Message`，目前只保留方法/URI（或状态）、body 与 content type —— **调用方传入的头部被丢弃**。请求仍能工作，因为 builder 自己生成 `Via`、`From`、`To`、`Call-ID` 与 `CSeq`，但手工构建的响应会丢头部并被线上拒绝。头部透传留给后续变更；`sipprobe --answer` 不受影响，因为它直接构建 gosip 消息。

### 未决：异步端口释放

gosip 的 `layer.Cancel()` 只关闭取消通道；监听 socket 由它自己的 goroutine 稍后释放。因此 `Transport.Close()` 会（最多 500ms）等待端口重新可绑定，否则在同一地址上停止后立即重启会以 "address already in use" 失败。超时不致命 —— 调用方只会看到它本来就会看到的 bind 错误。

# Design: platform-large 注册受理与在线设备表

## Context

路线图 #5 完成后，device 身份能在启动后完成 REGISTER、发心跳、重注册与注销，但它的对端一直是测试
用的 `internal/adapter/siptest/uas.go`。`kind: platform-large` 的节点（type code `200`）在配置里
已经合法，启动后却只绑定 listener 停在 `registering`：不回 401，也不认识任何下级。

现状盘点（决定本 change 有多少是"新建"而非"复用"）：

| 能力 | 现状 | 本 change 的动作 |
|---|---|---|
| 服务端 Digest 校验 | **有**：`port.Authenticator.Verify(req, cred)` + `adapter/auth.NewResponder` | 复用 |
| 生成 WWW-Authenticate 挑战 | **有**：`port.Challenger.Challenge(realm)` + `adapter/auth.NewChallenger` | 复用（自己拼头串） |
| 发送 SIP 响应 | **有**：`model.NewResponse` + `PortAdapter.Send`（`IsResponse` → `sipbuild.BuildResponse`） | 复用 |
| 节点 transport 取用 | **有**：`NodeLifecycle.Transport(id)` | 复用 |
| 每节点常驻 goroutine + 收口 | **有**（`Keeper` 的 session 集合 + `io.Closer` + ctx） | 照搬其写法 |
| 在线设备表 | **无** | 新建 `port.DownstreamRegistry` + `adapter/devicereg` |
| 下级凭据存储 | **无** | 新建 `port.CredentialStore` + `adapter/credstore`（配置 accounts） |
| 有效期协商 | **无**（device 侧只会请求） | 新建 `model.ExpiresPolicy` |
| UAS 受理用例 | **无** | 新建 `app.Acceptor` |
| 设备查询 HTTP | **无**（`controlNode` 是动作型，不适用） | 新建两个只读 handler |

硬约束（沿用既有决定）：**app 层不得 import adapter**（`go list -deps ./internal/app/...` 有测试
断言），所以凭据与在线表都必须经 domain 端口注入。

## Goals / Non-goals

Goals：受理下级 REGISTER（401 → 校验 → 200 OK + Expires 协商）、按 accounts 鉴权、维护按节点分区
的在线设备表、经 HTTP 暴露、生命周期随节点启停、配置声明式可校验。

Non-goals：MESSAGE 心跳接收与超时踢线（下一个 change）；MANSCDP 目录查询/响应；级联转发
（#9）；platform-small 身份（#7）；nonce 重放防护与 nonce TTL；在线表持久化。

## Decisions

### D1. 受理循环放 app 层 `internal/app/acceptor.go`，只依赖端口

```
session goroutine:
  for {
      msg, peer, err := tr.Receive(ctx)          // ctx 可取消
      if err != nil { return }                    // 关闭/取消即退出
      if msg.Method() != "REGISTER" { log debug; continue }
      401 / 403 / 200  = a.handle(ctx, msg, peer, sess)
      tr.Send(ctx, resp, peer)
  }
```

`Receive` 返回的 `peer` 直接作为 `Send` 的目的地，无需改传输层。

- **备选**：在 adapter 里做一个"UAS server"——协议判断会落到基础设施层，且无法复用
  `NodeLifecycle.Transport(id)`；不采用。

### D2. 401 挑战无状态：每次新 nonce，不做 nonce 存储

`Challenger.Challenge(realm)` 每次生成 16 字节随机 nonce。模拟器场景下重放防护收益极低，而
nonce 存储要引入 TTL、并发清理与"nonce 过期如何提示"的一整套语义。

- **备选**：带 nonce store（nonce → issuedAt，TTL 60s，一次性消费）——更严谨，但本 change 的
  目标是"能被真实下级注册成功"，不是抗重放；不采用，风险在 Risks 中登记。
- 挑战头串由 app 按 `model.Challenge` 的字段拼装（`Digest realm=..., nonce=..., qop="auth",
  algorithm=MD5`），因为 `port.Challenger` 只给结构化值。

### D3. 鉴权分支映射（GB/T 28181 §L.2）

| 情况 | 响应 |
|---|---|
| 无 `Authorization` | **401** + 新挑战 |
| `Authorization` 解析失败（`ErrMalformed` / `ErrMalformedAuthorization`） | **401** + 新挑战 |
| username 不在 accounts | **403**，不再挑战 |
| response 不符（`ErrInvalidResponse`） | **403**，不再挑战 |
| 校验通过 | **200 OK** + 协商后的 `Expires` |

- **备选**：所有失败统一 401——会导致真实下级在密码错误时无限重试，掩盖真实故障；不采用。
- 403 时不写在线表、也不删既有记录：本 change 不因单次失败踢线（踢线属于心跳 change）。

### D4. 有效期协商是 domain 值对象，不是 app 里的 if 链

```go
// internal/domain/model/expires_policy.go
type ExpiresPolicy struct{ min, def, max uint32 }
func NewExpiresPolicy(min, def, max uint32) (ExpiresPolicy, error)   // 0 值取 60/3600/86400；校验 0<min<=def<=max
func (p ExpiresPolicy) Negotiate(requested uint32) uint32            // 0 → 0（注销）；否则钳制到 [min,max]；0 请求值 → def
```

放 domain 便于单测，也让"平台授予什么"这个规则与协议层解耦。

### D5. `Expires: 0` = 注销：200 OK + 出表

与 device 侧 `Registrar.Unregister` 的语义对齐。出表失败（设备不存在）不报错——幂等。

### D6. 本 change 不消费 MESSAGE

非 REGISTER 报文记 debug 后丢弃。device 侧心跳周期默认 60s，e2e 窗口远小于它，不会触发 device
侧的连续失败回落；这一点在 e2e 里不成为干扰。

### D7. 在线表按节点分区，凭据存储按节点分区

```go
// internal/domain/port/device.go
type DownstreamRegistry interface {
    Upsert(ctx context.Context, nodeID model.NodeID, dev model.DownstreamDevice) error
    Remove(ctx context.Context, nodeID model.NodeID, deviceID string) error
    Lookup(ctx context.Context, nodeID model.NodeID, deviceID string) (model.DownstreamDevice, bool)
    List(ctx context.Context, nodeID model.NodeID) []model.DownstreamDevice   // 按 deviceID 升序
    Clear(ctx context.Context, nodeID model.NodeID) error
}

// internal/domain/port/credentials.go
type CredentialStore interface {
    Lookup(nodeID model.NodeID, username string) (model.Credentials, bool)
}
```

适配器：`internal/adapter/devicereg`（`sync.RWMutex` + 两级 map）、`internal/adapter/credstore`
（`sync.RWMutex` + 两级 map，密码比较走 `Credentials.PasswordEquals`）。

- **备选**：把 accounts 放进 `NodeProfile`——密码会随 Node 走到 HTTP 序列化路径，泄漏风险高；
  不采用（凭据单独存，Node 只带非密的 realm 与策略）。

### D8. 平台侧配置落在 `NodeProfile`，不进 Node 的 JSON

新增值对象：

```go
// internal/domain/model/platform_serving.go
type PlatformServing struct{ realm string; policy ExpiresPolicy }
func NewPlatformServing(realm string, p ExpiresPolicy) (PlatformServing, error)  // realm 空 → 调用方给 domain
```

`NodeProfile.WithPlatformServing(ps)`。HTTP 的 Node 序列化不新增字段，因此不涉及脱敏。

### D9. 配置：`nodes[]` 新增可选 `platform:` 段

```yaml
- id: "34020000002000000001"
  kind: platform-large
  domain: "3402000000"
  addr: "127.0.0.1:15061"
  platform:
    realm: "3402000000"        # 可选；默认取节点的 domain
    accounts:                  # 可选；未声明则所有 username 都被拒
      - username: "34020000011310000001"
        password: "change-me"
    min_expires: 60            # 可选；默认 60
    default_expires: 3600      # 可选；默认 3600
    max_expires: 86400         # 可选；默认 86400
```

校验沿用 `ValidateNodes` 的既有范式，错误形如 `config: nodes[1].platform.accounts[0]: ...`；
密码不进错误、不进日志。

### D10. 生命周期：Start 即服务，Stop 先停受理

- `NodeService.Start`：`lifecycle.Start` → 对 platform-large 调 `acceptor.Serve(id, tr, ...)` →
  `MarkRegistered` → `MarkOnline`（"平台在服务"）。状态机没有 `registering → online` 的迁移，
  所以平台与 device 一样先 `registered`（已绑定、就绪）再 `online`（在服务）。未装配 Acceptor
  时（其他身份的测试路径）保持 `registering` 不变。受理 goroutine 起不来则按既有 `fault`
  路径回落并释放端口。
- `NodeService.Stop` / `fault`：`acceptor.Stop(id)` → `devices.Clear` → `lifecycle.Stop`。
- `Acceptor` 实现 `io.Closer`，在组合根 `defer acceptor.Close()`（与 `Keeper` 同构）。

顺序上必须先停 goroutine：`lifecycle.release` 会 `Close()` transport，之后 `Receive` 只会报错。

### D11. HTTP 查询是两个只读 handler，不复用 `controlNode`

`controlNode` 处理"动作 + 409/502"，而设备查询是只读：只有 200 与 404。
新增 `GET /v1/nodes/:id/devices` 与 `GET /v1/nodes/:id/devices/:deviceID`，`NodeView` 增加
`Devices(ctx, id)` / `Device(ctx, id, deviceID)`，未知节点与未知设备都返回 404 + JSON 错误体。

### D12. 测试策略

- 单元：`ExpiresPolicy.Negotiate` 的钳制表、`DownstreamDevice` 校验、`devicereg` / `credstore`
  的并发与分区、`Acceptor` 用 scripted transport 覆盖 401/403/200/注销/非 REGISTER。
- 端到端：一个 device 节点 + 一个 platform-large 节点（真实 UDP transport）：device `/start`
  完成 401 挑战 → `GET /v1/nodes/{platform}/devices` 出现该设备；`/stop` 平台后表清空。
- e2e 断言用 `waitFor`（轮询）避免 sleep 竞态；`go test -race` 跑全套。

## Risks / Trade-offs

- **无 nonce 重放防护（D2）**：同一 nonce + 同一 Authorization 可被重放。模拟器场景可接受；
  后续若要收紧，改 `Challenger` 与 `Acceptor` 之间的 nonce store 即可，接口不变。
- **受理 goroutine 泄漏**：与 `Keeper` 同样三重收口（ctx 取消、`Stop(id)`、`Close()`）；`-race`
  跑全套并在 `Stop` 上做幂等。
- **403 不踢线**：一个已在线设备若后续注册密码错误，表里旧记录仍在；这是有意的（注册失败不
  等于心跳失联），踢线逻辑属于心跳 change。
- **只做 Keepalive 之外的 MANSCDP**：本 change 不解析任何 XML，目录查询留给下一个 change，
  因此 `manscdp` adapter 不变。
- **device 侧心跳在本 change 不被应答**：非 REGISTER 一律丢弃，device 若长时间在线且心跳周期
  很短（如 1s）会在 3 次后回落 fault——这是预期行为，文档中说明。

## Migration Plan

纯增量：`platform:` 段可选、HTTP 只加路由、`NodeProfile` 新字段零值取默认。未声明 platform 段或
非 platform-large 的节点行为完全不变；device 侧与已归档的两个 change 一行不改。

## Open Questions

- 无（范围拆分与鉴权策略已与用户确认：本次只做受理与在线表；凭据来自配置 accounts）。

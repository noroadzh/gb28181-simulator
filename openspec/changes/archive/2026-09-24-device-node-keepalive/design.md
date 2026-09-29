# Design: device 保活与注销

## Context

`device-node-registration` 之后，device 节点能完成 REGISTER 并进入 `online`，但 `NodeService.Start`
全程同步、返回后没有任何常驻行为：没有心跳、没有重注册、停止时也不通知平台。真实平台会在心跳超时或注册
过期后把设备踢下线。

现状盘点（决定本 change 有多少是"新建"而非"复用"）：

| 能力 | 现状 | 本 change 的动作 |
|---|---|---|
| MANSCDP XML 编解码 | **无**（全仓库零 `encoding/xml`） | 新建 `internal/adapter/manscdp` |
| 周期任务 / goroutine 管理抽象 | **无**（`internal` 下唯一 ticker 是日志 `AsyncFileWriter`） | 新建 `port.Ticker` |
| `port.Clock` | 有，只有 `Now()` | 复用；新增 ticker 实现 |
| 注册事务编排（Send/Receive + Call-ID/peer 过滤 + 分期错误） | 有（`Registrar.Register`） | 复用并抽出可共享的过滤/分期 |
| 失败回落与端口释放 | 有（`lifecycle.Fail` / `Stop` / `release`） | 复用 |
| HTTP 动作骨架 | 有（`controlNode` 的 404/409/502 映射） | 复用，加一行路由 |
| 测试 UAS | 有，但 `if msg.Method() != "REGISTER" { continue }` | 扩展 MESSAGE 分支 |

硬约束（沿用既有决定）：**app 层不得 import adapter**（`go list -deps ./internal/app/...` 有测试断言），
因此 MANSCDP 编解码必须经 domain 端口注入。

## Goals / Non-goals

Goals：心跳保活、过期前重注册、优雅注销、后台任务随节点生命周期启停、参数可配置可校验。

Non-goals：platform-large 的在线设备表与踢线（#6）；其余 device 行为（目录/报警/点播）；
完整 MANSCDP+ 命令集（本 change 只有 `Keepalive`）；一设备多主（#9）；后台任务持久化。

## Decisions

### D1. 心跳体由新的 `manscdp` adapter 生成，经 `port.KeepaliveCodec` 注入 app

`internal/adapter/manscdp` 用标准库 `encoding/xml` 生成 MANSCDP `Keepalive` 通知；domain 侧新增：

```go
// internal/domain/model/keepalive.go
type Keepalive struct { deviceID string; sn uint32; status string }  // + NewKeepalive / 访问器

// internal/domain/port/keepalive.go
type KeepaliveCodec interface {
    MarshalKeepalive(k model.Keepalive) (string, error)
}
```

app 只拿到 `string` 的 body，自己拼 `model.NewRequest("MESSAGE", uri, hdrs, body)` 并加
`Content-Type: Application/MANSCDP+XML`（`transport_port.go:126` 会把该头转成 gosip 的
`WithContentType`，无需改传输层）。

- **备选**：app 层直接 `fmt.Sprintf` 拼 XML —— 省一个端口，但把协议细节放进用例层，且无法做字节级 golden 校验；不采用。
- **备选**：把 MANSCDP 编解码塞进既有 `port.SDPCodec` —— 语义无关，不采用。
- **扩展性**：后续 MANSCDP 命令（目录/报警/控制）在同一 adapter 内增量添加，端口按命令各开一个方法（避免"万能 codec"）。

### D2. 新增 `port.Ticker`，心跳与重注册都靠它驱动

```go
// internal/domain/port/clock.go
type Ticker interface { C() <-chan time.Time; Stop() }
type TickerFactory func(d time.Duration) Ticker
```

- 真实实现放 `internal/platform/clock`（包一层 `time.NewTicker`）；
- 测试用 app 包内的 `scriptedTicker`（`tick()` 手动投递一次，不发真实信号），配合 `clock.Fake`
  的 `Set` / `Advance` 让"周期 + 到期时刻"完全确定性，测试里零 sleep。

- **备选**：直接用 `time.NewTicker` + 真实短周期（如 50ms）测 —— 测试会变得慢且偶发失败；不采用。
- **备选**：只加 `Sleep` 端口 —— 无法表达"被停止时中断等待"，取消语义会退化；不采用。

### D3. 一个节点一个 goroutine：单 ticker + 每拍检查重注册到期

`internal/app/keeper.go` 的 `Keeper` 为每个 `online` 节点起一个 goroutine：

```
for {
  select {
  case <-ctx.Done(): return
  case <-ticker.C():
      sendKeepalive()                       // 一次 MESSAGE 事务，受 heartbeat_timeout 约束
      if clock.Now().After(nextRenewAt) { reRegister() }
  }
}
```

- 心跳周期即 ticker 周期；重注册不做第二个定时器，而是在每拍用 `clock.Now()` 与 `nextRenewAt`
  比较（判定粒度 = 心跳周期）。到期时刻 = `min(granted/2, granted-60s)`，取 > 0 者。
- **备选**：再开一个 renewal ticker —— 两个定时器要各自 Reset/停止，fake 实现复杂度翻倍，
  而重注册提前量本身有分钟级余量；不采用。
- 重注册复用 `Registrar.Register`（新 `Call-ID`、同样走 401）；成功后 `registry.RecordRegistration`
  更新结果并重算 `nextRenewAt`，**不改状态**（保持 `online`）。
- 失败：`nextRenewAt = now + backoff`，backoff 从 5s 起翻倍、上限 60s；成功归零。

### D4. 心跳失败计数达到阈值才回落 `fault`

每次心跳事务：发送 → 在 `heartbeat_timeout` 内等匹配响应（`Call-ID` 相等且 `peer == reg.Server()`，
与 `Register` 同一过滤范式）→ 2xx 成功（计数归零）/ 超时或非 2xx 记一次失败。连续 `max_failures`
次失败 → 停止该节点后台任务 → `lifecycle.Fail`（`online → fault` + 释放端口）→ 退出 goroutine。
单次失败只打一条 warn 日志（含 `node_id` 与连续失败次数）。

- **备选**：首次失败即 fault —— 抖动即掉线，不符合"模拟器要能长时间稳定在线"的目标；不采用。

### D5. 注销：先发 `Expires: 0`，成功后才停后台任务并推进 offline

`Registrar.Unregister(ctx, tr, node, reg)` 与 `Register` 同构（同样应答 401、同样分期错误），
只是 `Expires: 0`。

`NodeService.Unregister` 顺序：

1. 校验节点存在且状态允许（只有 `registered` / `online` 可注销，其余 → `ErrIllegalTransition`）；
2. 发注销事务；失败 → 返回分期错误，**节点保持 `online`、心跳继续**（注册仍有效）；
3. 成功 → 先 `keeper.Stop(id)` 停后台 goroutine，再 `lifecycle.Stop`（`→ offline` + 释放端口）。

- **备选**：把注销塞进 `/stop`（在线节点先注销再停） —— 让 `/stop` 从一个纯本地动作变成依赖网络
  事务，停止会因超时变慢，且无法表达"注销失败但本地仍需停止"；不采用（用户已选定独立动作）。
- 顺序上必须先停 goroutine：`lifecycle.release` 会 `Close()` transport，之后任何发送都会失败。

### D6. 后台任务的启停点全部在 `NodeService`，`Keeper` 不感知状态机

- `Start` 注册成功后 → `keeper.Start(ctx, id, tr, node, reg)`；
- `Stop` / `Unregister` / `fault`（`NodeService.fault`）→ `keeper.Stop(id)`；
- `Keeper` 自身实现 `io.Closer`（`Close()` 停全部），在组合根注册进 `servicectx` 的 `Cancel`
  （`container.go:218` 逆序关闭），保证进程关闭时无残留 goroutine。

`Keeper` 只依赖 `port.NodeRegistry` / `port.NodeLifecycle` / `Registrar` / `KeepaliveCodec` /
`Clock` / `TickerFactory`，不 import adapter。

### D7. 心跳参数挂在 `Registration` 值对象上，构造时补默认值

`model.RegistrationParams` 增加 `HeartbeatInterval`、`HeartbeatTimeout`、`HeartbeatMaxFailures`
—— 都是零值即取默认（60s / 5s / 3），因此既有的 `NewRegistration` 调用点（配置装配、测试）不受影响。
新增访问器 `HeartbeatInterval()` / `HeartbeatTimeout()` / `MaxHeartbeatFailures()`，校验：
interval > 0、timeout > 0、timeout < interval、maxFailures ≥ 1。

- **备选**：单独 `model.Heartbeat` 值对象 + `NodeProfile.WithHeartbeat` —— 更"纯"，但心跳只在注册存在时
  才有意义，且要在 profile、配置、构造链上再铺一层；不采用。

### D8. 配置：`registration` 段增加三个可选键

```yaml
registration:
  server: "127.0.0.1:15061"
  password: "..."
  heartbeat_interval: 60s      # 可选，默认 60s
  heartbeat_timeout: 5s        # 可选，默认 5s
  heartbeat_max_failures: 3    # 可选，默认 3
```

`config.go` 的既有校验入口（`n.Registration != nil` 分支，第 72 行附近）内追加字段校验，错误同样
带条目序号与字段名；`configs/config.example.yaml` 同步注释。

### D9. HTTP：新增一条路由，复用 `controlNode`

`server.go:75` 加 `e.POST("/v1/nodes/:id/unregister", s.handleNodeUnregister)`；`NodeView` 加
`Unregister(ctx, id) error`；handler 一行复用 `controlNode(c, "unregister", NodeView.Unregister)`，
于是 404 / 409（`ErrIllegalTransition`）/ 502（`port.StagedFailure` + `Stage`）语义自动继承，
不需要新增错误分支。

### D10. 测试 UAS 扩展 MESSAGE 分支

`internal/adapter/siptest/uas.go` 的 `Serve` 目前 `if msg.Method() != "REGISTER" { continue }`。
改为：`REGISTER` 走既有 `answer`；`MESSAGE` 默认回 200 OK（并照常记入 `Received()`）；
新增选项 `WithSilentMessages()` 让它**不**回应，用于测"连续失败 → fault"。
另加 `UAS.Keepalives() []model.Message`（按 method 过滤）方便断言心跳条数与 SN。

### D11. 测试策略

- 单元：`Registration` 心跳参数与校验、`manscdp` 的 XML golden test、`Keeper` 用 scripted
  transport + scripted ticker + fake clock 覆盖"成功/单次失败/达阈值 fault/重注册触发与退避"、
  `Unregister` 的各分期。
- 端到端：两个 device 节点 + 两个 `siptest.UAS`：注册 → 收到心跳 → 200 OK；再用
  `WithSilentMessages()` 验证 3 次失败后 `fault` 且端口释放；注销后 UAS 侧不再收到心跳。
- 断言"停止后不再发送"用 `UAS.Received()` 在等待窗口后的长度不变来判定，避免 sleep 竞态。

## Risks / Trade-offs

- **goroutine 泄漏**：心跳 goroutine 必须能被 `ctx` 取消、`Stop` 与 `Close` 双通道终止；
  用 `-race` 跑全套测试，并在 `Keeper.Stop` 上做幂等。
- **后台任务与状态机竞态**：节点被并发停止时 goroutine 可能正要发送；以"发送前检查 ctx/停止标记 +
  发送失败即计入失败"处理，且生命周期释放前已停 goroutine（D5/D6 的顺序保证）。
- **重注册判定粒度 = 心跳周期**：若心跳周期被配得比有效期半程还长，重注册会偏晚。
  校验里不禁止该组合，但在配置注释中提示；真出问题可由 `heartbeat_max_failures` 兜底暴露。
- **MANSCDP 只做 `Keepalive`**：后续命令加入时端口会增方法，属预期演进，不是阻塞。

## Migration Plan

纯增量：心跳参数可选、路由新增、`Registration` 新字段零值取默认。未配置注册的节点与既有调用点行为不变；
`NewRegistration` 的入参是结构体，新增字段不破坏调用方。

## Open Questions

- 无（心跳阈值、重注册时机、注销入口三项已与用户确认）。

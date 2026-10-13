# Design: device-catalog-answer

## Context

参考 `proposal.md` 的动机与 `specs/device-catalog-answer/spec.md` 的契约。本节说明实现层面的关键技术决策。

**当前架构事实**：

device 节点生命周期（`node_service.go:register`）：

```
Registrar.Register()
  → tr.Send(first REGISTER)
  → tr.Receive()  ← 阻塞！只消费 self Call-ID 的 Response
  → tr.Send(retry with auth)
  → tr.Receive()  ← 阻塞！
  → RecordRegistration
  → keeper.Start()  ← 只发心跳，不收 SIP
```

`Registrar.transaction` 是**阻塞同步**的——整个函数退出后，device 进程才继续执行后续逻辑。**没有任何 goroutine 在收 SIP 消息**，所以 platform 发来的 catalog query MESSAGE 直接被 socket 缓冲区丢弃。

**现有平台侧设施已完整**（`cascade-media-source-propagation` change）：

- `catalogPoller` 每 30s 发 `MESSAGE/Query CmdType=Catalog`（`catalog_poller.go:sendQuery`）
- `acceptor.run` 读 socket → `msg.IsRequest()` 检测 → `handleMessage` → `answerCatalog`
- `answerCatalog` 对 device 节点**天然可用**：`a.devices.List()` 为空，`a.registry.Get(p.id).Profile().Channels()` 返回 device 自己的 channels，`buildCatalogMediaByDeviceID` 带上 device 自己的媒体源

**待解决问题**：device 节点需要一个 SIP 读循环来接收 catalog query，并路由到现有的 `handleMessage` → `answerCatalog`。

## Goals / Non-Goals

**Goals:**

- device 注册成功后启动一个 SIP 读循环，接受来自 platform 的 MESSAGE 请求
- 复用现有的 `handleMessage` → `answerCatalog` 处理 catalog query，返回携带 `<Extension><MediaSource>` 的完整目录响应
- device 不处理 INVITE/BYE/OPTIONS/SUBSCRIBE/INFO，返回 405 Method Not Allowed（而非静默丢弃）
- 保持 device 节点生命周期完整：注册 → 在线 → 心跳保活 → catalog query 响应 → 可被 INVITE 点播
- 单元测试覆盖 device catalog query 响应路径（e2e 真实 socket）

**Non-Goals:**

- 不改变 device 注册流程（REGISTER 认证逻辑不变）
- 不改变 platform 侧的 CatalogPoller 发送逻辑
- 不改变 `answerCatalog` 的聚合逻辑（device 无下游）
- 不实现 device 主动 NOTIFY 推送 catalog 的机制
- 不实现 device 对 INVITE 的媒体处理（仅返回 200 OK）

## Decisions

### 决策 1：复用 `Acceptor` + 新增 `ServeDevice()` 方法

**理由**：

- `Acceptor.run` 的 readLoop 完整支持 SIP 消息分发，且 `handleMessage` → `answerCatalog` 逻辑对 device 节点**完全适用**
- `Acceptor` 已是项目基础设施，共享同一套 fault gate、sweep、catalog poller 等机制
- 新增 `ServeDevice()` 而非另建新类型，避免重复代码和状态管理复杂度

**实现策略**：

```go
// Acceptor 新增方法（acceptor.go）
func (a *Acceptor) ServeDevice(id model.NodeID, tr port.SIPTransport, realm string) error {
    // 复用 Serve() 的骨架，但 device 节点特殊处理：
    // - 使用固定的 policy（不影响功能，仅满足 Serve 校验）
    // - 不启动 catalogPoller（device 不发 Query）
    // - 不调用 devices.Clear()（device 无下游设备表）
}
```

**备选**：

- A. 新建独立 `DeviceSession` 类型——代码量翻倍，状态管理复杂，且丢失 `answerCatalog` 复用
- B. 在 `Registrar.transaction` 完成后用 channel 转发收到的消息——侵入 `Registrar`，改动面大

**选定**：A 改的变体——复用 `Acceptor`，`ServeDevice()` 专用路径。

### 决策 2：device 节点专用 `platform` 标记

**理由**：

- `Acceptor.run` 的 readLoop 分发所有 SIP 方法到对应 handler（`handleInvite` / `handleOptions` 等），device 节点需要 gate 掉非 MESSAGE 的方法
- 通过 `platform.isDevice` bool 标记区分 device 与 platform-large 节点

**实现策略**：

```go
// platform struct 新增字段（acceptor.go）
type platform struct {
    // ... 现有字段 ...
    isDevice bool
}

// ServeDevice 中设置
p := &platform{
    id:        id,
    tr:        tr,
    realm:     realm,
    policy:    model.NewExpiresPolicy(3600, 3600), // 固定值，满足校验
    isDevice:  true,
    // catalogPoller: nil（device 不需要）
}
```

**备选**：

- A. 新建 `devicePlatform` 子结构继承 `platform`——侵入大，引入接口复杂性
- B. 用方法分发 map（`method → handler`）代替 switch-case——改动面大

**选定**：简单 bool 标记 + `if p.isDevice { gate }` 分支。

### 决策 3：device 节点 only 应答 MESSAGE，其他方法 405

**理由**：

- device 作为 UAS（UAC 是 platform），platform 可能发 OPTIONS/INFO 等探测，**静默丢弃**会让平台侧认为 device 无响应
- 405 告知平台该方法不支持，平台侧可正常处理（不会触发重试风暴）
- catalog query 是 MESSAGE 的唯一**业务关键**路径，其他 MESSAGE 子类型（心跳、DeviceInfo 等）走现有 `handleMessage` 逻辑

**实现位置**：`acceptor.run` 的 method switch，在 switch 入口处加 device 节点 gate：

```go
case "MESSAGE":
    // platform-large 和 device 共用 handleMessage
    resp, answered = a.handleMessage(ctx, p, msg, peer)
    if !answered { continue }
default:
    // device 节点对非 MESSAGE 方法返回 405
    if p.isDevice {
        resp, _ = buildResponse(405, "Method Not Allowed", msg, []model.Header{allowHeader()}, "")
    } else {
        resp, _ = buildResponse(405, ...)
    }
}
```

### 决策 4：catalogPoller 不在 device 侧启动

**理由**：

- `catalogPoller` 由 `Serve()` 在 `a.registry != nil && a.catalogPollEvery > 0` 时创建（`acceptor.go:431`）
- `ServeDevice()` 调用 `Serve()` 复用同一套逻辑，但 device 节点的 `catalogPollEvery` 始终为 0（`catalogPollEvery` 通过 `WithCatalogPollInterval` 全局设置，device 不调用此方法）
- 即便 `catalogPollEvery > 0`，device 节点的 `a.devices.List()` 为空，`pollRound` 第一个 if 即返回，代价可控

**备选**：

- A. 在 `ServeDevice()` 中显式禁用 catalogPoller——更清晰，但需要改动 `Serve()` 逻辑
- B. device 节点不调用 `Serve()`，自己写独立 readLoop——不共享 `answerCatalog`

**选定**：device 不设置 `catalogPollEvery`，catalogPoller 自动不启动。

### 决策 5：NodeService 侧注册成功后调用 `ServeDevice()`

**理由**：

- device 节点注册成功后（`register()` 返回 nil），`NodeService` 需要启动 SIP 接收 goroutine
- `ServeDevice()` 接收 `tr`（已绑定的 transport）和 `realm` 参数

**实现位置**：`node_service.go` 的 device 注册路径（`nodeKind == device` 分支）：

```go
// 位于 register() 成功返回后，或 device 节点上线时的启动逻辑中
if err := s.acceptor.ServeDevice(id, tr, realmForDevice(id)); err != nil {
    return s.fault(ctx, id, fmt.Errorf("app: serve device %s: %w", id, err))
}
```

其中 `realmForDevice` 可用 `node.Profile().Domain()` 或直接用 `id.String()`。

### 决策 6：device 节点 Stop 行为

**理由**：

- `Acceptor.Stop()` 调用 `devices.Clear()`，但 device 节点没有下游设备表，这是无害的误操作
- `ServeDevice()` 创建的 `platform` 加入 `a.serving` map，`Stop()` 正确取消 goroutine

**实现**：device 节点调用 `Acceptor.Stop()` 无副作用（`devices.Clear()` 对空表无影响）。

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| device 与 platform-large 共用 Acceptor 实例，并发调用 `Serve()` / `ServeDevice()` 对同一 Acceptor 是否安全 | `Acceptor.run` 每个节点有独立 goroutine，`a.mu` 保护 `a.serving` map，`Stop()` 正确隔离 |
| device 节点的 `handleMessage` 中 catalog query 路径依赖 `a.registry` 非空 | device 节点创建时必须 attach NodeRegistry（`NewNodeService` 已有 `registry port.NodeRegistry`），否则 `ServeDevice()` 前置检查报错 |
| device 节点的 `answerCatalog` 依赖 `a.devices.List()` 返回空（device 无下游） | 正确——device 无下游设备表，`a.devices.List()` 返回 `[]model.DownstreamDevice{}`，不触发 downstream channel 展开 |

## Migration Plan

无需数据迁移。部署步骤：

1. 合并代码并通过单元测试
2. 在测试环境（10.96.1.125）重建 device + platform-large 双容器
3. 验证：`GET /v1/nodes/34020000002000000001/channels/34020000130000000001/media` 返回 `has_media: true`（约 30s 内，catalogPoller 周期）

回滚：`git revert` 即可，device catalog 响应为本次新增，移除后回到"platform 发 query，device 无响应"状态。

## Open Questions

- **Q1**：device 节点的 `realm` 参数在 digest auth 场景中是否需要与 platform 侧匹配？当前决定：device 作为 UAS 时 digest challenge 由 device 发出（`handle()`），platform 侧不 challenge device，因此 `realm` 不影响功能，可用任意非空字符串。
- **Q2**：device 节点是否需要支持 `CatalogPoller` 发送 catalog query（自己作为平台向下查自己的设备）？当前决定不支持，device 无下游设备。
- **Q3**：device 节点收到 platform 发来的 INVITE 后应该如何响应？当前决定：返回 200 OK（走 `handleInvite` 现有逻辑），但媒体 pipeline 为空（Change #8 已有基本实现）。

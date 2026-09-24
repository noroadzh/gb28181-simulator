# Design

## Context

见 `proposal.md` 的 Why。实现前的相关现状：

- `NodeID` 已能从类型码 `216` 解析出 `NodeKindPlatformSmall`（`node-abstraction`），但
  `NodeService.Start` 只对 `NodeKindDevice` 走 `register`、只对 `NodeKindPlatformLarge` 走
  `serve`，platform-small 落到最后的 `return nil`——起停成功但什么也不做。
- `NodeProfile` 已经能同时持有 `registration`（UAC）与 `serving`（UAS）两段，两者互不排斥；
  配置层也已允许同一条目同时写 `platform:` 与 `registration:`，只是注释说 `platform:` 只对
  platform-large 有意义。
- 两半的用例都已存在且按节点隔离：`Acceptor.Serve/Stop`（#6，含心跳、目录应答、超时清扫）
  与 `Registrar.Register` + `Keeper.Start`（#5）。它们都不关心节点是哪一种 kind。
- 状态机 `node_status.go` 没有 `online → online` 边；`serve` 与 `register` 各自都会推
  `registered → online`。

也就是说：这一 change 不需要新的用例类，需要的是**分派**与**幂等**。

## Goals / Non-Goals

**Goals:**

- 让 platform-small 在一次 `Start` 里建立两半（受理 + 注册保活），并作为一个节点推进/回卷。
- 让配置层明确"两段对 platform-small 都有效"。
- 用一条三级级联的 e2e（`device → platform-small → platform-large`）证明中间那一环真的既是
  UAS 又是 UAC，且上级能向它查到它下面的设备。

**Non-Goals:**

- 主动能力（SUBSCRIBE 目录订阅、INVITE 点播、OPTIONS 保活、MediaStatus）——#7 的后续
  change，本 change 只把身份与级联链路打通。
- 目录在平台之间的**同步**（把学到的下级通道报给上级）。本 change 的目录应答只回答"注册到
  我的那些设备"，跨级目录汇聚需要订阅与通道表，留给下一部分。
- 新的 HTTP 端点、新的配置键。
- 级联转发头（`X-RoutePath` / `X-PreferredPath`）——Change 9。

## Decisions

### 1. 复用既有用例，不为 platform-small 新建服务类

`Start` 对 platform-small 依次调用既有的 `serve` 与 `register`。

- 备选：新建 `Cascader` 用例，把两半编排在它内部。
- 取舍：`Acceptor` 与 `Registrar`/`Keeper` 已经按节点隔离、各自处理失败与停止，再包一层只会
  多一个要维护的状态容器。真正新的东西是"分派顺序"和"推进幂等"，两者都属于 `NodeService`。
- 结论：不改 `app` 的分层，只改 `NodeService` 的分支。

### 2. 先受理，再注册

顺序为 `serve` → `register`。

- 理由：受理几乎不会失败（监听已由 lifecycle 绑好），而注册失败是常态（上级不可达、密码
  错）。把容易失败的一半放在后面，失败路径就只是"停掉已建立的受理"，与 `NodeService` 已有
  的 `stopServing` 完全一致。
- 备选：先注册后受理。失败时需要回滚一次向上的注册（注销 + 停止保活 + 清在线表），路径更
  长，且上级会短暂看到一个马上又消失的下级。
- 结论：serve-first。

### 3. 状态推进做幂等，不改状态机表

新增一个内部 `advanceOnline(ctx, id)`：若节点当前是 `idle`/`registering` 才 `MarkRegistered`，
若还不是 `online` 才 `MarkOnline`；已经是则跳过。`serve` 与 `register` 都改用它。

- 备选 A：给状态机加 `online → online` 自反边。会放宽所有 kind 的转换规则，代价大于收益。
- 备选 B：让 platform-small 的第二半干脆不推进状态。那第二半失败时就没人负责把节点推到
  `online`/`fault`，语义更乱。
- 结论：幂等推进，状态转换表保持原样（`node_status.go` 不动）。

### 4. 任一半失败即回滚另一半并 fault

`register` 失败时调用 `stopServing(id)`（停受理 goroutine + 清空该节点在线表）再 `fault`。
这与 #6 已确立的"先停受理再释放端口"一致；`lifecycle.Stop` 由 `fault` 路径统一处理。

### 5. 受理对平台身份是固有的，不引入开关

platform-small 与 platform-large 一样：`platform:` 段省略时取域默认并受理。不给
`platform.enabled` 之类的新键。

- 理由：现实中的小平台（接入网关、边缘平台）总是受理下级的；"不受理的平台"没有对应物，为
  它加一个配置键是给不存在的需求留口子。纯下级场景由 `registration:` 单独表达即可——它仍然
  受理，只是没人注册上来。

### 6. 一个监听口上的两半要分捡

两半都跑在节点自己的那个监听口上（spec 如此要求），但一个 socket 把每个报文
只交给"先读到的那一半"。受理半是一直在读的，它会把注册半等待的 `401` / `200 OK`
取走，发现不是请求就丢弃，注册永远完不成——这一点是 e2e 4.1 第一次跑的时候暴露
出来的（注册必然超时）。

新增 `splitTransport`（`internal/app/node_split_transport.go`）：由**一个** reader
独占 socket，按方向分捡——请求给受理半，响应给注册半；两半仍然从同一个 socket
发出，所以对端看到的始终是一个节点一个地址。

- 备选 A：给 `Transport` 加按 Call-ID 的事务登记。那是真正的 SIP 事务层，本
  change 用不到（小平台目前不会在上游侧收到请求），只是把成本提前。
- 备选 B：给两半各配一个监听口。直接违反 spec 的"两半都走自己的 transport"，
  且让一个节点在上级眼里变成两个地址。
- 结论：按方向分捡。一个节点一个 splitter，随节点停止/异常而结束，不读的那一
  半不会堵住另一半（无人认领的报文丢弃而不是排队）。

### 7. 配置层只改文档与注释

`validateNodePlatform` 与注册段的校验都是按条目进行的、与 kind 无关，无需改动；需要改的是
`NodePlatformConfig` 的注释（"only meaningful for a platform-large" → 对 platform-small 同样
生效）与 `configs/config.example.yaml`、`README` 的说明。无新增键，因此向后兼容。

## Risks / Trade-offs

- **两半共享一个状态机，"半在线"不可表达。** 一个 platform-small 若受理成功但注册失败，会
  整体 fault 而不是停在"只受理"的中间态。→ 这是刻意的：节点状态只能有一个，`fault` 比一个
  说不清的中间态更好解释；日志会写明是哪一半失败。
- **注册失败会清空已受理的在线表。** 下级此刻可能已经注册上来。→ 与 #6 "停止即清空"一致，
  且平台即将不再服务，保留表只会让上级查到已经不可达的设备。
- **跨级目录尚未汇聚。** 上级向小平台查目录只会拿到直接注册到小平台的设备，拿不到更下一级
  的（本 e2e 中恰好就是直接下级）。→ 已在 Non-Goals 中声明，由目录订阅的 change 补齐。
- **分捡是按方向，不是按事务。** 现在小平台在上游侧只会收到响应，按方向分捡够用；
  一旦 #7 后续给它加上上游来的请求（INVITE / SUBSCRIBE），分捡必须升级为按
  Call-ID / branch 匹配。→ 已在 `docs/architecture.md` 中写明这一边界。
- **e2e 依赖三个真实 socket。** 端口由 `freeAddr` 逐个申请，UDP 偶发丢包可能让注册重传。
  → 沿用既有 e2e 的 `waitUntil` 轮询与 generous 超时（20–30s），不引入硬等待。

## Migration Plan

无需迁移：行为此前为空（platform-small 启动后什么都不做），放开后只会新增能力。配置向后
兼容——已有的 platform-small 条目若未声明任何一段，行为从"什么都不做"变为"以默认值受理"，
这是该身份应有的语义，且不影响 device / platform-large。

## Open Questions

无。

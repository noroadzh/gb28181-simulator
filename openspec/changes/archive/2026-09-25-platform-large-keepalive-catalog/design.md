## Context

第一部分（`2026-09-24-platform-large-registration`）之后，platform-large 节点能受理 REGISTER、
下发 401 挑战、校验 Authorization、协商 Expires，并把通过的下级记进按节点分区的在线设备表。
`app.Acceptor` 每个平台节点一个 goroutine：`Receive` → 只认 REGISTER → `Send` 回 peer，
其余 method 记 debug 后丢弃。

缺的是让这张表"活起来"的两件事：接收下级的 MANSCDP `Keepalive`（否则表只反映"注册过"），
以及把过期未续期的下级踢下线（否则拔网线的设备永远在线）；再加上对上级目录查询
（`CmdType = Catalog`）的应答——这是任何上级平台接入后立刻会发的第一条控制消息。

现成可复用的部分：

- `port.Ticker` / `port.TickerFactory`（`internal/domain/port/clock.go`）——device 侧 `Keeper`
  已用它做心跳节拍，测试可手动打点，这里照搬。
- `internal/adapter/manscdp/keepalive.go`——MANSCDP 的 `xml.MarshalIndent` + 手工 XML 声明 +
  golden 测试范式，目录响应沿用。
- `model.DownstreamDevice` 已有 `WithSeen(at)`（第一部分为"后来的心跳"预留）与 `ExpiresAt()`。
- `model.NewResponse` + `PortAdapter.Send`：回应 `MESSAGE` 不需要动传输层。

## Goals / Non-Goals

**Goals**

- 接收 `MESSAGE / MANSCDP Keepalive`，刷新在册设备的 `last_seen_at` 并回 `200 OK`。
- 周期性清扫：把 `expires_at` 已过的记录移出在线表并留日志。
- 应答 `MESSAGE / MANSCDP Catalog`：回 `200 OK` + `Catalog` 响应（SN 一致、按 device id 升序、
  `SumNum` 准确、空表为 `SumNum = 0`）。
- MANSCDP 的解析与渲染留在 adapter，经 `port.MANSCDPCodec` 进入用例层。

**Non-Goals**

- 级联转发（作为下级向上级注册）、点播 / 回放信令、通道（Channel）级目录、设备主动目录上报。
- 心跳超时后主动向上级推送状态（本 change 只在本地出表）。
- 新的配置项（清扫周期为代码常量，测试靠注入 ticker 驱动）。
- 新的 HTTP 路由（在线表沿用 `GET /v1/nodes/{id}/devices`）。

## Decisions

**D1 — 踢线只看 `expires_at`，不引入 idle 超时配置。**
心跳推进 `last_seen_at`，只有成功注册推进 `expires_at`；清扫把 `now >= expires_at` 的记录移出。
理由：GB/T 28181 里注册有效期是"是否在线"的权威，心跳只是活着的证据；再引入第二个超时
会让"为什么它下线了"变成两个答案。代价：只发心跳不重注册的设备到期即被踢——这正是本模拟器
device 侧在半程重注册的原因。

**D2 — 未在册设备的心跳不回应。**
记 warn 后丢弃。理由：平台不承认未在册者；本模拟器 device 侧把无应答计入失败并在阈值后
fault，语义自洽（平台重启清空表后，下级确实应当发现自己在平台上已不存在）。

**D3 — 目录条目的唯一来源是在线设备表。**
不引入通道 / 目录配置：本 change 不做通道模型，而"表即目录"既最小可用，又与真实平台
"只上报当前在线设备"一致。`CivilCode` 取 device id 前 6 位行政区域码，`Status` 在册即 `ON`；
`Manufacturer` / `Model` 等未知字段给固定占位值而非留空，避免上级解析出空串。

**D4 — 清扫周期是内部常量（30s），ticker 由 `port.TickerFactory` 注入。**
不新增配置键：清扫频率不影响协议语义，加一个键只会多一处需要校验与解释的地方。测试通过
注入 scripted ticker 手动打点，与 `Keeper` 一致。

**D5 — MESSAGE 分发放在 `Acceptor` 的既有 goroutine 里，不新建服务。**
每个平台节点只有一个 `Receive` 循环；再开一个 goroutine 读同一 transport 会与它争消息。
清扫另起一个 goroutine（它靠 ticker 而非 `Receive` 唤醒），随 session 的 ctx 结束。

**D6 — 新增 `port.MANSCDPCodec`，不扩 `port.KeepaliveCodec`。**
两个端口方向不同：`KeepaliveCodec` 是设备侧"发"，`MANSCDPCodec` 是平台侧"收 + 应答目录"。
合并会强迫设备侧依赖它用不到的解析能力，也会让平台侧为一句 `MarshalKeepalive` 买单。

**D7 — 领域新增两个值对象：`model.Notify`（收到的通知）与 `model.Catalog`（目录响应）。**
`Notify{CmdType, SN, DeviceID, Status}` 描述"下级说了什么"，`Catalog{DeviceID, SN, Items}`
描述"平台回答什么"。都不含凭据，构造即校验（`NewNotify` 要求 CmdType 与 DeviceID、
`NewCatalog` 要求 DeviceID；`CatalogItem` 要求 DeviceID）。

**D8 — 畸形 / 无关报文只记日志，不改状态、不中断循环。**
解码失败、CmdType 不认识、SN 缺失：debug 或 warn 一条，然后 `continue`。理由：一个畸形包
不该打掉整个平台的受理循环，这是 UAS 的鲁棒性底线。

**D9 — 测试分三层。**
adapter 用 golden 固定 XML 字节；app 用 fake codec + scripted ticker 驱动节拍；e2e 用真实
socket：device 节点注册到 platform-large 节点后由 `Keeper` 发心跳，断言平台侧 `last_seen_at`
前进，再由一个裸 transport 发 `Catalog` 查询并解析回 200 + XML。

**D10 — 兼容性。**
第一部分的行为与配置一字不改；无新配置键、无新 HTTP 路由；`NewAcceptor` 只多一个
`port.TickerFactory` 参数（内部调用点一处，缺 nil 时回落到真实 ticker）。

## Risks / Trade-offs

- **踢线过激**：只发心跳、从不重注册的第三方设备会在到期时被踢（D1）。这是刻意的：注册有效期
  才是协议的权威；若将来要更宽容，应加"宽限期"配置而不是改判定来源。
- **目录条目信息量少**：`Parental` / `SafetyWay` / `RegisterWay` / `Secrecy` 等字段给固定值。
  真实目录要等通道模型（后续 change）才有意义。
- **清扫与注册的竞争**：清扫移除一条记录的同时该设备正在重注册。判定是"读到过期就删"，
  重注册随后会重新写入，最坏情况是表里短暂缺一条；不加锁跨越两次 IO，避免清扫持锁阻塞受理。
- **XML 声明手工拼**：`encoding/xml` 不写声明。沿用第一部分 keepalive 的做法（常量 + MarshalIndent），
  并用 golden 固定结果。

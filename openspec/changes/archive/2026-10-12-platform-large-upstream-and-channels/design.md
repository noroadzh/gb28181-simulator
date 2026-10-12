# Design: platform-large 双半与通道聚合

## Context

现状（来自 `node_service.go`）：

```
启动分支 (node_service.go:312-330)

case model.NodeKindDevice:     → register() 或 advanceOnline()
case model.NodeKindPlatformLarge: → serve()               ← 纯 UAS，无 upstream
case model.NodeKindPlatformSmall: → startPlatformSmall()  ← 双半（先 serve，后注册）
```

`unregisterUpstream`（node_service.go:586-588）对 `platform-large` 直接 return，不处理上游告别。

Catalog 应答（`acceptor.go:2186` 附近）只取 `devices.List`（已注册下级），不聚合节点自身通道。

`WithChannelAdded`（`node.go:484`）没有按 `NodeKind` 限制——实现与 `channel-http-api` spec 的"非 device 节点回 404"条款不一致。

本设计基于上述事实，参考 proposal.md 的 Why 章节，不重复其内容。

---

## Goals / Non-Goals

**Goals:**

1. `platform-large` 节点在声明 `registration:` 时能向上级平台完成 REGISTER / 401 挑战 / 续期 / 保活，路径与 `platform-small` 一致。
2. `platform-large` 节点可通过 `POST /v1/nodes/:id/channels` 增删通道，通道即时进入 Catalog 渲染。
3. Catalog 查询响应的 `DeviceList` = 下级在线设备（`devices.List`） + 节点自身通道（`node.Channels()`），统一按 device id 升序排列。
4. spec 与实现对齐：`channel-http-api` 的通道增删行为描述与实际代码一致（"非 platform 节点回 404"）。

**Non-Goals:**

- 不引入新节点类型（如 platform-direct）。
- 不支持一个 `platform-large` 同时向多个上级注册（单上级是本 change 边界）。
- 不修改 `platform-small` 的行为契约。
- 不修改 PTZ / Record / Playback / Talk / Snapshot 端点——这些在 `node_service.go` 中以 "not a device" 拒绝的逻辑暂时维持不变，待路线图第 10 阶段处理。
- 不修改 `X-RoutePath` 等级联头语义（`cascade-routing` spec 不受影响）。

---

## Decisions

### D1：复用 `platform-small` 的 registration 半套，而非新建逻辑

**结论**：platform-large 启动时，若节点声明了 `registration:`，走 `startPlatformSmall` 同序推进。

**依据**：`platform-small-node` spec 已沉淀完整的"先 serve / 后注册 / 任一失败回滚"逻辑；platform-large 需要的语义完全一致（都是先 UAS 后 UAC）。两个节点类型启动入口不同（`NodeKind` 分支），但 `startPlatformSmall` 的内部序列对两者等价。

**实现路径**：在 `node_service.go:324-327` 的 `platform-large` 分支加一个分支判断：

```
if wants, _ := node.Registration(); wants {
    return s.startPlatformSmall(ctx, id, node, reg, wants, transport)
}
return s.serve(ctx, id, node, transport)
```

**替代方案 A**（已排除）：把 `startPlatformSmall` 抽象成一个通用函数 `startNodeDuo`，让两个 kind 共用。代价是需要重构现有函数签名，且 `platform-small` 的 spec 契约需要显式说明"本逻辑可被其他 kind 复用"，增加理解成本。**本 change 范围外。**

**替代方案 B**（已排除）：在 `platform-large` 分支内重新写一遍 registration 逻辑。代价是重复代码、两个路径行为漂移风险。**不采用。**

---

### D2：Catalog = 在线设备表 + 节点通道，统一排序

**结论**：在 `acceptor.go` 的 `Catalog` 处理路径中，将 `node.Channels()` 的输出与 `devices.List` 合并，按 device id 升序渲染。

**关键点**：`node.Channels()` 返回的是 `model.Channel` 列表，需要映射为 `CatalogItem` 才能进 `Catalog`。映射字段取：

| Channel 字段 | CatalogItem 字段 | 备注 |
|---|---|---|
| `ID()` | `DeviceID` | 20 位国标码，通道 ID 本身即为设备编码 |
| `Name()` | `Name` | 直取 |
| `Status()` | `Status` | ON→`ON`，OFF→`OFF` |
| `ParentID()` | `CivilCode` | 回退到节点域的前 6 位行政区域码 |
| 固定 | `Manufacturer` | "SIMULATOR" |
| 固定 | `Model` | "VIRTUAL" |
| 固定 | `Secrecy` | 0 |

**为何不另外构造 `DownstreamDevice`**：下级设备的 Catalog 条目来自 `DownstreamDevice`（含来源地址、注册有效期等），通道没有这些字段。两者用同一渲染路径（`CatalogItem`），在视图层统一序列化，无须改 `CatalogItem` 的字段定义。

**排序**：统一在合并后按 device id 升序排列一次；空通道 + 空设备表 = `SumNum=0` 的合法空响应。

**已有代码位置**：`acceptor.go:2190-2202` 附近是设备列表渲染处，通道合入在该处插入。

---

### D3：通道增删端点对 platform-large 开放，spec 与实现对齐

**结论**：`channel-http-api` spec 中"非 device 节点回 404"的 Scenario 改为"非 platform 节点回 404"；实现端 `handleChannelAdd/Remove` 不需要改代码（实现本来就不拦），只需补齐 platform-large + channel 的端到端测试。

**依据**：实测 `model.NodeProfile.WithChannelAdded` 没有按 kind 限制；`channels.go` 的 handler 只调 `nodes.AddChannel`，不在 HTTP 层按 kind 拒绝。spec 文本与实现不一致，本 change 将 spec 修正以反映现状。

**非 platform 节点**：目前只有 device 和 platform-large / platform-small 有通道语义。节点种类扩展时（例如未来引入的中间件节点）需要在 spec 中显式更新排除列表。

---

### D4：级联拓扑下 platform-large 的 catalog 行为

**结论**：`platform-large` 作为级联中间节点时，其 Catalog 查询的响应来源是"注册到自己的下级" + "自己的通道"，与当前行为一致。级联转发（`cascade-routing` spec）不受本 change 影响。

**说明**：若 `platform-large` 声明了 `registration:` 向上级注册，它自己的在线设备表（被上级查询时用）仍然只含注册到它的下级；它不会去查自己的上级再合并。这是 GB/T 28181 标准的"节点自治"语义，不是本 change 要覆盖的范围。

---

## Risks / Trade-offs

**[Risk] spec 与实现的缺口在测试覆盖前未暴露**

当前 `handleChannelAdd/Remove` 没有按 kind 返回 404 的代码路径——实现已经允许 platform-large 加通道，只是 spec 写了"非 device 回 404"。如果未来有人在 HTTP handler 层加 kind 检查，需要同步更新 spec。

→ ** Mitigation**：在本 change 的 tasks.md 中显式列一条"写 platform-large + channel 的端到端 HTTP 测试"，覆盖 `POST /v1/nodes/:id/channels` → 201 → Catalog 包含该通道的全链路验证。

**[Risk] channel 字段映射到 CatalogItem 时假设通道 ID 是 20 位国标码**

若用户通过 API 添加了一个不符合国标码格式的通道 ID（如 `test_ch_1`），该条目会进入 Catalog 响应，但上级真实平台可能因为不符合格式规范而拒绝解析。

→ ** Mitigation**：在 `handleChannelAdd` 层面加 ID 格式校验（必须是 20 位数字），不符合格式的回 400。这在 tasks.md 中列为"输入校验增强"任务。

**[Risk] `startPlatformSmall` 中的 goroutine 清理逻辑依赖 `platform-small` 的 `NodeKind` 判断**

当前 `unregisterUpstream` 按 `NodeKindPlatformSmall` 判断是否需要向上级发注销。若 platform-large 也走 `startPlatformSmall`，这个判断会被绕过（因为 `NodeKind` 是 PlatformLarge，不会命中），导致 platform-large 的反向注册在节点停止时不发 `Expires: 0` 的告别 REGISTER。

→ ** Mitigation**：在 tasks.md 中显式列一条"核查 `unregisterUpstream` 对 PlatformLarge + wants=true 的行为"，若测试发现缺失，补上。修复路径是改变判断条件：`if node.ID().Kind() == model.NodeKindPlatformLarge && node.Registration().Want` 也走注销路径。

**[Trade-off] 两半启动顺序固定为先 serve 后注册**

当前 `startPlatformSmall` 先启动 UAS（`serve`），再启动 UAC（`register`）。对于 platform-large，这个顺序意味着端口先开放，再向上级注册。若上级在端口开放前就发查询，会得到"节点不存在"的 404（端口未监听）。

→ **这是 GB/T 28181 的标准行为**，不需要规避。真实设备启动时同样是先监听端口再向上注册。

---

## Migration Plan

**步骤 1（最小可行）**：验证现有代码路径

1. 启动一个 `platform-large` 节点，声明 `registration:` 指向一个不存在的上级，确认节点进入 `fault` 状态（而不是静默卡住）。
2. 通过 `POST /v1/nodes/:id/channels` 添加通道，通过 `GET /v1/nodes/:id/channels` 确认返回。
3. 通过 `GET /v1/nodes/:id/devices` 确认返回空，Catalog 响应的 `DeviceList` 包含新加的通道（这一步需要在 `node_service.go` 改动后测试）。

**步骤 2（实现）**：按 tasks.md 推进。

**步骤 3（验证）**：

- `go test ./internal/app/...` 全通过。
- `go test ./internal/interface/http/...` 全通过。
- 端到端测试：platform-large 声明 `registration:` + `channels:` → 启动 → 上级平台发 `MESSAGE(Catalog)` → 确认 `DeviceList` 含通道条目。

**回滚**：若实现后发现行为异常，删除 `registration:` 配置即可——该字段是可选的，回滚到纯 UAS 行为。

---

## Open Questions

**Q1**：`platform-large` 的通道与下级设备在 Catalog 中是否需要区分来源（如一个 `DeviceType` 字段标识"本级通道" vs "下级设备"）？

GB/T 28181 MANSCDP 规范中 `DeviceType` 用于标识设备类型（camera / NVR 等），但没有"本级 vs 下级"的语义。若需要区分，Catalog 响应需要扩展字段（加 XML 属性或子元素）。当前设计假设不需要区分——都是"这个 platform-large 管辖下的可见实体"。

**Q2**：平台注册时，下级设备列表中的 `Contact` / 来源地址字段如何填？

下级设备的 `DownstreamDevice` 含有来源地址（注册时的 Socket 地址）。通道没有来源地址，渲染为 Catalog 条目时这些字段留空。上级真实平台是否能接受带空地址的条目取决于其实现。这是 GB/T 28181 的已知模糊地带，当前设计保持与现有 `DownstreamDevice` 渲染路径一致，不做特殊处理。

**Q3**：是否需要为 `platform-large` 的通道实现"自动注册到上级时把这些通道当作子设备向上级 notify"（即 catalog NOTIFY）？

`platform-small-node` spec 中有"解析下级 DeviceList notify 并向上级转发"的逻辑，但 platform-large 增删通道目前没有对应的主动 notify。若第三方平台支持 `SUBSCRIBE` catalog 事件，则每次增删通道时应发一条 NOTIFY。这个行为目前不在 scope 内，留作后续 change。
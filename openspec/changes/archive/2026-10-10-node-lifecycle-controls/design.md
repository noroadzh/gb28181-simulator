# Design

## Context

节点生命周期后端已完备：`POST /v1/nodes/{id}/start|stop|unregister` 端点、六态状态机（`internal/domain/model/`）、`Lifecycle.Start` 内置 fault → idle 恢复路径均已实现。断层在两处：

1. **前端**：`web/src/api.js` 的 `startNode`/`stopNode`/`unregisterNode` 无任何组件调用；`NodesView` 仅 `onMounted` 拉取一次，无轮询。
2. **后端组合根**：遵循 design D9 只注册不启动（`cmd/gb28181-simulator/main.go`）；且 `NodeService.Start` 中无 `registration:` 的 device 节点启动后静默停留在 `registering`（对比 platform-large/platform-small 无上游时 serve 后即 `advanceOnline`，行为不一致）。

## Goals / Non-Goals

**Goals:**

- Web 节点卡片提供按状态/身份显隐的启停/重试/注销按钮。
- 节点列表轮询刷新，状态推进全程可见。
- `auto_start` 配置项支持进程启动自动推进全部节点（默认关闭，不破坏 D9 显式语义）。
- 修正无 `registration:` 的 device 节点启动后停在 `registering` 的不一致行为。

**Non-Goals:**

- 不做 WebSocket 状态推送（现有 `logs` WS 通道职责是日志，状态推送另立 change 更合适）。
- 不做自动重试策略/退避（重试是用户显式点击）。
- 不做节点级 `auto_start` 覆盖（只有顶层全局开关，保持简单）。

## Decisions

### D1：重试 = 再次调用 Start，不新增端点

`Lifecycle.Start` 已实现 fault → idle → registering 的自动恢复（`internal/adapter/nodereg/lifecycle.go`）。前端对 `fault` 状态显示「重试」按钮，点击调用与「启动」相同的 `api.startNode`。**备选**：新增 `POST /nodes/{id}/reset` + 单独 retry 端点——被否决， lifecycle.go 注释已明确"demanding a separate reset call would only add an endpoint the operator cannot act on differently"。

### D2：device 无 registration 时推进到 online（而非报错）

在 `NodeService.Start` 的 device 分支，`wants == false` 时调用 `advanceOnline`，与 platform-large/platform-small 无上游时的行为对齐。**备选**：Start 时直接返回错误"device 必须配置 registration"——被否决：设备作为被动 UAS（NVR 接收下级 INVITE）不注册任何人是合法部署形态，报错会把合法场景拒之门外；停在 `registering` 则语义错误（"正在注册"却永远不会有注册事务）。

### D3：`auto_start` 为 YAML 顶层布尔字段

加在配置结构体顶层（与 `nodes:` 平级），viper 解析默认 `false`。组合根在 `RestorePersistedState` 循环之后执行：遍历 `nodeService.List()`，逐个 `Start`，失败仅记 Warn 日志继续下一个。**备选**：`--autostart` CLI flag——被否决：配置随 YAML 走可版本化、可被环境变量覆盖，符合本项目 viper 的既有约定；CLI flag 可后续作为覆盖手段追加，不属于本 change。**备选**：并发启动——被否决：串行启动日志有序、端口冲突等失败原因更易定位，节点数量级（数十）下串行耗时可接受。

### D4：轮询用 `setInterval` + `onBeforeUnmount` 清理，间隔 4 秒

`NodesView` 现有 `refresh()` 即拉取逻辑，轮询只需包装定时器。间隔取 4 秒：低于 5 秒的上限（spec 要求），又不会对后端造成可感知压力（节点列表是内存读取）。**备选**：WebSocket 推送——归入 Non-Goals；**备选**：`setTimeout` 递归防重叠——本接口为内存读取、无阻塞风险，`setInterval` 足够，若未来加入慢查询再升级。

### D5：后端 `nodeResponse` 增加 `has_registration` 字段，前端三条件推导按钮显隐

`nodeResponse`（`internal/interface/http/nodes.go`）新增只读布尔 `has_registration`，由 `model.Node.Registration()` 是否存在直接推导——「该节点身份是否配置了上游注册」这个事实只有后端知道，前端不可见。注销按钮显隐条件为：

```
(kind === 'device' || kind === 'platform-small')
  && status === 'online'
  && has_registration === true
```

四类组合全部无歧义：device 有注册且 online → 显示；device 无注册（按 D2 已 online）→ 不显示；platform-small 同理；platform-large → 永远不显示（它是被注册方，从不向谁注册，「注销」对它没有意义）。**备选**：仅凭 `(kind, status)` 前端推导、无上游 device 点击注销后接受 409 错误提示——被否决：用户会看到一个必然失败的按钮，属于可预防的糟糕交互；一个只读字段的成本远低于误导性 UI。**备选**：后端直接给 `can_unregister` 布尔——被否决：显隐规则属于 UI 策略，`has_registration` 表达的是节点配置事实，前端组合策略更灵活（未来注册按钮等其他 UI 也可复用该字段）。

### D6：状态标签配色映射

| status | el-tag type |
|---|---|
| online | success |
| fault | danger |
| registering | warning + loading icon |
| registered | warning |
| idle | info |
| offline | info |

`registering` 与 `registered` 同为 warning 底色，靠 loading 图标区分动态过程与静态就绪。

## Risks / Trade-offs

- [auto_start 为 true 时上级不可达，全部 device 节点进入 fault] → 每个节点失败独立记 Warn 日志，用户通过 Web「重试」按钮或修正配置后重启恢复；不引入自动退避重试（Non-Goal）。
- [轮询在多标签页同时打开时请求量翻倍] → 单次请求为内存读取（`GET /v1/nodes`），4 秒间隔下无性能风险；若未来节点数增长到百级再加节流。
- [device 无 registration 推进到 online 改变既有可观测行为] → 原"停在 registering"是规格未定义的边缘行为而非承诺；delta spec 已显式定义新行为，并在 `nodes_registration_test.go` 补充回归用例。
- [前端 embed 构建物需随代码提交] → `internal/interface/webui/embed/dist` 已纳入 git（Makefile 注释明确不可删除），`make web` 重新构建后一并提交。

## Migration Plan

纯增量：`auto_start` 缺省 `false` 时进程行为与现状完全一致；device 无 registration 的新行为只影响此前规格未定义的场景。无数据迁移。回滚 = revert 提交。

## Open Questions

无。

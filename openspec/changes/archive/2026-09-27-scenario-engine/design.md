# 设计：场景引擎（路线图 #15）

## 总体架构

场景引擎是既有能力的编排层：解析 YAML 场景包 → 顺序执行步骤（复用
NodeService / FaultService 等既有应用服务）→ 产出 JSON + Markdown 报告。
引擎自身不引入任何 GB/T 28181 协议行为。

```
YAML 包(embed + 磁盘) ──► loader ──► ScenarioService ──► HTTP /v1/scenarios*
                                   │        │
                                   │        ▼
                                   │   executor（步骤分发）
                                   │     ├─ create/start/stop-node → NodeService
                                   │     ├─ send-command           → 动态行为 API
                                   │     ├─ inject-fault           → FaultService
                                   │     ├─ expect                 → 断言求值
                                   │     └─ wait                   → 定时器
                                   ▼
                              RunReport（JSON / Markdown）
```

## 关键决策

### 1. YAML 解析：gopkg.in/yaml.v3

纯 Go、零 CGO、社区标准。仅用于 loader；执行期不再触碰 YAML。

### 2. 领域模型与端口

- `internal/domain/model/scenario.go`：
  - `Scenario{Name, Description, Steps []Step}`
  - `Step{Type, Timeout (默认 30s), Description, Params map[string]any}`
  - `StepStatus` 枚举：`passed / failed / skipped`
  - `StepResult{Index, Type, Status, Duration, Error}`
  - `RunReport{ScenarioName, StartedAt, FinishedAt, Total, Steps []StepResult}`
- `internal/domain/port/scenario.go`：`ScenarioRunner` 接口
  （`List() []ScenarioMeta`、`Run(name) (RunReport, error)`、
  `LastRun() (RunReport, bool)`），HTTP 层仅依赖端口。

### 3. 包加载：embed + 磁盘合并

- 内置示例放 `internal/adapter/scenario/examples/*.yaml`，`embed.FS` 嵌入；
  冷启动零磁盘依赖。
- 配置项 `scenario.dir`（可选）：存在时扫描 `*.yaml`，与内置包按 `name`
  合并去重，磁盘优先。加载期统一校验，任何非法包导致该包被跳过并记录
  启动日志（不阻断进程），执行时对不存在的 `name` 返回 404。

### 4. 执行模型：同步顺序执行

- `Run(name)` 同步执行到结束再返回报告（当前场景均为秒级短演练，
  异步任务队列与进度推送不在本期范围）。
- 一步失败即终止，后续步骤标记 `skipped`；每步独立计时。
- 步骤分发用 `map[StepType]StepHandler` 注册表，新增步骤类型只加注册项。

### 5. 步骤类型与参数（第一期）

| type | 关键参数 | 依赖 |
|------|----------|------|
| `create-node` | `id`、`addr`、`domain`、`vendor`；可选 `registration` 子 map（`server`/`server-id`/`username`/`password`/`gb_version`/`expires`，设备/下级注册参数）；可选 `platform` 子 map（`realm` 与 `accounts` 列表，平台侧接入凭据） | NodeService |
| `start-node` / `stop-node` | `id` | NodeService |
| `wait` | `seconds` | — |
| `send-command` | `node`、`action`（alarm / catalog-update / channel-status / …）、`params` | 各运行时 API |
| `expect` | `target`（如 `node.dev01.status`）、`op`（`eq`/`contains`）、`value` | NodeService 快照 |
| `inject-fault` | `node`、`profile` 或 `clear: true` | FaultService |

`expect` 的 target 采用 `node.<id>.status` 点路径，从节点快照求值；
不支持嵌套循环/分支——顺序 + 断言已覆盖演练需求（non-goal 明确排除控制流；
前序步骤产出断言为后续演进，本期仅支持节点状态）。

### 6. 报告：同一份 RunReport 双渲染

- JSON：`RunReport` 直接序列化（含omitempty错误字段）。
- Markdown：`report.go` 内置渲染器（标题、汇总行、步骤表格），
  不引入模板引擎。

### 7. HTTP 契约（替换 501）

- `GET /v1/scenarios` → `200 [{name, description}]`
- `POST /v1/scenarios/run` body `{"name": "..."}` → `200 RunReport(JSON)`；
  未知 name → `404 {"error": "..."}`
- `GET /v1/scenarios/last-run` → `200 RunReport`；从未运行 → `404`
- 现有 501 handler 与 mock 前端数据一并移除。

### 8. Web 场景页

`ScenarioView.vue` 改为：加载真实列表 → 单行"执行"按钮（执行中禁用）
→ 完成后以表格渲染报告（状态 tag + 耗时 + 错误列）。`api.js` 增加三个
对应方法。

## 风险与取舍

- **同步执行占用 HTTP 请求**：可接受——场景均为秒级；若未来出现长场景，
  再引入异步任务与进度 API（记录为后续演进，不在本期）。
- **expect 的点路径求值**：只支持节点快照的浅层字段，避免实现通用
  表达式引擎；够用且可测。
- **非法包跳过而非启动失败**：单个坏包不应拖垮整个模拟器；执行期 404
  提供闭环反馈。

## 兼容性

`POST /v1/scenarios/run` 从 501 变为真实实现是**有意的破坏性变更**——
Change 14 的 spec 明确将其标记为占位，由本 change 取代。前端 mock 同步
移除，无其他迁移成本。

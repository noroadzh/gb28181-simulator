# Proposal

## Why

当前节点在 Web 管理界面中永远显示 `idle`，原因是进程启动后只执行 `registry.Register()`，而 design D9 把"启动节点"定义为显式操作——进程不会自动调用 `Start()`。同时前端 `NodesView` 只拉取一次数据，无轮询，用户既看不到状态推进，也无法对节点执行启停或重试。本 change 补齐这一断层：为三种节点身份（device / platform-small / platform-large）提供统一的启停/重试/注销入口，并以 `auto_start` 配置项支持进程启动时自动推进全部节点。

## What Changes

- **新增 Web 节点卡片操作栏**：每个节点卡片展示当前状态，并按状态/身份显示「启动/重试」「停止」「注销」按钮。`api.startNode` / `api.stopNode` / `api.unregisterNode` 已存在，本次补齐前端调用与按状态/身份的条件渲染。
- **新增状态轮询**：`NodesView` 每 3~5 秒刷新一次节点列表，使启停/注册进度可见。
- **新增状态标签配色**：扩展 `statusType()`，区分 idle / registering / registered / online / offline / fault 六种状态。
- **新增 `auto_start` 配置项**：YAML 顶层布尔值，`true` 时进程在注册节点后自动 `Start` 全部节点（默认 `false`，保持现有显式语义）。
- **修正无 registration 的 device 状态语义**：当前无 `registration:` 的 device 节点 `Start()` 后永远停在 `registering`。改为：若 device 未配置 `registration:` 则直接推进到 `online`（设备已在监听），或在 `Start()` 时返回明确错误要求配置上级——在 spec 中定义 MUST 行为，消除不确定性。

## Capabilities

### New Capabilities

- `node-lifecycle-controls`: 节点生命周期在 Web 管理界面中的可视化与控制能力，包括按状态/身份显示的启停/重试/注销按钮、状态轮询、状态标签配色，以及进程启动时自动启动全部节点的 `auto_start` 配置项。

### Modified Capabilities

- `node-abstraction`: 新增无 `registration:` 的 device 节点在 `Start()` 后的状态行为要求，消除"静默停在 registering"的不确定性。

## Impact

- **后端**：`internal/app/node_service.go` `Start()` 中 device 分支增加无 registration 的语义修正；`cmd/gb28181-simulator/main.go` 组合根读取 `auto_start` 并在 `RestorePersistedState` 后按配置自动启动节点；`internal/domain/config/` 增加 `AutoStart` 字段解析。
- **前端**：`web/src/views/NodesView.vue` 增加状态轮询、操作按钮栏、状态标签配色；`web/src/api.js` 已有方法接入组件；`web/src/stores/` 若需则新增节点级状态缓存。
- **配置**：YAML 示例与文档更新。
- **Non-goals**：不做 WebSocket 推送替代轮询（已有专项可后续演进）；不做节点级自动重试策略（超出按钮显式操作范围）；不做平台级 auto_start 继承/排除规则（全部/全部不，保持简单）。

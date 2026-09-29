# Proposal: scenario-engine

## Why
模拟器已经交付了全部协议能力（注册、目录、报警、媒体、级联、故障注入、抓包），但每一项都只能通过手动的 API 调用触发，缺少一种统一的方式将这些能力编排为可重复执行的端到端演练。Change 14 交付的 Web 管理界面已经预留了场景页与 `POST /v1/scenarios/run` 端点，但该端点目前返回 501。路线图的最后一个里程碑，就是用真正的 YAML 驱动场景引擎替换这个占位，并产出结构化执行报告。

## What Changes
- 新增声明式 YAML 场景包格式：有序步骤列表，支持步骤类型 `create-node`、`start-node`、`wait`、`send-command`、`expect`、`inject-fault`、`stop-node`，每步携带独立的 `timeout`（秒，默认 30）与 `description`。
- 新增场景引擎：加载场景包（内置示例 + 可选的磁盘目录），在运行的 NodeRegistry / NodeService 之上顺序执行步骤，记录每步结果，产出结构化执行报告（JSON + Markdown）。
- 用真正的运行时 API 替换 Change 14 的 501 占位：场景包列表、场景运行（默认同步完成并限制最大时长）、最近一次运行结果查询。
- 将 Web 场景页从 mock 卡片升级为真实场景包列表、运行反馈与报告渲染。
- 内置 3 个示例场景包：register-keepalive-catalog、alarm-capture、fault-injection drill。

## Capabilities

### New Capabilities
- `scenario-engine`：新增能力。负责 YAML 场景包解析、基于既有 domain 服务的顺序步骤执行、报告产出，以及运行时 HTTP API 的 list / run / result 契约。

### Modified Capabilities
- `scenario-manager`：既有能力。场景页停止作为 mock / 501 预览，必须改为列出引擎中的真实场景包、触发执行并渲染返回的报告；501 占位需求被本 change 取代。

## Impact
- **New files**：`internal/domain/model/scenario.go`、`internal/domain/port/scenario.go`、`internal/app/scenario_service.go`、`internal/adapter/scenario/`（YAML loader + step executor + report）、内置示例目录 `internal/adapter/scenario/examples/*.yaml`。
- **Modified files**：`internal/interface/http/scenarios.go`（真实 handlers）、`internal/interface/http/server.go`（路由接线）、`web/src/views/ScenarioView.vue`、`web/src/api.js`、`internal/platform/config/config.go`（scenario 目录配置项）。
- **Dependencies**：`gopkg.in/yaml.v3`（纯 Go，无 CGO）。
- **Non-goals**：不做可视化录制器/编辑器；不新增 GB/T 28181 协议行为（仅复用既有能力）；不做多进程或分布式编排；不做复杂控制流（本期仅为顺序步骤 + 每步超时）；不作为单元/集成测试的替代方案。

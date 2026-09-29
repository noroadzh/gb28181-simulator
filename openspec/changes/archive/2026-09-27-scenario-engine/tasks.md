# Tasks

## 1. 依赖与领域模型

- [x] 1.1 引入 `gopkg.in/yaml.v3` 依赖并确认 `go build ./...` 通过（无 CGO）
- [x] 1.2 创建 `internal/domain/model/scenario.go`：`Scenario`、`Step`（含默认 30s 超时）、`StepStatus`（passed/failed/skipped）、`StepResult`、`RunReport`，单元测试覆盖默认值与 JSON 序列化后 `go test ./internal/domain/model/` 通过
- [x] 1.3 创建 `internal/domain/port/scenario.go`：`ScenarioRunner` 接口（`List` / `Run` / `LastRun`），确认 `go build ./...` 通过

## 2. 场景包加载器

- [x] 2.1 实现 `internal/adapter/scenario/loader.go`：YAML 结构解析与校验（name/steps/type 必填、未知步骤类型报错、错误含文件与字段名），表驱动单元测试覆盖合法包与 3 类非法包（缺 name、缺 type、未知 type），`go test ./internal/adapter/scenario/` 通过
- [x] 2.2 实现 embed 内置包 + `scenario.dir` 磁盘目录扫描与按 `name` 合并去重（磁盘优先）；非法磁盘包跳过并记 slog 启动日志，测试覆盖合并优先级与坏包跳过
- [x] 2.3 在 `internal/platform/config/config.go` 增加可选 `scenario.dir` 配置项并接线默认值，确认现有配置 golden test 不回归

## 3. 步骤执行器

- [x] 3.1 实现 executor 骨架：步骤注册表分发、顺序执行、单步超时控制、失败即终止且后续步骤标记 skipped、每步计时，单元测试覆盖全通过/中途失败/超时三条路径
- [x] 3.2 实现 `create-node` / `start-node` / `stop-node` 步骤（复用 NodeService），集成测试创建并启动一个 device 节点成功
- [x] 3.3 实现 `wait` 与 `send-command` 步骤（alarm、catalog-update 等经既有运行时 API），集成测试触发一次 Alarm 成功
- [x] 3.4 实现 `expect` 步骤：`node.<id>.<field>` 点路径求值 + `eq`/`contains` 断言，表驱动测试覆盖命中、不命中、目标不存在三种情形
- [x] 3.5 实现 `inject-fault` 步骤（安装/清除 profile，复用 FaultService），集成测试注入后清除成功

## 4. 执行报告

- [x] 4.1 实现 JSON 渲染（RunReport 直接序列化）与 Markdown 渲染（标题、汇总、步骤表格，无模板引擎依赖），golden test 锁定同一报告的两种输出，`go test ./internal/adapter/scenario/` 通过

## 5. 应用服务

- [x] 5.1 实现 `internal/app/scenario_service.go`：`List` / `Run`（同步执行并保存 last-run）/ `LastRun`，单元测试用假 runner 验证 last-run 保存与未运行时返回未执行状态

## 6. HTTP API（替换 501）

- [x] 6.1 改造 `internal/interface/http/scenarios.go`：`GET /v1/scenarios`、`POST /v1/scenarios/run`（未知 name 返回 404 + JSON 错误体）、`GET /v1/scenarios/last-run`（未运行返回 404），删除 501 占位实现；httptest 覆盖列表/执行/404 三条路径，`go test ./internal/interface/http/` 通过
- [x] 6.2 在 `server.go` 完成路由接线并确认 `go build ./...` 通过

## 7. 内置示例场景包

- [x] 7.1 编写 3 个内置 YAML：`register-keepalive-catalog`（create→start→wait→expect 在线）、`alarm-capture`（注册后 send-command 触发 Alarm + expect 断言）、`fault-injection`（注册后注入错误应答故障并 expect、最后清除）；每个包通过 loader 校验测试

## 8. Web 场景页

- [x] 8.1 `web/src/api.js` 增加 `listScenarios` / `runScenario` / `getLastRun` 三个方法，`npm run build` 通过
- [x] 8.2 改造 `ScenarioView.vue`：真实列表（名称/描述/执行按钮）、执行中禁用按钮、完成后表格渲染报告（状态 tag + 耗时 + 错误列）、失败步骤错误可见；构建后人工核对一次冷启动页面渲染

## 9. 端到端验证与收尾

- [x] 9.1 `go vet ./... && go test ./...` 全绿、无新增诊断问题
- [x] 9.2 启动进程后按 spec 场景手工演练：冷启动 `GET /v1/scenarios` 至少返回 3 个内置包、执行 `register-keepalive-catalog` 得到 passed 报告、未知 name 得到 404
- [x] 9.3 `openspec validate --strict --specs` 18/18 通过且本 change delta 校验通过

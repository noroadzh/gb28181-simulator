# scenario-engine 规范

## Purpose

`scenario-engine` 将模拟器已有的协议能力（注册、目录、报警、媒体、故障注入、抓包）编排为可重复执行的端到端演练：以声明式 YAML 场景包描述步骤序列，由引擎顺序执行并产出结构化执行报告。

## Requirements

### Requirement: YAML 场景包是自描述且可校验的

系统 MUST 支持以 YAML 文件描述场景包：顶层字段包含 `name`（唯一标识）、`description` 与有序的 `steps` 列表；每个步骤 MUST 携带 `type`，并可携带 `timeout`（秒，缺省 30）与 `description`。加载场景包时系统 MUST 做结构校验；非法包 MUST 被拒绝，且错误信息 MUST 指出文件与出错字段，MUST NOT 静默忽略无法识别的步骤类型。

#### Scenario: 合法包加载成功

- **WHEN** 引擎加载一个字段完整、步骤类型均可识别的 YAML 场景包
- **THEN** 包被解析为内存场景对象，`name` 与步骤序列保持声明顺序

#### Scenario: 非法包被拒绝并指出位置

- **WHEN** 引擎加载一个缺少 `name`、步骤缺 `type`、或含未知步骤类型的 YAML 文件
- **THEN** 加载失败，错误信息包含文件路径与出错字段（或未知类型名），不产生部分初始化的场景对象

### Requirement: 步骤按声明顺序执行且失败即终止

引擎 MUST 按步骤声明顺序执行场景；每一步 MUST 受其 `timeout` 约束，超时按失败处理。任一步骤失败后引擎 MUST 停止执行，后续步骤 MUST 被标记为 `skipped` 而非执行或静默通过。步骤的执行结果 MUST 记录状态（`passed` / `failed` / `skipped`）、耗时与错误信息。

#### Scenario: 全部步骤通过

- **WHEN** 一个场景的所有步骤都在各自超时内成功
- **THEN** 场景运行结果为 `passed`，每步记录独立状态与耗时

#### Scenario: 步骤失败终止场景

- **WHEN** 场景第 2 步失败（断言不匹配或操作出错）
- **THEN** 运行立即停止，第 2 步标记 `failed` 并记录错误，其余后续步骤标记 `skipped`，场景结果为 `failed`

#### Scenario: 步骤超时按失败处理

- **WHEN** 某步骤在声明的 `timeout` 内未完成
- **THEN** 该步骤标记 `failed`，错误信息说明超时，场景按失败终止

### Requirement: 步骤类型覆盖既有节点能力

系统 MUST 至少支持以下步骤类型，且全部复用既有 domain 服务而不引入新协议行为：`create-node`（按 profile 创建节点）、`start-node` / `stop-node`（节点启停）、`wait`（固定等待）、`send-command`（经运行时 API 触发动态行为，如报警、目录更新）、`expect`（对节点状态的断言，target 为 `node.<id>.status` 点路径，支持等于/包含匹配；前序步骤产出断言为后续演进，本期不支持）、`inject-fault`（安装或清除故障 profile）。

#### Scenario: 注册-心跳-目录演练可被编排

- **WHEN** 场景依次执行 create-node（device 与 platform-large）、start-node、wait、expect（设备注册在线）
- **THEN** 场景在不编写任何代码的情况下完成一轮注册演练并产出 `passed` 报告

#### Scenario: 报警与抓包演练可被编排

- **WHEN** 场景在注册后执行 send-command（触发 Alarm）与 expect（抓包缓冲出现对应事件）
- **THEN** 报告记录该断言结果，演示动态行为与抓包能力的组合

### Requirement: 执行报告是结构化且可获取的

每次运行 MUST 产出报告，包含：场景 `name`、开始与结束时间、总结果、每步的状态/耗时/错误。系统 MUST 同时提供 JSON 与 Markdown 两种表示；JSON MUST 可被程序消费，Markdown MUST 人类可读。最近一次运行报告 MUST 可通过运行时 API 获取。

#### Scenario: 报告反映失败步骤

- **WHEN** 一个场景在第 2 步失败后结束
- **THEN** 报告总结果为 `failed`，第 2 步的条目含错误信息，后续步骤条目为 `skipped`

#### Scenario: Markdown 报告人类可读

- **WHEN** 以 Markdown 形式渲染同一份报告
- **THEN** 报告含场景名、每步结果表格与总耗时，无 JSON 转义噪声

### Requirement: 运行时 API 替换 501 占位

系统 MUST 提供：`GET /v1/scenarios`（列出可用场景包：内置示例 + 配置目录中的包）、`POST /v1/scenarios/run`（按 `name` 执行一个场景并返回其报告）、`GET /v1/scenarios/last-run`（返回最近一次运行的报告）。请求不存在的场景名 MUST 返回 404 与 JSON 错误体。上述端点 MUST NOT 再返回 501。

#### Scenario: 列出场景包

- **WHEN** 客户端调用 `GET /v1/scenarios`
- **THEN** 响应 200 并返回 JSON 数组，每项含 `name` 与 `description`

#### Scenario: 执行场景并返回报告

- **WHEN** 客户端以存在的场景名调用 `POST /v1/scenarios/run`
- **THEN** 响应 200 并返回该次运行的完整报告；响应到达时执行已完成（同步语义）

#### Scenario: 未知场景名返回 404

- **WHEN** 客户端以不存在的 `name` 调用 `POST /v1/scenarios/run`
- **THEN** 响应 404 与 JSON 错误体，不启动任何执行

### Requirement: 内置示例场景随二进制交付

系统 MUST 以 embed 方式内置至少 3 个示例场景包：注册-心跳-目录、报警-抓包、故障注入演练。未配置磁盘场景目录时，内置包 MUST 仍可被列出与执行；配置了目录时，目录中的包与内置包按 `name` 合并去重（磁盘优先）。

#### Scenario: 冷启动即可列出内置场景

- **WHEN** 进程以默认配置（无场景目录）启动并调用 `GET /v1/scenarios`
- **THEN** 响应至少包含 3 个内置场景包

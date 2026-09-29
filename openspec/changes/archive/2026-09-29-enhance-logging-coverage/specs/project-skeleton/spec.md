# Spec Delta

为已有 capability `project-skeleton` 中的 Requirement `分级结构化日志与流式输出` 补充模块级日志配置与运行时热更新行为（增量 Scenario，不删除已有 Scenario）。

## MODIFIED Requirements

### Requirement: 分级结构化日志与流式输出

系统 MUST 使用分级结构化日志，包含五个严重等级（trace、debug、info、warn、error），同时写入文件 sink 与 WebSocket sink，并对配置的敏感字段做脱敏处理。系统 MUST 支持按模块路径独立设置日志级别，并通过 HTTP 接口实现运行时热更新。

#### 场景：五个等级可配置且可过滤

- **WHEN** 配置中设置日志级别为 `warn`
- **THEN** 文件与 WebSocket 输出均不含 trace/debug/info 记录，但保留 warn 与 error 记录

#### 场景：文件与 WebSocket 双输出一致

- **WHEN** 系统产生一条 info 级日志
- **THEN** 该记录同时出现在日志文件与 `/v1/logs/stream` WebSocket 消息中，且字段（时间戳、等级、消息、结构化属性）一致

#### 场景：敏感字段脱敏

- **WHEN** 任意 log 记录中包含名为 `password`、`secret` 或 `private_key` 的属性字段
- **THEN** 两个输出通道中该字段的值均为 `***REDACTED***`

#### 场景：无 XDG 环境变量时回退默认目录

- **WHEN** 在未设置 `XDG_STATE_HOME` 的 Linux/macOS 环境启动
- **THEN** 日志文件写入 `$HOME/.local/state/gb28181-simulator/logs/`；在 Windows 下写入 `%AppData%\gb28181-simulator\logs\`

#### 场景：模块级日志配置被读取

- **WHEN** `configs/config.yaml` 包含 `log.modules.<module-path>: <level>` 节
- **THEN** logger 初始化时为每个配置的模块路径独立设置级别，优先级高于 `log.level` 全局兜底
- **AND** 未在 `log.modules` 中列出的模块继承全局级别

#### 场景：运行时 PATCH 修改日志配置生效

- **WHEN** 调用 `PATCH /v1/config/log` 传入合法的新级别
- **THEN** logger 在 1 秒内按新级别过滤输出
- **AND** 现有 Hub 订阅者继续接收推送（无中断）

## REMOVED Requirements

无。

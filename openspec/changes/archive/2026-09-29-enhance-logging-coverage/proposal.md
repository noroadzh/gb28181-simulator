# Proposal

## Why

当前日志基础设施（Change 1, project-skeleton）已具备基础框架：slog 全局 logger、脱敏、多 handler（文件/WebSocket）。但实际业务模块（app/、adapter/ 等）中的日志调用密度不足——关键决策点、错误分支、状态转换、SIP 事务边界、媒体流状态等处缺乏 trace/debug 级别日志，导致生产问题定位困难。且 `config.example.yaml` 中仅有顶层 `log.level`，缺乏按模块细粒度控制的能力。

## What Changes

- **新增细粒度日志调用点**：覆盖 SIP 信令收发、节点状态机、媒体流、级联路由、认证/鉴权、异常注入等关键路径，以 `trace`/`debug` 为主。
- **新增模块级日志配置**：在 `configs/config.yaml` 中引入 `log.modules` 配置节，允许按包路径独立设置日志级别（如 `log.modules.sip: debug`）。
- **保留现有能力**：顶层 `log.level` 仍作为全局兜底级别，`redact_keys`、文件输出、WebSocket 推送保持不变。
- **运行时日志级别热更新**：通过 HTTP PATCH `/v1/config/log` 动态调整全局或模块级别，无需重启进程（通过 viper 热重载实现）。

## Capabilities

### New Capabilities

- `logging-coverage`: 新增 capability，定义关键日志点的覆盖范围、模块级配置语法、运行时热更新接口。该 capability 无需修改已有 spec 的 requirement，仅为增量规范。

### Modified Capabilities

- `project-skeleton`: 扩展其 Scenario（WHEN 日志系统初始化，THEN ...）中关于模块级配置的描述，补充 `log.modules` 配置节的行为约束。

## Impact

- **代码**：新增日志调用约 60~80 处，分布于 `internal/app/`、`internal/adapter/` 各子包。
- **配置**：`configs/config.example.yaml` 新增 `log.modules` 节。
- **HTTP API**：`internal/interface/http/` 已有 handler 中添加 PATCH `/v1/config/log` 端点（Viper 热重载）。
- **测试**：e2e 测试需验证日志 handler 收到各模块记录；golden test 保持不变。
- **依赖**：无新增依赖，log/slog 已在 go.mod。

## Non-goals

- 不实现结构化日志的数据库持久化（由外部日志收集系统负责）。
- 不实现日志轮转（log rotation）——由外部工具（如 logrotate）负责。
- 不改变日志输出格式（保持 JSON）。
- 不修改 Web UI 日志面板 UI（已有 `#14 web-management-ui` 覆盖）。

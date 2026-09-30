# Proposal

## Why

`enhance-logging-coverage` 与 `media-aggregator-integration` 引入了三类运维面能力：`log.modules` YAML 按模块级别覆盖、`PATCH /v1/config/log` 运行时热更新、`component/subsystem` 标签体系。这些能力目前只散落在 `configs/config.example.yaml` 注释与 `docs/smoke-test.md` §7 冒烟用例中，运维与二次开发者无法从单一入口理解：如何配 env 变量、`modules` 前缀匹配规则是什么、PATCH 契约（成功/失败语义、重启回退）是什么。

## What Changes

- 新建 `docs/logging.md`，作为日志配置的用户文档单一入口
- 覆盖内容：
  - `log.*` 配置项总表（`level` / `file` / `add_source` / `redact_keys` / `modules`）
  - `log.modules` 语法：YAML map、`component` 前缀最长匹配、可用 Level 枚举（trace/debug/info/warn/error）
  - 环境变量覆盖：`GB28181_LOG_LEVEL` 等等（与 config 包 env 绑定一致）
  - `component` / `subsystem` 标签体系：各模块的 tag 值对照表（sip_acceptor / keepalive_keeper / device_registrar / node_service / fault_store / cascade_handler / capture / media 等）
  - `PATCH /v1/config/log` 接口契约：请求/响应 schema、400 语义、内存生效/重启回退
  - 媒体聚合摘要（`internal/adapter/media` component）与 `ErrorAggregator` 窗口语义
  - 故障排查：如何用 `log.modules` 临时打开某个子系统 trace、如何识别热循环错误摘要

## Capabilities

### New Capabilities
- `logging-user-docs`: 日志配置的用户文档契约——`docs/logging.md` 必须覆盖的内容边界与准确性要求

### Modified Capabilities
- 无

## Impact

- 仅新增一个 Markdown 文档；无代码改动
- 风险：文档与实现漂移——以 spec 锁定关键契约（modules 前缀匹配、PATCH 语义、Level 枚举）并在 CI 中通过 `go test` 覆盖的 API 保持一致
- roadmap #18 关闭项

# Tasks

## 1. 文档结构与配置段

- [x] 1.1 新建 `docs/logging.md`，顶部导航 + 五段标题
- [x] 1.2 段 1 "配置参考"：`log.*` 字段表（level / file / add_source / redact_keys / modules），含类型、默认值、效果
- [x] 1.3 段 1 列出 Level 枚举（trace/debug/info/warn/error），注明未知值回退 info

## 2. 模块前缀匹配语义

- [x] 2.1 段 2 "模块级级别"：解释 `log.modules` 是 `map[string]Level`；key 为 `component` 前缀
- [x] 2.2 说明最长前缀匹配规则并给出 `internal/app` 与 `internal/app/sip_acceptor` 的覆盖示例
- [x] 2.3 列出默认全局 `log.level` 的回退行为

## 3. PATCH /v1/config/log 契约

- [x] 3.1 段 3 "运行时热更新"：请求 schema（`level` 可选、`modules` 可选，至少一个非空）
- [x] 3.2 响应 schema（回显生效值）；400 三类（空 body / 未知 level / JSON 损坏）
- [x] 3.3 明确"内存生效，重启回退到 log.level / log.modules"

## 4. 组件/子系统标签对照

- [x] 4.1 段 4 "组件标签"：从代码 grep 抽取 component / subsystem 表格
- [x] 4.2 至少覆盖 sip_acceptor / keepalive_keeper / device_registrar / node_service / fault_store / cascade_handler / capture store / media sources

## 5. 媒体聚合摘要

- [x] 5.1 段 5 "聚合与热循环"：解释 `ErrorAggregator` 60s 窗口 + signature `ps-mux` / `rtp-send`
- [x] 5.2 说明可用 `log.modules: internal/adapter/media` 单独控制

## 6. 排障 playbook

- [x] 6.1 段 6 "排障示例"：给出通过 PATCH 临时把 `sip_acceptor` 提到 trace、再回退到默认的工作流
- [x] 6.2 给出等价静态配置示例（写在 `configs/config.example.yaml`）

## 7. 验证

- [x] 7.1 重读 `internal/platform/config/config.go`、`internal/platform/observability/logging/{level,handler}.go`、`internal/interface/http/log_config.go`，确保文档与实现一致
- [x] 7.2 重读 `internal/app/*.go`、`internal/adapter/{cascade,capture,media}/*.go`，确保组件表格与代码一致
- [x] 7.3 `openspec validate logging-user-docs --strict --type change --no-interactive` PASS
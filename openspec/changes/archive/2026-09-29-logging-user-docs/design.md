# Design

## Context

用户面文档缺失：`configs/config.example.yaml` 有字段注释但不讲语义；`docs/smoke-test.md` §7 只覆盖 PATCH 冒烟用例；`openspec/specs/logging-coverage/spec.md` 是 capability 契约而非用户文档。三者分散，运维没有单一入口。

代码事实（写入文档时必须与之一致）：
- `internal/platform/config/config.go` `LogConfig{Level, File, AddSource, RedactKeys, Modules}`（commit `4305604`）
- `internal/platform/observability/logging/level.go` `ParseLevel`（trace/debug/info/warn/error，未知回退 info）、`LevelFromSlog`、`IsValidLevel`
- `internal/platform/observability/logging/handler.go` `MultiHandler` 最长前缀匹配（`defaultLevel` + `moduleLevels`）
- `internal/interface/http/log_config.go` PATCH handler + 400 语义
- `internal/app/*.go` 各构造函数的 `component` / `subsystem` 标签（`sip_acceptor` / `keepalive_keeper` / `device_registrar` / `node_service` / `fault_store`）
- `internal/adapter/cascade/handler.go` `cascade_handler`；`internal/adapter/capture/ring.go` capture store；`internal/adapter/media/*` `component=internal/adapter/media`
- `internal/adapter/media/error_aggregator.go` 60s 窗口、`ps-mux` / `rtp-send` signature（`media-aggregator-integration`）

## Goals / Non-Goals

**Goals:**
- 单一 `docs/logging.md`，运维可从零配出"模块级 trace + 热更新"的完整日志面
- 文档与代码契约绑定（YAML key、Level 枚举、PATCH 400 语义、component 表格）
- 覆盖 `media-aggregator-integration` 引入的聚合摘要语义

**Non-Goals:**
- 不写日志架构内部实现解析（那是 `docs/architecture.md` 或 spec 的职责）
- 不重复 `smoke-test.md` 的冒烟步骤（仅链接）
- 不覆盖 Web UI 的日志面板操作（独立 capability）
- 不本地化：文档以中文为主、关键字段/命令/JSON 保留英文原文

## Decisions

### Decision 1: 文档结构按"配置 → 标签 → 运行时 → 聚合 → 排障"五段

- **Rationale**: 阅读顺序与运维动线一致（先配好、再看懂记录、再动态调、再理解聚合、最后排障）。每段自足，可单独引用。
- **Alternative considered**: 按 API reference 字母序罗列。否决：不利于新手理解层级关系。

### Decision 2: component/subsystem 表格从代码抽取而非手写

写入文档时逐文件 grep `component` / `subsystem` 标签生成对照表，并在文档末尾标注"以 commit `91544ef` / `4305604` 为基准"。

- **Rationale**: 手写表格容易与代码漂移；标注基准 commit 让读者知道校对时点。
- **Alternative considered**: 代码注释生成工具。否决：引入工具链依赖超出本 change 范围。

### Decision 3: PATCH 契约以 `log_config.go` 现行为准，不引入新语义

文档只描述已实现行为（400 三类、内存生效、重启回退），不提议任何新字段。

- **Rationale**: 文档型 change 的价值在于准确而非扩展；契约变更应走独立 change。
- **Alternative considered**: 顺手补 `persist=true` 持久化选项。否决：超范围。

## Risks / Trade-offs

- **风险**: 文档写完时代码又动。缓解：tasks 中校验步骤要求最终读一遍关键文件核对；后续代码变更涉及 log 面时，change 模板要求检查 `docs/logging.md` 是否需要同步。
- **Trade-off**: 不做英文版。当前仓库文档以中文为主，保持一致。
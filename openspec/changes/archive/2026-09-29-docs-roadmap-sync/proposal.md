# Proposal

## Why

`docs/roadmap-15-steps.md` 表格中 `#15 scenario-engine` 仍标 `⬜ 待实施`，但实际归档目录 `openspec/changes/archive/` 中 `2026-09-27-scenario-engine` 已存在，`openspec list` 也已无任何活跃变更（自 2026-09-29 起 change 全部归档）。路线图文档与项目现实严重脱节，会误导后续贡献者按 15 步规划的"下一步"误以为还有未实施的核心阶段。同时项目当前已无活跃 change，需要为已识别但尚未启动的后续能力（manscdp-logging-coverage、media-aggregator-integration、logging-user-docs、scenario-engine 增强等）登记明确的下一阶段编号。

## What Changes

1. **同步路线图状态**：将 `#15 scenario-engine` 改为 `✅ 已归档（2026-09-27）`，补充归档日期列内容。
2. **新增后续阶段编号**：在 15 步路线图之后追加 `#16`–`#19` 编号占位，覆盖 `enhance-logging-coverage` 阶段已识别但 defer 的 follow-up：
   - `#16 manscdp-logging-coverage`：MANSCDP+ 业务面日志调用覆盖（alarm/catalog/keepalive/notify/ptz 等 18 个文件）。
   - `#17 media-aggregator-integration`：媒体热循环接入 error_aggregator（`ps_packetizer.go`、`rtpizer.go`）。
   - `#18 logging-user-docs`：`docs/logging.md` 用户文档（env 变量、`log.modules` 语法、PATCH 接口契约）。
3. **新增 capability `docs-roadmap-sync`**：定义路线图文档的同步契约，确保后续每次 archive 后路线图状态可机器/人工核查，避免再次脱节。
4. **状态字段语义**：明确状态枚举为 `⬜ 待实施` / `🔨 进行中` / `✅ 已归档`，并在每个 `✅` 行强制带 `（YYYY-MM-DD）` 归档日期。

## Capabilities

### New Capabilities

- `docs-roadmap-sync`: 项目路线图文档维护契约。定义路线图状态字段（阶段编号、状态、归档日期、说明）、同步触发条件（每次 archive 后必须更新）、验证手段（`openspec list` 与表格状态数对齐脚本/手动核查）。该 capability 对应于路线图文件 `docs/roadmap-15-steps.md` 的结构与维护规则。

### Modified Capabilities

无。

## Impact

- **文档变更**：仅修改 `docs/roadmap-15-steps.md`，不修改任何 Go 源码、配置、测试。
- **CI/工具**：不引入新 CI 步骤；路线图同步作为人工核查清单，由 archive 流程执行人确认。
- **依赖**：无新增依赖。
- **影响范围**：所有查阅 `docs/roadmap-15-steps.md` 的贡献者与运维人员。

## Non-goals

- 不实现自动归档与文档同步 CI hook（属于工具链集成，需后续 change 跟进）。
- 不修改 `openspec/config.yaml` 中的 context 路线图注释（context 是项目冻结的元数据，与 roadmap 文档同步策略解耦）。
- 不重新组织已归档 change 的目录结构（保持 `openspec/changes/archive/<date>-<name>/` 命名规则）。
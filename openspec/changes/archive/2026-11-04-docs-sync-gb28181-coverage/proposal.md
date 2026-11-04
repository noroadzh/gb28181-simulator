# Proposal

## Why

对照 GB/T 28181-2016 与路线图 #1–#44 的覆盖分析暴露了文档/契约漂移：`architecture.md` 仍声称回放 FLV 未接入（已由 #37 交付）、规格写 `AlarmCmd` 而线协议与实现使用 `GuardCmd`、缺少一份可维护的「国标章节 ↔ 实现」对照表，且路线图在全部归档后未登记 #45+ 承接项。现在补齐，避免联调与后续 change 继续以过期文档为准。

路线图编号：#45 `docs-sync-gb28181-coverage`（文档与契约同步；与 #46 `interop-hardening` 并行规划、本 change 先落地文档面）。

## What Changes

- 修正 `docs/architecture.md` 中回放 FLV / 过期限制描述，与 #37 及 `docs/web-ui-guide.md` 一致。
- 修正过时代码注释（如 `node_service` 中「outbound DeviceControl 未实现」）。
- 将 `device-command-coverage` 规格中的布防字段名改为与线协议一致的 `GuardCmd`（SetGuard/ResetGuard），并注明勿再写 `AlarmCmd`。
- 新增 `docs/gb28181-coverage.md`：国标第 7/9 章与附录的覆盖矩阵（✅/🟡/❌），来源为既有分析，并要求随归档更新。
- 更新 `docs/roadmap-15-steps.md`：登记 #45（本 change）、#46（interop-hardening）及后续候选，满足「全部归档后必须有承接行」。
- 帮助页或覆盖文档中写清：级联 MESSAGE 中继 ≠ 媒体 B2BUA。

## Capabilities

### New Capabilities

- `protocol-coverage-docs`：维护国标章节覆盖对照文档，并约束架构/帮助文档不得与已归档能力矛盾。

### Modified Capabilities

- `device-command-coverage`：布防/撤防需求文本改为 `GuardCmd`（与实现及 #44 proposal 一致）。
- `docs-roadmap-sync`：登记 #45/#46 及说明节中的下一阶段候选，满足 Forward roadmap numbering。

## Impact

- 文档：`docs/architecture.md`、`docs/roadmap-15-steps.md`、新建 `docs/gb28181-coverage.md`；可选同步 `docs/web-ui-guide.md` 中与 GuardCmd/级联限制相关的措辞。
- 规格：`openspec/specs/device-command-coverage`、`docs-roadmap-sync`，以及新主规格 `protocol-coverage-docs`（归档时 sync）。
- 代码：仅注释/字符串文档类修正，无行为变更。
- `.cursor/plan/gb28181-coverage-analysis.md` 可保持为工作区分析底稿；正式对照表以 `docs/gb28181-coverage.md` 为准。

## Non-goals

- 不改变任何 SIP/MANSCDP/HTTP 运行时行为（行为深化属 #46）。
- 不实现校时、SRTP、RecordCmd、PlaybackControl 入站语义等协议缺口。
- 不重写 Web UI 结构；帮助页仅允许小幅补充级联限制说明。
- 不导入或 OCR 整本 GB28181 PDF。

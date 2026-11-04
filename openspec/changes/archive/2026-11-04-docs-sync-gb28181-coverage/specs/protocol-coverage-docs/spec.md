# Spec Delta

## Purpose

维护一份与代码/路线图对齐的 GB/T 28181 章节覆盖对照文档，并约束架构与帮助文档不得与已归档能力矛盾，降低联调时的文档误导。

## ADDED Requirements

### Requirement: 国标覆盖对照文档

仓库 MUST 提供 `docs/gb28181-coverage.md`，按 GB/T 28181-2016 第 7/9 章与主要附录列出覆盖状态（✅ 已实现 / 🟡 部分 / ❌ 未做或非目标），并简要指向实现要点或缺口。该文档 MUST 可被贡献者作为联调前阅读材料，无需打开 PDF 全文。

#### Scenario: 覆盖文档存在且含第 7 章条目

- **WHEN** 贡献者打开 `docs/gb28181-coverage.md`
- **THEN** 文档至少包含注册、实时点播、设备控制、报警、目录/信息查询、心跳、录像检索、回放、下载、校时、订阅、广播/对讲等第 7 章相关行
- **AND** 每行带有 ✅ / 🟡 / ❌ 之一

#### Scenario: 级联限制写清

- **WHEN** 读者查阅覆盖文档或帮助文档中的级联说明
- **THEN** 文档明确区分「MESSAGE 信令中继」与「媒体 B2BUA」，不得暗示二者自动等价

### Requirement: 架构文档不得否定已交付能力

`docs/architecture.md` MUST NOT 声称某能力「尚未接入 / 未实现」，若该能力对应的路线图阶段已为 `✅ 已归档` 且主规格要求已存在（例如回放 HTTP-FLV）。发现矛盾时 MUST 在文档同步 change 中修正。

#### Scenario: 回放 FLV 描述与 #37 一致

- **WHEN** 阅读 `docs/architecture.md` 中回放相关段落
- **THEN** 不得再出现「`/playback/flv` 仍返回未实现 / FLV 历史流尚未接入」之类与已归档 `#37 playback-flv-stream` 矛盾的表述
- **AND** 应指向当前会话与 `/playback/flv?sn=` 行为（或等价现行端点）

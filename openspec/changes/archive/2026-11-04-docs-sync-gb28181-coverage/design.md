# Design

## Context

See proposal.md — Why。当前行为面已到 #44，但 `docs/architecture.md` 回放段落、规格 `AlarmCmd` 用词、路线图承接行与缺失的章节对照表会造成「文档说没做 / 代码已做」或「规格字段名 ≠ 线协议」的噪声。本 change 只动文档与规格文本。

## Goals / Non-Goals

**Goals:**

- 单一权威覆盖表：`docs/gb28181-coverage.md`（可由 `.cursor/plan/gb28181-coverage-analysis.md` 提炼）。
- 架构/注释/规格用词与已归档实现一致。
- 路线图出现 #45/#46 承接，满足 `docs-roadmap-sync`。

**Non-Goals:**

- 不改运行时；不 OCR PDF；不在本 change 实现 #46 语义。

## Decisions

1. **覆盖表放 `docs/` 而非仅 `.cursor/plan/`**  
   - 理由：plan 是工作区分析底稿；`docs/` 可版本管理并对贡献者可见。  
   - 备选：只更新 plan —— 拒绝，因 OpenSpec/路线图读者不看 `.cursor`。

2. **GuardCmd 用 MODIFIED 覆盖原需求全文**  
   - 理由：主规格当前错误写 `AlarmCmd`；delta 替换整段需求避免残留。  
   - 备选：同时接受 AlarmCmd 别名 —— 拒绝，实现只认 GuardCmd，双名增加对接歧义。

3. **级联限制写在覆盖表 + 帮助页一句**  
   - 理由：分析中的高频误解；不必新 UI。  
   - 备选：大改 HelpView —— 超出 Non-goals。

4. **过时注释一并改**  
   - 理由：`node_service` 误导后续实现者；属零行为风险。

## Risks / Trade-offs

- [覆盖表随后又漂] → `docs-roadmap-sync` 新增「协议归档须改覆盖表」；归档 checklist 引用。  
- [#45/#46 编号与并行 apply 顺序] → 约定先 apply 文档 change，再实现 interop；路线图两行可同时 `🔨 进行中`。

## Migration Plan

- 纯文档/规格：合并后立即生效，无数据迁移。  
- 归档本 change 时：sync `protocol-coverage-docs` 入主规格，并把 #45 标为已归档。

## Open Questions

无（文档范围已由 proposal 钉死）。

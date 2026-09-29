# Design

## Context

见 `proposal.md` — Why。此处只补充实现层面的约束：

- `docs/roadmap-15-steps.md` 是纯 Markdown 表格，渲染在 GitHub/GitLab 上，无构建步骤、无 schema 校验、无自动化测试。
- `openspec/config.yaml` 的 `context` 字段内也有一份路线图注释（`# 1 init-project-skeleton` ... `# 15 scenario-engine`），但该字段是 OpenSpec 项目元数据，OpenSpec 自身不渲染、不校验、不要求与实现同步。
- 项目 CI（`.github/workflows/`）当前只有 lint / unit test / build matrix / release 四类任务，没有 docs 校验任务。
- 已归档 change 目录命名为 `openspec/changes/archive/<YYYY-MM-DD>-<change-name>/`，日期前缀即归档日期，天然可作为路线图日期注释的事实来源。

## Goals / Non-Goals

**Goals:**
- 让路线图表格的每一行都能被肉眼或 grep 校验（状态列是受限枚举；`✅` 行必带日期）。
- 让 archive 流程有明确的"顺手更新路线图"义务，写在 spec 里而不是靠口头约定。
- 为已 defer 的三项日志 follow-up 提供正式的阶段编号，使它们可被跟踪。

**Non-Goals:**
- 不实现 CI job 自动改写 Markdown（见 Decision 2）。
- 不同步 `openspec/config.yaml` 的 `context` 路线图注释（见 Decision 3）。
- 不重排已有编号；`#16`–`#18` 严格追加在 `#15` 之后。

## Decisions

### Decision 1: 日期注释挂在 `名称` 列而非新增列

**方案**：保持现有五列不变，把 `（2026-09-27）` 追加到 `名称` 列末尾，例如 `Web 管理界面（2026-09-27）`。

**理由**：
- 现有表格已经是五列（`#` / `编号` / `名称` / `状态` / `说明`），新增第六列 `归档日期` 会让已归档的 15 行全部需要填充，无信息增量。
- `名称` 列是短语，追加日期后仍可读；`说明` 列是长句，追加日期会被淹没在句尾。
- 归档日期本质是"这一项的标识信息"，与名称同属一行标识语义。

**替代方案**：
- 新增 `归档日期` 列 → 拒绝。15 个已归档行会产生 15 个 `-` 空值，降低表格信噪比。
- 写入 `说明` 列 → 拒绝。`说明` 列已有 30+ 字的长句，日期在句尾不可见。

### Decision 2: 同步义务写在 spec，不实现 CI 脚本

**方案**：spec Requirement `Post-archive roadmap update obligation` 规定 archive 后必须同提交更新路线图；不加 CI job。

**理由**：
- OpenSpec 的 `archive` 是本地 CLI 命令，CI 并不执行它（CI 只跑 lint/test/build），因此无法在 CI 阶段拦截"archive 了但没更新文档"。
- 真正需要拦截的是**开发者本地执行 archive 后忘记更新文档**。这属于流程纪律，spec 契约 + PR review 已足够。
- 写一个校验脚本（解析 Markdown 表格、比对 archive 目录）需要引入 Markdown 解析依赖或脆弱的正则，且对 15 行静态表格收益有限。

**替代方案**：
- 加 `make roadmap-check` 脚本 + CI 步骤 → defer 到后续 change（见 proposal Non-goals）。等路线图行数超过 30 或出现多文件拆分需求时再引入。

### Decision 3: 不同步 `openspec/config.yaml` 的 context 路线图注释

**方案**：`config.yaml` 的 `context` 中 `# 1 ... # 15 scenario-engine` 保持原样不动。

**理由**：
- `context` 是 OpenSpec 的项目级冻结元数据，主要供 AI agent 在规划时读取"项目背景与技术约束"。其中的路线图注释是 2026-09-23 立项时的原始规划快照，保留它有历史对照价值。
- 改它会让 `openspec context` 每次输出都变，agent 上下文噪声增大，且没有任何可观测行为依赖它的准确性。

**替代方案**：
- 同步 context → 拒绝。context 不是文档，不承担"进度追踪"职责。

### Decision 4: defer 的三项各占一个阶段编号

**方案**：`#16 manscdp-logging-coverage`、`#17 media-aggregator-integration`、`#18 logging-user-docs`。

**理由**：
- 这三项来自 `2026-09-29-enhance-logging-coverage` 的 tasks.md `Deferred` 小节，各自有明确的验收标准（grep 日志调用数 > 0、aggregator 接入断言、文档渲染检查），彼此无共享代码路径，可以独立交付。
- 合并为一个"日志补完" change 会让 review 面过大，且 §5.1（纯文档）与 §2.4（18 个文件的业务代码）性质差异太大。

**替代方案**：
- 一并作为 `#16 logging-followups` → 拒绝。范围过大，违反"每 change 独立可交付"的项目约定。

## Risks / Trade-offs

**[Risk 1] 状态枚举靠人工遵守，spec 无机器校验**
- Markdown 表格没有任何 schema，理论上有人可以写 `状态: Done`，spec 层面无法阻止。
- **Mitigation**：接受。spec 的 Scenario 明确列出三个合法值，PR review 时人工比对；等 30+ 行时再引入脚本校验（Decision 2 已 defer）。

**[Risk 2] 阶段编号与 OpenSpec change 名脱钩**
- 路线图 `#16` 只是规划编号，与未来 `openspec new change <name>` 起的名字没有强制映射，编号可能被跳号或复用。
- **Mitigation**：spec Requirement `Roadmap document structure` 要求编号严格递增、无跳号重号。新 change 落地时由执行人在路线图追加行，编号自动成为下一个可用值。

**[Trade-off 1] 人工同步 vs 自动化**
- 放弃 CI 自动改写，换取零新增依赖与零构建耦合。代价是依赖执行人自觉。
- **接受**：项目当前阶段（15 项已全部归档，路线图文档每季度级更新频率）下人工成本可忽略。

## Migration Plan

1. 修改 `docs/roadmap-15-steps.md`：#15 状态改 `✅ 已归档` + 补日期；追加 #16–#18 三行 `⬜ 待实施`；更新文末"说明"小节的统计数字。
2. 无代码变更，无需构建/测试。
3. 回滚策略：单文件 revert。

## Open Questions

无。spec 已完整描述同步契约，设计覆盖所有决策点，不存在可推迟的未知。

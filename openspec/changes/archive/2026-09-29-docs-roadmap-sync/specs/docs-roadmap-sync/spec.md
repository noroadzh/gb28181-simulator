# Spec Delta

## Purpose

维护项目路线图文档 `docs/roadmap-15-steps.md` 与归档现实的同步契约：定义状态字段、必填列、同步触发条件，以及防止文档再次脱节的验证机制。

## ADDED Requirements

### Requirement: Roadmap document structure

路线图文档 MUST 包含以下列，且每一行 MUST 同时提供这四列：`阶段编号`（`#`）、`编号`（`kebab-case` 标识符）、`名称`（中文短语）、`状态`（见状态枚举）、`说明`（一句话交付目标）。阶段编号 MUST 严格连续递增，无跳号或重号。

#### Scenario: All five columns present

- **WHEN** 任意贡献者阅读 `docs/roadmap-15-steps.md`
- **THEN** 每行 MUST 同时包含 `阶段编号`、`编号`、`名称`、`状态`、`说明` 五个字段
- **AND** 阶段编号 MUST 严格递增（`#1`、`#2`、…、`#N`）

### Requirement: Roadmap status enum

路线图文档中的 `状态` 列 MUST 仅使用以下三个枚举值之一：`⬜ 待实施`（change 尚未启动）、`🔨 进行中`（change 已创建但未归档）、`✅ 已归档`（change 已在 `openspec/changes/archive/` 目录下存在）。其他字符串（如 `Done`、`完成`、`finished`）MUST NOT 出现。

#### Scenario: Status uses canonical enum only

- **WHEN** 解析路线图任意行的 `状态` 列
- **THEN** 该值 MUST 严格等于 `⬜ 待实施`、`🔨 进行中` 或 `✅ 已归档` 之一
- **AND** 任何其他取值 MUST 视为文档错误，由归档流程责任人修正

### Requirement: Archive date annotation

所有 `✅ 已归档` 状态的行 MUST 在 `名称` 列或 `说明` 列中以 `（YYYY-MM-DD）` 格式标注归档日期。归档日期 MUST 与 `openspec/changes/archive/<date>-<name>/` 目录前缀中的日期一致。

#### Scenario: Archived rows carry archive date

- **WHEN** 路线图中某行的状态为 `✅ 已归档`
- **THEN** 该行 MUST 包含形如 `（2026-09-27）` 的日期注释
- **AND** 该日期 MUST 与 `openspec/changes/archive/<该日期>-<change 名>/` 目录前缀对齐

#### Scenario: Date mismatch surfaces as drift

- **WHEN** 路线图 `✅ 已归档` 行注释的日期与 `openspec/changes/archive/` 实际目录前缀不一致
- **THEN** 该偏差 MUST 视为路线图文档与现实脱节，必须由下一次 archive 的执行人或专项 cleanup change 修正

### Requirement: Post-archive roadmap update obligation

每次执行 `openspec archive <change-name>` 成功后，archive 流程执行人 MUST 在同一提交或紧随的提交中更新 `docs/roadmap-15-steps.md`：将对应阶段的 `状态` 从 `🔨 进行中` 改为 `✅ 已归档`，并在 `说明` 列追加归档日期注释。本次归档不允许出现"路线图未更新"的中间态。

#### Scenario: Successful archive updates roadmap same commit

- **WHEN** `openspec archive` 命令成功完成某 change 的归档
- **THEN** `docs/roadmap-15-steps.md` 中对应行的状态 MUST 在同一提交或紧随的下一次提交中被改为 `✅ 已归档`
- **AND** 该行的归档日期 MUST 填入与目录前缀一致的 `（YYYY-MM-DD）` 注释

### Requirement: Forward roadmap numbering

路线图文档 MUST 在每个 `✅ 已归档` 行的下方保留至少一个 `🔨 进行中` 或 `⬜ 待实施` 行，或在文档末尾的"说明"小节中显式登记下一阶段的 `编号` 与 `名称` 候选。一旦所有当前登记的阶段全部归档，必须在 `docs/roadmap-15-steps.md` 文件名或文档末尾追加新的待实施阶段条目以承接路线图。

#### Scenario: New pending phases appended after archive

- **WHEN** 路线图中所有登记的阶段都已 `✅ 已归档`
- **THEN** 文档 MUST 在末尾追加新的阶段条目（编号 `#+1`、`#+2` 等），保持路线图可被新贡献者作为规划参考
- **AND** 新条目 MUST 至少包含 `编号` 与 `名称`，`状态` 列 MUST 为 `⬜ 待实施` 或 `🔨 进行中`

### Requirement: Sync verification before commit

在涉及路线图变更的提交中，archive 流程执行人 MUST 在本地核对 `openspec list --specs` 与 `docs/roadmap-15-steps.md` 的对应关系，且核对命令的输出 MUST 在提交说明中可追溯（例如在 commit message 中引用 `openspec list` 输出摘要）。

#### Scenario: Roadmap change accompanied by verification artifact

- **WHEN** 提交修改 `docs/roadmap-15-steps.md` 或创建新的 `openspec/changes/` 目录
- **THEN** 提交说明或合并请求描述 MUST 引用 `openspec list` 的输出（或等价命令）
- **AND** 若修改了归档日期注释，必须同时验证 `openspec/changes/archive/<date>-<name>/` 目录存在

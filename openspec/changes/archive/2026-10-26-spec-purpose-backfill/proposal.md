# Proposal

## Why

有 4 个主 spec 的 `## Purpose` 仍是归档时留下的占位符（`TBD - created by archiving change ...`）。读者打开这些能力时看不到「它是做什么的」，只能从需求标题反推；`openspec validate --specs` 也持续报占位符警告。

## What Changes

- 为 `channel-id-auto-gen`、`media-loop-validation`、`platform-large-node-catalog-aggregate`、`web-realtime-logs` 各写一句真实 Purpose。
- 不改任何 `### Requirement`、场景或实现。

**BREAKING**：无。

## Capabilities

不新增、不修改任何 capability 的需求。`skip_specs: true`：这是主 spec 的 Purpose 文本补全，不是行为变更。

对应路线图编号：#36 `spec-purpose-backfill`。

## Impact

- 只改 `openspec/specs/<capability>/spec.md` 的 `## Purpose` 段。
- 不改代码、配置、API 或数据库。

## Non-goals

- 不重写这些 spec 的需求或场景。
- 不新增 capability。
- 不改动其他已有作者撰写的 Purpose。

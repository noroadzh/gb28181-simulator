# Design

## Context

Purpose 只是一句话说明「这个能力为什么存在」。占位符是归档流程留下的，不是作者的选择。

## Decisions

### Decision 1: 直接编辑主 spec，不走 delta

`openspec` 的规则是：delta 里的 `## Purpose` 只在**创建新能力**时被采用；修改既有能力的 Purpose 必须直接编辑主 spec。因此本 change 用 `skip_specs: true`，避免写一个不会被采用的 delta。

### Decision 2: Purpose 取自现有需求的行为边界

每句都由该 spec 已有需求归纳：通道 ID 自动生成规则、loop 仅对文件源生效、目录聚合、实时日志页。不引入新承诺——Purpose 描述现状，不是路线图。

## Risks / Trade-offs

- **R1 写出超出需求的承诺** → 每句只概括已有需求，评审时逐条对照 `### Requirement`。

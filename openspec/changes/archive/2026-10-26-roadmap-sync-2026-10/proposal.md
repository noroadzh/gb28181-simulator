# Proposal

## Why

路线图文档自 2026-10-10 起未再同步，已与归档现实脱节四处：`media-aggregator-integration`（#17）实际 14/14 完成并归档于 2026-09-29，却仍标 `⬜ 待实施`；「说明」节写的归档目录数 39 与实际 48 不符；2026-10-10 之后归档的 13 个 change 在文档中完全没有登记；文档因此也违反了 `docs-roadmap-sync` 自己的「归档即同步」与「始终保留待实施条目」两条要求。结果是任何人读路线图都会得到「#17 还没做」和「总共只有 39 个 change」两个错误结论。

## What Changes

- **#17 状态修正**：`media-aggregator-integration` 改为 `✅ 已归档`，补归档日期 `（2026-09-29）`。
- **补登记 #21–#33**：把 2026-10-10 之后归档的 13 个 change 按目录名顺序登记为编号阶段行，每行五列齐全、状态 `✅ 已归档`、附归档日期。
- **修正计数**：说明节的「已完成 19 个编号 / 39 个目录」更新为 33 个编号 / 48 个目录。
- **追加前沿候选**：在末尾登记 `#34 channel-control-signalling`（⬜，通道控制面信令实现：PTZ / 录像查询 / 回放 / 对讲 / 抓拍）与 `#35 spec-purpose-backfill`（⬜，4 个主 spec 的 Purpose 占位符）。
- **补一条规则**：`docs-roadmap-sync` 增加「归档目录全覆盖」要求，防止后续跟进型 change 再次只归档不登记。

**BREAKING**：无。

## Capabilities

### Modified Capabilities

- `docs-roadmap-sync`：新增「归档目录全覆盖」要求——每个归档目录必须能在路线图中定位，新增归档若不对应既有编号，必须在同一提交补登记。

## 影响与边界

- 只改 `docs/roadmap-15-steps.md` 与 `docs-roadmap-sync` 的 spec，不改任何 Go 代码、不改 HTTP API、不改 DB。
- 对应的路线图编号：#17（状态修正）、#21–#33（补登记）；前沿候选登记为 #34、#35。

## Non-goals

- 不实现 #34 描述的任何信令能力（PTZ / 录像 / 回放 / 对讲 / 抓拍），那是下一个独立 change。
- 不回溯修改已归档 change 的 proposal/design/tasks。
- 不删除或重排 #1–#20 的任何行（编号与顺序保持原样）。
- 不引入路线图自动生成工具；本次仍是人工维护 + 契约约束。

# Tasks

## 1. 路线图修正

- [x] 1.1 `docs/roadmap-15-steps.md`：把 #17 `media-aggregator-integration` 的状态从 `⬜ 待实施` 改为 `✅ 已归档`，说明列补 `（2026-09-29）`；验收：该行四列齐全且日期与 `openspec/changes/archive/2026-09-29-media-aggregator-integration/` 前缀一致
- [x] 1.2 按目录名顺序追加 #21–#33 十三行，逐行给出 `编号`（目录名去日期前缀）、`名称`（中文短语）、`状态`（`✅ 已归档`）、`说明`（一句话 + 归档日期）；验收：13 个编号与 13 个归档目录一一对应，日期与目录前缀一致
- [x] 1.3 更新「说明」节：编号完成数 20→33，归档目录数 39→48，同步日期改为 2026-10-26；验收：文中不再出现旧计数
- [x] 1.4 在表末追加 `#34 channel-control-signalling` 与 `#35 spec-purpose-backfill`，状态 `⬜ 待实施`，各带一句话范围；验收：`Forward roadmap numbering` 要求的待实施条目存在
- [x] 1.5 校验：写一段一次性脚本比对 `openspec/changes/archive/` 目录名与路线图行内 `编号`+日期，输出零差异；再跑 `openspec validate --specs` 确认主 spec 未受影响；验收：两条命令均通过

# Tasks

## 1. Roadmap document sync

- [x] 1.1 Update `#15 scenario-engine` row in `docs/roadmap-15-steps.md`: change `状态` from `⬜ 待实施` to `✅ 已归档`; append `（2026-09-27）` to the `名称` cell; verify the date matches the `openspec/changes/archive/2026-09-27-scenario-engine/` directory prefix
- [x] 1.2 Append `#16 manscdp-logging-coverage` row with `状态: ⬜ 待实施`; include `编号` and `名称`; verify the row appears in the markdown table with correct column ordering
- [x] 1.3 Append `#17 media-aggregator-integration` row with `状态: ⬜ 待实施`; include `编号` and `名称`; verify the row appears in the markdown table with correct column ordering
- [x] 1.4 Append `#18 logging-user-docs` row with `状态: ⬜ 待实施`; include `编号` and `名称`; verify the row appears in the markdown table with correct column ordering
- [x] 1.5 Update the document footer "说明" section to reflect the new totals (e.g., "已完成 17 个 change，对应 18 个归档 change 目录"); verify the numbers match `ls openspec/changes/archive/ | wc -l` output

## 2. Verification

- [x] 2.2 Run `openspec validate docs-roadmap-sync --strict --type change --no-interactive` and verify exit code 0 with no warnings
- [x] 2.3 Run `openspec show docs-roadmap-sync --json --deltas-only` and verify the delta spec contains 6 requirements with all scenarios intact
- [ ] 2.1 (post-archive) Run `openspec list --specs` after archiving and verify the new `docs-roadmap-sync` capability appears in the output

# Tasks

## 1. 规格用词与过时注释

- [x] 1.1 将主规格 `openspec/specs/device-command-coverage/spec.md` 中布防需求的 `AlarmCmd` 改为 `GuardCmd`（与本 change delta 一致）；验收：全文检索无错误的布防 `AlarmCmd` 表述（Alarm 通知 CmdType 保留）
- [x] 1.2 修正 `internal/app/node_service.go` 等处声称「outbound DeviceControl 未实现」的过时注释；验收：相关注释描述与 `PTZ()` 实际发 MESSAGE 行为一致

## 2. 架构与覆盖文档

- [x] 2.1 重写 `docs/architecture.md` 回放段落：删除「FLV 历史流尚未接入」；写明现行 `/playback/flv?sn=` 与会话语义；验收：文档内无与 #37 矛盾的「未实现」句
- [x] 2.2 新增 `docs/gb28181-coverage.md`（由 `.cursor/plan/gb28181-coverage-analysis.md` 提炼第 7/9 章与附录矩阵 + 级联 MESSAGE≠媒体说明）；验收：文件存在且含 ✅/🟡/❌ 与级联限制段
- [x] 2.3 在 `docs/web-ui-guide.md` 或 Help 相关说明中补一句级联信令/媒体分工（若帮助页静态文案在仓库内）；验收：用户可见文案含该区分

## 3. 路线图承接

- [x] 3.1 更新 `docs/roadmap-15-steps.md`：增加 `#45 docs-sync-gb28181-coverage`（🔨 进行中）、`#46 interop-hardening`（🔨 进行中或 ⬜ 待实施），说明节保留 #47+ 候选；验收：满足 Forward numbering，状态枚举合法
- [x] 3.2 运行 `openspec validate --change docs-sync-gb28181-coverage --strict`（或项目等价校验）并通过；验收：命令退出码 0

## Workflow follow-up

- 实现完成后由用户触发 archive；归档时将 #45 标为 ✅ 并 sync `protocol-coverage-docs` 入主规格
- 建议在 archive 本 change 后再全面 apply `interop-hardening`，或并行时以本 change 的覆盖表为 #46 收尾更新目标

# Spec Delta

## ADDED Requirements

### Requirement: 协议覆盖表与路线图同步

当仓库存在 `docs/gb28181-coverage.md` 时，每次归档会改变国标章节覆盖状态的 change（例如新增 CmdType、媒体路径、安全能力）MUST 在同一提交或紧随提交中更新该覆盖表中对应行的状态与说明；同时 MUST 按既有 Forward roadmap numbering 在 `docs/roadmap-15-steps.md` 保留 `#N+1` 承接行。

#### Scenario: 归档后覆盖表与路线图同时更新

- **WHEN** 归档一个改变协议覆盖面的 change（如新增 DeviceControl 子类型）
- **THEN** `docs/roadmap-15-steps.md` 对应行改为 `✅ 已归档` 并带归档日期
- **AND** `docs/gb28181-coverage.md` 中相关行的状态/说明与新能力一致
- **AND** 若无更多进行中阶段，文档 MUST 仍保留至少一个 `⬜ 待实施` 或 `🔨 进行中` 承接行

#### Scenario: 本阶段登记 #45 与 #46

- **WHEN** 本 change 处于进行中且 `interop-hardening` 已创建
- **THEN** 路线图 MUST 出现 `#45 docs-sync-gb28181-coverage`（本 change）与 `#46 interop-hardening` 两行，状态为 `🔨 进行中` 或归档后的 `✅ 已归档`
- **AND** 说明节可继续列出 #47+ 候选（如 Web UI 平台媒体入口）

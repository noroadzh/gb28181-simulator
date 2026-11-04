## MODIFIED Requirements

### Requirement: Forward roadmap numbering

系统 MUST 在 `docs/roadmap-15-steps.md` 主表登记 `#47` `webui-platform-live-and-recordings` 为已归档（本 change 交付后），并在说明节将下一候选更新为 `#48+`：cascade 可观测性、校时/媒体保活、SIP TLS / SRTP（不再把「Web UI 平台拉流/录像库入口」列为未做首项）。

#### Scenario: #47 行存在

- **WHEN** 本 change 归档
- **THEN** 路线图主表含 #47 且状态为已归档
- **AND** 说明节不再将平台拉流/录像库 UI 列为首个未做候选

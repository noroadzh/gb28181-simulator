# Spec Delta

## ADDED Requirements

### Requirement: 场景管理预览页

场景管理页必须（MUST）以列表形式展示场景包（本期使用本地 mock 数据：名称、描述、状态标签），每项提供"执行"按钮。执行按钮必须（MUST）调用 `POST /v1/scenarios/run`；该端点在本期返回 501 与明确的"由 scenario-engine（#15）提供"提示，前端必须（MUST）将该响应以提示框形式展示给用户，而不是静默失败。

#### Scenario: 场景列表展示

- **WHEN** 用户打开场景管理页
- **THEN** 页面展示至少一个场景卡片，包含名称与状态标签

#### Scenario: 执行未实现的场景

- **WHEN** 用户点击某场景的执行按钮
- **THEN** 前端发出 POST /v1/scenarios/run 请求，收到 501 后以提示框告知"该能力由 #15 scenario-engine 提供"

# Spec Delta: node-abstraction

## ADDED Requirements

### Requirement: platform-large 节点的双半身份

`platform-large` 节点 MUST 在身份矩阵中具有双半性质：

- 默认身份为 UAS：受理下级 REGISTER，应答目录查询，保留下级在线表。
- 当配置中声明 `registration:` 时 MUST 叠加 UAC 身份：完成向上游平台的注册与续期。

`platform-large` 节点类型为 `model.NodeKindPlatformLarge`，与 `platform-small`（`model.NodeKindPlatformSmall`）和 `device`（`model.NodeKindDevice`）并列，MUST NOT 引入新节点类型。

#### Scenario: 未声明 registration 时保持纯 UAS

- **WHEN** platform-large 节点配置中无 `registration:` 段
- **THEN** 节点仅承担 UAS 职责，与路线图第 6 阶段既有契约一致

#### Scenario: 声明 registration 后身份扩展为双半

- **WHEN** platform-large 节点配置中有 `registration:` 段
- **THEN** 节点同时承担 UAS 与 UAC 职责
- **AND** 启动序列为"先 serve / 后 register"，任一失败回滚并进入 `fault`

#### Scenario: platform-large 不实现节点类型扩展

- **WHEN** 本 change 完成
- **THEN** 系统中节点类型集合保持为 `{device, platform-small, platform-large}`，未引入新节点类型
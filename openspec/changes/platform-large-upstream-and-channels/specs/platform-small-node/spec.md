# Spec Delta: platform-small-node

## ADDED Requirements

> 适用范围：新增一条 capability 级别的契约，将 platform-small 已沉淀的"UAS + UAC 双半"逻辑标注为可被 platform-large 复用。本 capability 的既有需求不变。

### Requirement: platform-small 的双半逻辑作为通用模块可被复用

`platform-small` 节点已沉淀的 UAS / UAC 双半启动序列、注册事务、保活与踢线、注销告别逻辑 MUST NOT 因复用而被修改。其函数实现 MUST 以可在 `platform-large` 启动路径上直接调用的形式暴露（`internal/app` 包内导出符号），复用方不重新实现注册流程。

#### Scenario: platform-large 启动时复用 platform-small 的 startPlatformSmall

- **WHEN** `platform-large` 节点声明 `registration:`
- **THEN** 节点启动调用与 `platform-small` 同一函数（`startPlatformSmall`）
- **AND** 双半语义、失败回滚、保活与注销行为与本 capability 一致

#### Scenario: 复用不修改 platform-small 自身行为

- **WHEN** platform-large 复用 platform-small 的函数
- **THEN** platform-small 节点的既有 Scenario 全部保持（"两半同时存在时依次建立"、"只有受理配置时只做上级"、"省略受理配置时仍以默认值受理" 等均不受影响）
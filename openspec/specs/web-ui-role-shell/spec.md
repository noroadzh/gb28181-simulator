# web-ui-role-shell Specification

## Purpose

在同一 Web 壳内用「全部 / 平台 / 设备」视角降低角色混淆，不拆成两个前端产品。

## Requirements

### Requirement: 合壳角色视角

Web UI MUST 提供全部 / 平台 / 设备三种视角切换（可持久化）。平台视角 MUST 保留账号管理与级联中继入口并默认过滤设备节点；设备视角 MUST 保留媒体源入口并默认过滤平台节点；全部视角 MUST 显示完整菜单。MUST NOT 要求拆分进程或仓库。

#### Scenario: 切换平台视角

- **WHEN** 运维选择平台视角
- **THEN** 侧栏强调平台运维入口，节点列表默认仅平台类节点

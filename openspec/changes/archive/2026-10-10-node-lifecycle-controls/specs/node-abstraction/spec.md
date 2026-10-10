# Spec Delta

## ADDED Requirements

### Requirement: 无上游注册的设备节点启动后直接在线

`device` 身份的节点在不携带 `registration:` 配置段时，其 `Start()` 操作 MUST 在绑定信令监听器成功后将节点推进到 `online` 状态——设备此时仅提供被动的 UAS 服务（接收 INVITE、订阅等），没有上游注册事务需要执行，MUST NOT 停留在 `registering` 状态。携带 `registration:` 配置段的 `device` 节点行为 MUST 保持不变：启动后执行注册事务，成功进入 `online`、失败进入 `fault`。

#### Scenario: 无 registration 的设备节点启动

- **WHEN** 一个 `device` 节点的配置不含 `registration:` 段，用户对该节点执行启动
- **THEN** 信令监听器绑定成功后节点状态为 `online`，节点可接收下级 INVITE 等请求

#### Scenario: 有 registration 的设备节点行为不变

- **WHEN** 一个 `device` 节点的配置含 `registration:` 段，用户对该节点执行启动且上级可达
- **THEN** 节点执行注册事务并在成功后进入 `online`，与既有行为一致

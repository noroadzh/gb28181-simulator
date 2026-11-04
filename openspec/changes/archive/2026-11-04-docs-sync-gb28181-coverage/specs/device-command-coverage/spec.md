# Spec Delta

## MODIFIED Requirements

### Requirement: 报警查询与布防撤防

系统 MUST 支持报警查询（`CmdType=Alarm` 的 Query 信封，或实现所采用的等价查询形态）与 `DeviceControl` 中的布防/撤防元素 **`GuardCmd`**（取值含 `SetGuard` / `ResetGuard`）。报警查询 MUST 返回节点已记录的报警快照（按时间窗过滤），布防/撤防 MUST 改变节点的布防状态并在后续报警通知中体现。布防状态 MUST 可查询。

规格与文档 MUST 使用线协议字段名 `GuardCmd`；MUST NOT 再将布防元素写作 `AlarmCmd`（`Alarm` 仅用于报警通知/查询 CmdType）。

#### Scenario: 查询历史报警

- **WHEN** 平台查询某通道在时间窗内的报警
- **THEN** 应答列出该窗口内节点记录的报警条目，按时间升序

#### Scenario: 布防后报警上报

- **WHEN** 节点收到含 `<GuardCmd>SetGuard</GuardCmd>` 的 DeviceControl 并随后触发一次报警
- **THEN** 报警通知照常发送，且布防状态在查询中为 armed

#### Scenario: 撤防

- **WHEN** 下发含 `<GuardCmd>ResetGuard</GuardCmd>` 的 DeviceControl
- **THEN** 节点布防状态变为 disarmed，查询可见

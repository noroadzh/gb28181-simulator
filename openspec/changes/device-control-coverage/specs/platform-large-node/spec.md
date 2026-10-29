# platform-large-node Delta

## ADDED Requirements

### Requirement: 平台转发运维类 MANSCDP 命令

平台节点 MUST 能向下级转发 `DeviceStatus`、`Broadcast`、`AlarmQuery`、`ConfigDownload`、`DeviceUpgrade`、`DeviceReboot`，并把下级的应答回传给请求方；下级不在线时 MUST 以可辨识失败应答（`Result=ERROR` 或对应状态码），MUST NOT 静默超时。平台自身作为目标时 MUST 直接应答。

#### Scenario: 转发到在线下级

- **WHEN** 上级向平台查询某在线下级的设备状态
- **THEN** 平台把查询转给下级并回传其应答

#### Scenario: 下级不在线

- **WHEN** 目标下级不在平台的在线表中
- **THEN** 平台以可辨识失败应答，而不是挂起

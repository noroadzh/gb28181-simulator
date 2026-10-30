# device-node Delta

## ADDED Requirements

### Requirement: device 节点应答运维类 MANSCDP 命令

`device` 节点 MUST 应答 `DeviceStatus`、`Broadcast`、报警查询（`Alarm` + Query）、`ConfigDownload`、`DeviceUpgrade`、`DeviceControl/TeleBoot` 六类命令，应答形态与 `device-command-coverage` 中的定义一致。收到 `Broadcast` 后设备 MUST 主动向发起方发起音频 INVITE（这与既有的「设备作为 UAS 应答音频 INVITE」是不同方向，两者并存）。既有的目录、心跳、控制、录像查询行为 MUST 保持不变。

#### Scenario: 设备应答六类命令

- **WHEN** 平台依次发送 DeviceStatus、Alarm 查询、ConfigDownload、DeviceUpgrade、DeviceControl/TeleBoot
- **THEN** 每一类都收到 200 与对应应答体，且不影响节点状态

#### Scenario: 广播触发设备主动 INVITE

- **WHEN** 设备收到 Broadcast
- **THEN** 设备先回 ack，再向发起方发送音频 INVITE

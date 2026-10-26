# Spec Delta

## ADDED Requirements

### Requirement: 通道控制操作的能力缺失语义

当节点不是 `device` 身份、没有在线下级设备、或下发失败时，PTZ / 录像查询 / 回放控制 MUST 返回可辨识的失败：能力缺失 `501 Not Implemented`，下级不可达 `502 Bad Gateway`，附属超时 `504 Server Time-out`。MUST NOT 以 200 空操作掩盖未实现的路径，响应体 MUST 说明原因。

#### Scenario: 非 device 节点执行 PTZ

- **WHEN** 对 `platform-large` 节点调用 `POST /channels/:ch/ptz`
- **THEN** 返回 501，响应体说明"节点不是 device"

#### Scenario: 下级设备不可达

- **WHEN** device 节点存在但信令地址不可达，调用 PTZ 或录像查询
- **THEN** 返回 502，响应体含下级 deviceID 与原因

#### Scenario: 正常下发

- **WHEN** device 节点在线且可达，调用 PTZ
- **THEN** 返回 200，DeviceControl MANSCDP XML 已通过 SIP MESSAGE 发出

### Requirement: 回放会话生命周期

同一通道的重复 `play` MUST NOT 泄漏会话：要么复用既有会话，要么先终止旧会话再建新的。`teardown` MUST 释放该会话占用的端口与上游对话；会话超过约定时长无控制命令 MUST 自动回收。

#### Scenario: 重复 play 只留一个会话

- **WHEN** 对同一通道连续两次 `play`
- **THEN** 服务端只保留一个活动回放会话，且第一次占用的资源已释放

#### Scenario: teardown 释放资源

- **WHEN** `play` 后调用 `teardown`
- **THEN** 返回 200，回放端口可立即重新绑定，上游对话收到 BYE

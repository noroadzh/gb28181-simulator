# device-command-coverage Specification

## Purpose
补齐六个常用 MANSCDP 命令的双向行为：设备状态查询、语音广播、报警查询与布防撤防、设备配置、升级/重启、文件下载，使模拟设备与模拟平台在真实对接中不因缺命令被判为不完整。

## Requirements

### Requirement: 设备状态查询与应答

系统 MUST 支持 `CmdType=DeviceStatus` 查询与应答。设备节点收到查询 MUST 以 200 OK + `DeviceStatus` 应答，包含设备 ID、结果（OK/ERROR）与在线状态；平台节点收到指向自身或下级的查询时 MUST 从在线设备表或向下级转发取得结果并回传。

#### Scenario: 设备应答状态查询

- **WHEN** 平台向设备发送 `DeviceStatus` 查询
- **THEN** 设备回 200，body 为 `DeviceStatus` 应答，`Result=OK` 且在线状态为 ON

#### Scenario: 平台转发状态查询

- **WHEN** 上级向平台查询某下级设备的状态且该下级在线
- **THEN** 平台回 200，应答中的设备 ID 为该下级

#### Scenario: 查询未知设备

- **WHEN** 查询的设备不在平台的在线表中
- **THEN** 应答 `Result=ERROR`，而不是静默丢弃

### Requirement: 语音广播

系统 MUST 支持 `CmdType=Broadcast`。平台向设备下发广播 MUST 以 200 OK + `Broadcast` ack（`Result=OK`）应答；设备收到广播后 MUST 按国标向发起方发起音频 INVITE，平台 MUST 以 200 + SDP 应答并把该音频接入与对讲相同的 PCMU 通道。未启用广播或目标不在线时 MUST 以 `Result=ERROR` 应答。

#### Scenario: 下发广播

- **WHEN** 平台向设备下发 `Broadcast`
- **THEN** 设备回 200 + `Result=OK`，并向平台发起音频 INVITE

#### Scenario: 广播音频到达平台

- **WHEN** 广播 INVITE 建立后设备发送 PCMU 音频
- **THEN** 平台在广播对话的接收端口上收到该音频
- **AND** 接收的音频不进入对讲的 WebSocket 桥（该桥只服务平台发起的对讲会话）

#### Scenario: 目标不可用

- **WHEN** 广播目标设备不在线
- **THEN** 平台应答 `Result=ERROR`

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

### Requirement: 设备配置查询与下发

系统 MUST 支持 `CmdType=ConfigDownload` 的查询与下发。查询 MUST 以 `ConfigDownload` 应答携带基础配置（设备名称、厂商、通道数、心跳间隔等）；下发 MUST 返回 `Result=OK` 并让可变的配置项（如心跳间隔）在节点上生效。不支持的配置类型 MUST 返回 `Result=ERROR`。

#### Scenario: 查询基础配置

- **WHEN** 平台查询设备的基础配置
- **THEN** 应答包含设备名称、厂商与通道数

#### Scenario: 下发心跳间隔

- **WHEN** 平台下发新的心跳间隔
- **THEN** 设备应答 `Result=OK`，新值记录在节点上
- **AND** 新间隔在节点下一次启动（重新注册）后生效——运行中的保活循环不被热替换

#### Scenario: 不支持的配置类型

- **WHEN** 平台查询一个本模拟器不支持的配置类型
- **THEN** 应答 `Result=ERROR`

### Requirement: 升级与重启

系统 MUST 支持升级与重启的下发，命令形态按 GB/T 28181：升级为 `CmdType=DeviceUpgrade`（携带 `Firmware`），重启为 `DeviceControl` 中的 `TeleBoot` 元素。两者 MUST 以 200 OK 应答（升级带 `Result=OK` 的应答体），并 MUST NOT 中断节点的信令与媒体：升级只记录目标版本，重启只记录发生时间。对不在线的目标 MUST 以可辨识失败应答。

#### Scenario: 下发升级

- **WHEN** 平台下发 `DeviceUpgrade` 到某版本
- **THEN** 设备回 `Result=OK`，节点保持 online 并记录目标版本

#### Scenario: 下发重启

- **WHEN** 平台下发 `DeviceControl/TeleBoot`
- **THEN** 设备回 200 OK，节点保持 online 并记录重启时间

### Requirement: 文件下载

系统 MUST 支持 `s=Download` 的 INVITE。设备收到下载请求 MUST 以 200 + SDP（`a=sendonly`）应答，并按请求的时间窗以 PS/RTP（UDP 或附录 D TCP）发送文件流；无匹配录像 MUST 返回 404。平台作为请求方时 MUST 能把收到的流写入录像库目录。

#### Scenario: 下载录像文件

- **WHEN** 平台对设备发起 `s=Download` 且设备有该窗口的录像
- **THEN** 设备回 200 + SDP 并发送 PS/RTP，平台侧收到文件流

#### Scenario: 无匹配录像

- **WHEN** 下载请求的时间窗没有对应录像
- **THEN** 设备回 404，不发送媒体

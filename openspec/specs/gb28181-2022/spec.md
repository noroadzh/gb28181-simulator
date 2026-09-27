# gb28181-2022 规范

## Purpose

平台节点（platform-large / platform-small）实现 GB/T 28181-2022 版本协商与 2022 增量能力，同时为老对端保留字节级一致的 2016 行为。本 capability 约束节点如何依据对端协商的 GB 版本选择响应字段与线上行为。

## Requirements

### Requirement: X-GB-Ver 版本协商语义

收到携带 `X-GB-Ver` 头的 REGISTER 的平台节点必须（MUST）将声明版本记录在下游设备注册状态中，并必须（MUST）在其 `200 OK` 回显协商版本。不含该头的 REGISTER 必须（MUST）按 2016 对端处理：不记录版本、不回传 `X-GB-Ver`。

#### Scenario: 2022 设备注册并收到版本回显

- **WHEN** 设备发送携带 `X-GB-Ver: 2022` 与合法凭据的 REGISTER
- **THEN** 平台应答的 `200 OK` 携带 `X-GB-Ver: 2022`
- **AND** 下游设备记录暴露 `GBVersion == "2022"`

#### Scenario: 2016 设备无版本协商注册

- **WHEN** 设备发送不含 `X-GB-Ver` 头的 REGISTER
- **THEN** `200 OK` 不携带 `X-GB-Ver` 头
- **AND** 下游设备记录暴露的 `GBVersion` 为空

#### Scenario: 重注册更新记录的版本

- **WHEN** 设备首次以 `X-GB-Ver: 2016` 注册，之后以 `X-GB-Ver: 2022` 重注册
- **THEN** 第二次注册后存储的设备记录反映 `GBVersion == "2022"`

### Requirement: 版本门控行为

仅限 2022 的线上行为（额外响应字段、2022 命令回复、SDP 能力模块）必须（MUST）只对记录版本为 `2022` 的对端发出。2016 对端必须（MUST）收到与 2022 之前字节一致的响应。

#### Scenario: 2016 对端永远不会看到 2022 字段

- **WHEN** 未携带 `X-GB-Ver: 2022` 注册的设备被查询 DeviceStatus
- **THEN** 响应不含 storage-card 或其他 2022-only 字段

#### Scenario: 2022 对端收到扩展应答

- **WHEN** 携带 `X-GB-Ver: 2022` 注册的设备被查询 DeviceStatus 且 profile 声明了 storage-card 状态
- **THEN** 响应包含 2022 的 storage-card 状态字段

### Requirement: 云台精确位置

以 2022 注册的设备节点在 profile 声明时，位置/状态响应中必须（MUST）包含精确 PTZ 位置值（方位角、俯仰角、变倍，支持亚度精度）。

#### Scenario: 精确位置上报

- **WHEN** 2022 对端请求一台 PTZ 设备的位置，且 profile 声明方位角为 121.4731
- **THEN** 响应包含精确的方位角值

### Requirement: 位姿图附录 O 区段类型

当 profile 配置时，设备节点可能（MAY）在目录项上包含附录 O 区段类型属性。未协商 2022 的对端必须（MUST）忽略该属性。

#### Scenario: 2022 对端看到区段类型

- **WHEN** 2022 对端查询一台通道声明了区段类型 `23`（十字路口）的设备的目录
- **THEN** 目录项携带区段类型属性 `23`

#### Scenario: 2016 对端看不到区段类型

- **WHEN** 2016 对端查询同一目录
- **THEN** 目录项不携带区段类型属性

# platform-large-node-catalog-aggregate Delta

## ADDED Requirements

### Requirement: Catalog 响应聚合下游设备的通道条目

platform-large 节点在收到指向自身的 `CmdType = Catalog` 查询时，除了返回已注册的下游设备记录外，MUST 为每个已注册设备再追加其关联节点 profile 中定义的通道条目。通道条目的 `ParentID` MUST 设为所属设备的 DeviceID，`DeviceID` 为通道自身编号，`SumNum` MUST 等于设备条目数与通道条目数之和。

#### Scenario: 设备带通道注册后的目录展开

- **WHEN** 一个 device 节点（携带 7 条通道）向 platform-large 完成 REGISTER
- **AND** 第三方平台向该 platform-large 发送 `Catalog` 查询
- **THEN** 响应的 `DeviceList` 包含 1 条设备记录 + 7 条通道记录，共 8 条
- **AND** 7 条通道记录的 `ParentID` 均等于设备 DeviceID
- **AND** `SumNum = 8`

#### Scenario: 无通道设备保持原行为

- **WHEN** 一个未配置任何通道的设备完成 REGISTER
- **AND** 第三方发送 `Catalog` 查询
- **THEN** 响应中该设备仅有 1 条设备记录，无子通道条目
- **AND** `SumNum = 1`

#### Scenario: 目录条目按设备-通道层级排序

- **WHEN** 多个设备各自携带多条通道注册到同一 platform-large
- **AND** 第三方发送 `Catalog` 查询
- **THEN** `DeviceList` 按先 `ParentID` 升序、后 `DeviceID` 升序排列
- **AND** 每个设备的所有通道条目紧跟在该设备的设备条目之后

#### Scenario: 通道条目的字段映射

- **WHEN** 一条通道（ID、Name、Status、Manufacturer）被渲染为目录条目
- **THEN** 条目的 `DeviceID = Channel.ID`、`Name = Channel.Name`、`Status = Channel.Status`（ON/OFF）
- **AND** `Manufacturer` 取自节点 vendor，`Model` 为 `"Channel"`
- **AND** `CivilCode` 为 `Channel.ID` 前 6 位行政区域码

#### Scenario: 设备注册但通道 profile 为空时仅返回设备条目

- **WHEN** 一个设备完成 REGISTER 但其在平台 registry 中对应的节点 profile 没有任何通道
- **AND** 第三方发送 `Catalog` 查询
- **THEN** 响应中仅出现该设备的设备记录，不报错、不产生空通道条目
- **AND** `SumNum` 只计入设备条目

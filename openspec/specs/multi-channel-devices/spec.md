# multi-channel-devices Specification

## Purpose
支撑 device 节点拥有多个逻辑通道的多通道数据模型与目录行为。该能力让一台 IPC/NVR/DVR 在国标信令中以"多通道设备"的形式存在，channel ID 严格遵守 20 位编码规则。

## Requirements

### Requirement: 多通道数据模型
device 节点的 `NodeProfile` MUST 支持挂载多个 `Channel`，每个 channel 拥有独立的 id、name、status、parentID。`Channel.id` 必须遵循 20 位国标编码规则。channel 列表变更（增/删/改） MUST 通过 `NodeProfile.WithChannels` 走不可变拷贝路径，不允许就地修改。

#### Scenario: 配置多通道设备
- **WHEN** 节点 profile 配置 `channels` 列表含 3 个 channel
- **THEN** `profile.Channels()` 返回 3 个 channel 副本；每个 channel id 唯一且 20 位

#### Scenario: 拒绝重复 channel id
- **WHEN** 调用 `WithChannels` 时两个 channel id 相同
- **THEN** 返回错误且 profile 不被修改

#### Scenario: 缺省单通道设备
- **WHEN** profile 未配置 channels 列表
- **THEN** `Channels()` 返回 nil；下游按设备 id 作为默认 channel id 处理

### Requirement: 通道级在线状态
`NodeProfile` MUST 提供 `WithChannelStatus(id, status)` 路径，单 channel 状态变更不影响其他 channel。未知 channel id MUST 视为错误。

#### Scenario: 切换单通道状态
- **WHEN** profile 含 channel `34020000001310000001` 且调用 `WithChannelStatus("34020000001310000001", OFF)`
- **THEN** 返回的 profile 中该 channel 状态为 OFF，其余 channel 状态不变

#### Scenario: 未知 channel id 报错
- **WHEN** 调用 `WithChannelStatus` 时 id 不存在
- **THEN** 返回错误而非沉默忽略

### Requirement: 多通道 Catalog 序列化
device 节点的 Catalog notify MUST 包含所有 channel 条目，每个 channel 作为独立 `<Item>` 出现，DeviceID 为 channel id。

#### Scenario: 多通道设备响应 Catalog
- **WHEN** device 节点 profile 含 3 个 channel 且收到 Catalog 查询
- **THEN** 响应的 `DeviceList.Num` 为 3；`Items` 含 3 个条目，每个 DeviceID 与 channel.id 一致

#### Scenario: 单通道设备响应 Catalog
- **WHEN** device 节点未配置 channels
- **THEN** 响应含 1 个 Item，DeviceID 等于节点设备 id（向后兼容）

### Requirement: 单个通道的运行时增删

`NodeProfile` MUST 提供在不可变拷贝路径上新增和删除单个通道的方法：`WithChannelAdded(ch Channel)` 在现有通道列表追加一个通道（重复 id 报错、空 id 报错）；`WithChannelRemoved(id string)` 移除指定 id 的通道（未知 id 报错）。两者 MUST 返回新 profile 副本，不就地修改原 profile。

#### Scenario: 运行时新增单个通道

- **WHEN** profile 含 1 个通道，调用 `WithChannelAdded(NewChannel("34020000001320000099","通道2",""))`
- **THEN** 返回的 profile 的 `Channels()` 含 2 个通道，新通道 id 在末尾

#### Scenario: 新增重复 id 报错

- **WHEN** 调用 `WithChannelAdded` 时传入的 id 与已有通道重复
- **THEN** 返回错误且原 profile 不变

#### Scenario: 运行时删除单个通道

- **WHEN** profile 含 2 个通道，调用 `WithChannelRemoved("<通道1 id>")`
- **THEN** 返回的 profile 的 `Channels()` 仅含 1 个通道

#### Scenario: 删除未知 id 报错

- **WHEN** 调用 `WithChannelRemoved` 时 id 不存在
- **THEN** 返回错误且原 profile 不变

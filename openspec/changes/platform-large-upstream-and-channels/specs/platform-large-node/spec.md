# Spec Delta: platform-large-node

## ADDED Requirements

### Requirement: platform-large 节点可选地反向注册到上游平台

`platform-large` 节点在配置中声明 `registration:` 段时，必须（MUST）将其自身注册到上游平台：完成 REGISTER 事务、应答 401 Digest 挑战并取得授予的有效期，随后按 GB/T 28181 标准周期保活并到期前重注册。任一半（UAS / UAC）失败时节点必须（MUST）进入 `fault`，并回滚另一半。

未声明 `registration:` 时，节点保持纯 UAS 行为，与原有契约一致。

#### Scenario: 仅声明受理时仍以纯 UAS 身份服务

- **WHEN** 平台节点只声明 `platform:` 段而未声明 `registration:`
- **THEN** 节点受理下级注册，不向任何上游注册
- **AND** 节点在线状态与未声明 `registration:` 的原有行为一致

#### Scenario: 声明反向注册后启动即尝试注册到上游

- **WHEN** 平台节点同时声明 `platform:` 与 `registration:`
- **THEN** 节点先在自身 transport 上受理下级注册（UAS 半）
- **AND** 随后向上游平台发送 REGISTER，应答其 401 Digest 挑战并取得授予的有效期（UAC 半）
- **AND** 两半均成功后节点处于 `online`

#### Scenario: 注册失败时节点进入 fault

- **WHEN** 平台节点声明 `registration:` 且上游不可达、注册被拒绝或返回 5xx
- **THEN** 节点进入 `fault`，已建立的 UAS 受理一并回滚，监听端口被释放
- **AND** 失败原因写入节点结果，且不包含密码明文

#### Scenario: 节点停止时向上游发送注销 REGISTER

- **WHEN** 一个已注册到上游的平台节点被停止
- **THEN** 节点向上游发送 `Expires: 0` 的 REGISTER 告别，端口随后被释放

#### Scenario: 平台账号与有效期窗口配置复用 platform-small 的校验规则

- **WHEN** 平台节点声明 `registration:`
- **THEN** `accounts`、`realm`、`min_expires` / `default_expires` / `max_expires` 复用 platform-small 的校验：未知 username 回 403、`response` 不匹配回 403、不下发新挑战；非法配置导致加载失败并指出条目序号与字段名，错误信息不含密码明文

---

### Requirement: platform-large 节点的 Catalog 响应聚合下级与本级通道

收到 MANSCDP `CmdType` 为 `Catalog` 且 `DeviceID` 指向节点自身的 `MESSAGE` 时，platform-large 节点的 `Catalog` 响应必须（MUST）同时包含其在线设备表中的下级与节点自身声明的通道。响应按 device id 升序排列，空表 + 空通道时报告 `SumNum = 0` 且 `DeviceList` 为空。

#### Scenario: 同时聚合下级与通道的目录查询

- **WHEN** 平台收到 `CmdType = Catalog` 的查询
- **AND** 该节点在线表含两条下级、自身声明了一条通道
- **THEN** 响应 `DeviceList` 含三条条目（下级两条 + 通道一条），按 device id 升序排列，`SumNum = 3`

#### Scenario: 仅通道无下级时返回通道目录

- **WHEN** 平台在线表为空、自身声明了一条通道
- **THEN** 响应 `DeviceList` 含该通道条目，`SumNum = 1`

#### Scenario: 仅下级无通道时返回下级目录

- **WHEN** 平台在线表含下级、自身未声明通道
- **THEN** 响应 `DeviceList` 仅含下级条目，行为与原有契约一致

#### Scenario: 通道条目映射为目录项

- **WHEN** 一条通道被渲染为目录条目
- **THEN** 该条目的 `DeviceID` 取通道 `ID()`、`Name` 取通道 `Name`、`Status` 取通道状态（ON / OFF）
- **AND** `CivilCode` 取自通道 `ParentID()`；若 `ParentID()` 为空则回退到节点域前 6 位行政区域码
- **AND** `Manufacturer` 为 `SIMULATOR`，`Model` 为 `VIRTUAL`，`Secrecy` 为 0

#### Scenario: 空目录仍回 200 OK

- **WHEN** 平台在线表为空、自身无通道
- **THEN** 仍回 `200 OK`，body 为 `SumNum = 0` 且 `DeviceList` 为空的 `Catalog` 响应
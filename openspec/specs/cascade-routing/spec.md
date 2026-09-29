# cascade-routing 规范

## Purpose

定义 GB/T 28181 级联路径跟踪与 转发 的行为契约：X-RoutePath 与 X-PreferredPath 头部如何在任意多级拓扑中被注入、转发和选择。

## Requirements

### Requirement: 级联头注入

当节点向级联下游邻居转发 SIP 消息时，MUST 将其自身的 deviceID（或 platformID）前置到 `X-RoutePath` 头部，形成逗号分隔的路径跟踪（例如 `X-RoutePath: 34020000001320000001,34020000001320000002`）。

#### Scenario: 两级级联注入

- **WHEN** platform-large（34020000001320000001）将消息转发给 platform-small（34020000001320000002），后者再转发给 device（34020000001320000003）
- **THEN** device 收到的 SIP 消息携带 `X-RoutePath: 34020000001320000001,34020000001320000002`

### Requirement: 优选路径选择

当节点收到携带 `X-PreferredPath` 头的请求时，MUST 剥离最前（最近）的一段路径并把请求转发给列表中的下一个 deviceID。若没有剩余路径，则不带该头把请求转发给最终目的地。

#### Scenario: 优选路径路由

- **WHEN** 一条消息携带 `X-PreferredPath: 34020000001320000002,34020000001320000003` 到达，当前节点为 34020000001320000001
- **THEN** 当前节点转发给 34020000001320000002，并在转发前从头部剥离自身 ID

### Requirement: 级联转发

当节点收到面向下游邻居的 MANSCDP MESSAGE 且该节点配置了 `CascadeParent` 时，节点 MUST 将消息转发给上级，不改动 body，并保留全部 X-RoutePath 与 X-PreferredPath 头。

#### Scenario: 上级告警传播

- **WHEN** 一台 device 向其本地 platform-small 发送 Alarm notify，且该 platform-small 的 CascadeParent 指向某台 platform-large
- **THEN** platform-small 将 Alarm notify 上游转发给 platform-large，包含原始 Alarm body 与全部级联头

### Requirement: 多节点隔离

模拟器中的每个 Node 实例 MUST 维护自己独立的 SIP 协议栈、端口分配与节点状态。同一主机上的两个节点 MUST NOT 共享 transport 资源，MUST NOT 共享 dialog 状态，且 MUST 能以不同的 DeviceID/PlatformID 寻址。

#### Scenario: 并发多节点运行

- **WHEN** 模拟器同时启动 platform-large（ID 34020000001320000001，端口 5060）与 platform-large（ID 34020000001320000003，端口 5062）
- **THEN** 每个节点独立接收并处理自己的入站 SIP 流量；来自 ID 1 的 REGISTER 绝不出现在 ID 3 上

### Requirement: 环路预防

若到达的消息其 `X-RoutePath` 已包含当前节点自身的 ID，节点 MUST NOT 继续转发，并 MUST 返回 `482 Loop Detected` 响应（或在仿真模式下静默丢弃并记录日志）。

#### Scenario: 环形级联检测

- **WHEN** 节点（34020000001320000002）收到 `X-RoutePath: ...,34020000001320000002,...` 的消息
- **THEN** 该节点检测到环路并停止转发，同时记录该事件

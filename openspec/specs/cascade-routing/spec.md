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

### Requirement: HTTP API 设置级联拓扑

系统 MUST 支持通过 HTTP API `PUT /v1/nodes/:id/cascade` 运行时设置节点的 `cascade_parent` 和 `cascade_children`，调用方在 HTTP 边界传入 ID 字符串，存储层将 `cascade_children` 序列化为 JSON 文本写入 SQLite。设置成功后 MUST 触发 `nodereg.Registry` 拓扑重算（`refreshCascadeTopologyLocked`），使后续 `X-RoutePath` 注入反映新拓扑。

#### Scenario: HTTP API 设置级联

- **WHEN** 节点 A 已存在，调用 `PUT /v1/nodes/A/cascade` 携带 `{"parent": "B", "children": ["C"]}`
- **THEN** DB `nodes` 表 A 行的 `cascade_parent='B'`、`cascade_children='["C"]'`；`Registry` 中 A 的 Profile 已更新为带 B 为父、C 为子

#### Scenario: 环路被拒绝

- **WHEN** 节点 X 的祖先链为 `X→Y→Z`，调用 `PUT /v1/nodes/Z/cascade` 携带 `{"parent": "X"}`
- **THEN** HTTP 409，DB 与 Registry 均未修改

### Requirement: 级联设置错误码

`PUT /v1/nodes/:id/cascade` 在以下情况下 MUST 返回相应错误：`{"error": "cascade would create cycle"}` 返回 409、`{"error": "node cannot be its own parent"}` 返回 400、`{"error": "parent node not found"}` 返回 404、`{"error": "child node not found"}` 返回 404。所有错误响应 MUST 含 `id` 字段标识操作的节点。

#### Scenario: 自引用被拒绝

- **WHEN** 调用 `PUT /v1/nodes/A/cascade` 携带 `{"parent": "A"}`
- **THEN** HTTP 400，响应体 `{"error": "node cannot be its own parent", "id": "A"}`

### Requirement: 级联下行投递

当节点收到 MANSCDP MESSAGE，且其目标 deviceID 是本节点的直接下级时，节点 MUST 把报文投递到该下级的信令地址；MUST NOT 把报文回送给请求方。投递时 body MUST 保持不变，并 MUST 由 cascade handler 逐跳追加 `X-RoutePath`。未配置下级的节点保持现有直接应答行为。

#### Scenario: 按 PreferredPath 投递给直接下级

- **WHEN** platform-large 收到面向 device、且携带 `X-PreferredPath: <small>` 的 MESSAGE，其 `cascade_children` 为 `[platform-small]`
- **THEN** 报文被发往 platform-small 的信令地址，携带 `X-RoutePath: <large>`，且 `X-PreferredPath` 已消费掉 `<small>`

#### Scenario: 中间节点继续转发

- **WHEN** platform-small 收到上一条报文，其 `cascade_children` 含 device
- **THEN** platform-small 把报文发往 device 的地址，携带 `X-RoutePath: <large>,<small>`

#### Scenario: 目标既非下级也无 PreferredPath

- **WHEN** platform-large 收到面向孙节点 device 的 MESSAGE，且报文没有 `X-PreferredPath`
- **THEN** 不产生任何下游报文，节点按本地逻辑处理该 MESSAGE（与引入本 change 之前一致）

#### Scenario: 无下级时行为不变

- **WHEN** 未配置 `cascade_children` 的节点收到 MANSCDP MESSAGE
- **THEN** 节点按既有逻辑直接应答，不产生任何下游报文

### Requirement: 级联响应回传

系统 MUST 以下游应答的 Call-ID 与 CSeq 关联到原始请求，并把该应答回送给最初请求方；回传 MUST 保持状态码与 body 不变。事务超时 MUST 由本节点终结并告知请求方，不得让请求方静默等待。

#### Scenario: 两跳请求的应答回到发起方

- **WHEN** 外部探针向 platform-large 发送 MESSAGE，报文经 platform-small 到达 device，device 应答 200
- **THEN** 探针收到 200，且 body 与 device 的应答一致

#### Scenario: 下游超时返回 504

- **WHEN** 下一跳在事务超时内没有应答
- **THEN** 请求方收到 `504 Server Time-out`，节点记录包含 node_id、next 与超时的日志

### Requirement: 级联投递失败处理

若 cascade handler 判定环路、下一跳无法解析为信令地址或发送失败，节点 MUST 丢弃报文、记录含 node_id 与原因的 `warn` 日志，并向请求方返回可辨识的失败响应，MUST NOT 静默超时。环路 MUST 返回 `482 Loop Detected`，其余失败 MUST 返回 `500 Server Internal Error`。

#### Scenario: 环路被拒绝

- **WHEN** 到达报文的 `X-RoutePath` 已包含本节点 ID
- **THEN** 节点返回 `482 Loop Detected`，不向下游发出任何报文，并记录一条 warn

#### Scenario: 下一跳没有信令地址

- **WHEN** cascade handler 给出 next hop，但该下级 profile 没有可用 addr
- **THEN** 节点返回 `500 Server Internal Error`，日志含缺失地址的下级 ID

#### Scenario: 发送失败

- **WHEN** 向下一跳投递时 transport 返回错误
- **THEN** 节点返回 `500 Server Internal Error`，日志含 send 错误原因

### Requirement: 媒体转发与 MESSAGE 转发并存

级联节点对 MANSCDP `MESSAGE` 的转发规则 MUST 保持不变；新增的媒体中继 MUST 只在 INVITE 上生效。平台 MUST NOT 因媒体中继而改变 `X-RoutePath` / `X-PreferredPath` 在 MESSAGE 上的处理，也 MUST NOT 把 INVITE 交给 MESSAGE 的转发表。

#### Scenario: MESSAGE 转发不受影响

- **WHEN** 平台配置了级联下级并收到指向该下级的 MANSCDP MESSAGE
- **THEN** 报文按既有规则逐跳转发，路径头语义不变

#### Scenario: INVITE 不走 MESSAGE 转发表

- **WHEN** 平台收到指向下级通道的实时视频 INVITE
- **THEN** 平台按 `cascade-media-relay` 建立双对话，而不是把 INVITE 交给 MESSAGE 的转发逻辑

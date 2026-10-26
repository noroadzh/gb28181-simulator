# Spec Delta

## ADDED Requirements

### Requirement: 级联下行投递

当节点收到 MANSCDP MESSAGE，且其目标 deviceID 是本节点的直接下级时，节点 MUST 把报文投递到该下级的信令地址；MUST NOT 把报文回送给请求方。投递时 body MUST 保持不变，并 MUST 由 cascade handler 逐跳追加 `X-RoutePath`。未配置下级的节点保持现有直接应答行为。

#### Scenario: 直接下级收到投递

- **WHEN** platform-large 收到面向 device 的 MESSAGE，其 `cascade_children` 为 `[platform-small]`
- **THEN** 报文被发往 platform-small 的信令地址，携带 `X-RoutePath: <large>`

#### Scenario: 中间节点继续转发

- **WHEN** platform-small 收到上一条报文，其 `cascade_children` 含 device
- **THEN** platform-small 把报文发往 device 的地址，携带 `X-RoutePath: <large>,<small>`

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

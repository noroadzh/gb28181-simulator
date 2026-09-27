# Spec Delta

## ADDED Requirements

### Requirement: 故障 profile 是可选的

未安装故障 profile 的节点必须（MUST）走正常 handler 链处理每个请求，响应与本变更前字节级一致。安装故障 profile 只影响该节点本身：同进程内其他节点保持正常行为。

#### Scenario: 未配置故障的节点行为不变

- **WHEN** 一个节点启动时没有任何故障配置并收到 REGISTER
- **THEN** 响应由正常 handler 链产生，不带有故障引起的状态码、延迟或丢弃

#### Scenario: 故障按节点隔离

- **WHEN** 节点 A 配置了对 MESSAGE 返回 500 的故障 profile，节点 B 未配置，且对端同时向两者发送 MESSAGE
- **THEN** 节点 A 回 500，节点 B 按正常逻辑回 200 OK

### Requirement: 按方法预制错误回复

故障 profile 可以为某个 SIP 方法映射预制回复的状态码与原因（如 REGISTER→403 Forbidden、INVITE→486 Busy Here、MESSAGE→500 Server Internal Error）。当匹配请求到达时，节点必须（MUST）返回预制回复而不是执行正常 handler，且预制回复必须（MUST）是合法的 SIP 响应，携带请求的 Via/Call-ID/From/CSeq，以便对端能匹配事务。

#### Scenario: REGISTER 的预制 403

- **WHEN** 故障 profile 把 REGISTER 映射为 403，且携带正确 Digest 凭据的合法 REGISTER 到达
- **THEN** 节点直接回 403 Forbidden，不查询凭据存储

#### Scenario: 预制回复可匹配事务

- **WHEN** 为入站请求生成预制错误回复
- **THEN** 该回复回显请求的 Via、Call-ID、From（含 tag）与 CSeq 头

### Requirement: 响应延迟

故障 profile 可指定响应延迟：基础时长与可选抖动。profile 生效期间节点发出的每个响应必须（MUST）至少被延迟基础时长；配置抖动时，相邻延迟应有变化。

#### Scenario: 延迟响应超过基础时长

- **WHEN** 故障 profile 设置 2 秒延迟，且一个合法 REGISTER 到达
- **THEN** 200 OK 到达对端的时间不早于请求接收后 2 秒

#### Scenario: 延迟同样作用于预制回复

- **WHEN** REGISTER 同时配置了 canned 403 与 500ms 延迟
- **THEN** canned 403 仍会发送，只是更晚

### Requirement: 概率性静默丢弃

故障 profile 可指定 (0,1] 区间内的丢弃概率。被丢弃的请求必须（MUST）完全不被应答、不进入正常 handler，并且该丢弃必须（MUST）被记录日志并计数。概率为 0 或未配置时不丢弃任何请求。

#### Scenario: 被丢弃的请求没有应答

- **WHEN** 故障 profile 设置丢弃概率 1.0，且任意请求到达
- **THEN** 对端在网络上观察不到任何响应

#### Scenario: 丢弃概率为 0 时无影响

- **WHEN** 故障 profile 设置丢弃概率 0
- **THEN** 每个请求都正常应答

### Requirement: 黑洞方法

故障 profile 可列出需要被黑洞的 SIP 方法。profile 生效期间，匹配请求必须（MUST）被静默忽略（不响应、不执行 handler）。这用于模拟挂死的对等端——例如停止回应 Keepalive 触发查询的设备——并可观察对端自身的超时与重试逻辑。

#### Scenario: 被黑洞的 SUBSCRIBE 永不应答

- **WHEN** 故障 profile 黑洞了 SUBSCRIBE，且一个 SUBSCRIBE 到达
- **THEN** 不发送任何响应，订阅方依靠自身超时

#### Scenario: 未黑洞的方法仍正常

- **WHEN** 仅 SUBSCRIBE 被黑洞，而一个 INVITE 到达
- **THEN** INVITE 由正常 handler 链处理

### Requirement: 不支持方法的应答可配置

节点可配置为对不支持的请求方法回 `501 Not Implemented`。默认必须（MUST）保持变更前行为（静默丢弃 + debug 日志）。

#### Scenario: 未知方法配置 501

- **WHEN** 节点被配置为应答不支持方法，且一个方法为 FOO 的请求到达
- **THEN** 节点回 501 Not Implemented，并回显请求的事务头

#### Scenario: 默认保持静默丢弃

- **WHEN** 节点没有 unsupported-method 配置，且一个方法为 FOO 的请求到达
- **THEN** 不发送响应，并打印一条 debug 日志

### Requirement: 运行时故障 API

HTTP API 必须（MUST）提供 `POST /v1/nodes/:id/faults` 用于安装或替换节点的故障 profile，以及 `DELETE /v1/nodes/:id/faults` 用于清空，遵循既有 action-API 约定。安装 profile 对之后到达的请求生效；清空后立即恢复正常行为。对未知节点操作必须（MUST）返回 404；非法 profile 必须（MUST）返回 400。

#### Scenario: 运行时安装后清除

- **WHEN** 客户端 POST 一个把 REGISTER 映射为 403 的 profile，随后一个 REGISTER 到达；之后客户端 DELETE profile，另一个 REGISTER 到达
- **THEN** 第一个 REGISTER 回 403，第二个由正常 handler 应答

#### Scenario: 非法 profile 被拒绝

- **WHEN** 客户端 POST 一个丢弃概率为 1.5 或状态码为未知值 99 的 profile
- **THEN** API 返回 400，且节点行为未被改变

### Requirement: 异常事件统计

每次注入的故障——预制回复、延迟回复、丢弃请求、黑洞请求——都必须（MUST）按节点、按动作类型计数，并以 info 级别记录（带节点 ID、对端、方法、动作），使一次测试运行可以报告产生了多少异常。计数必须（MUST）通过节点详情 API 暴露。

#### Scenario: 计数反映注入的故障

- **WHEN** 某节点的 profile 已产生 3 次 canned 响应与 2 次 drop
- **THEN** 节点详情报告自 profile 安装以来 canned_response=3 且 drop=2

#### Scenario: 清除 profile 会重置其计数

- **WHEN** 故障 profile 被删除
- **THEN** 该 profile 的计数被重置，而正常行为统计不受影响

# Spec Delta: node-abstraction

## MODIFIED Requirements

### Requirement: Multiple nodes coexist in one process with isolated lifecycles


系统 MUST 维护进程级节点注册表（端口 `NodeRegistry`，位于 `internal/domain/port/`），同时持有多个节点。每个节点 MUST 拥有独立的传输监听器、独立生命周期，以及以节点 id 为键的独立日志字段。启动或停止某个节点 MUST NOT 影响其他节点，且注册表 MUST 是并发安全的。

节点后台资源的节点级归属（本 change 新增的不变量）：节点为其对话框（含 INVITE 过期看门狗）发起的所有后台 goroutine MUST 在以下任一条件发生时终止——(a) 定时器自然到期；(b) 对话框被确认或拆除；(c) 所属节点被停止。看门狗注册表 MUST 以节点实例为作用域而非进程级全局，同一进程内的两个节点实例 MUST NOT 互相观测到对方的计时器。节点停止后 MUST 不残留任何看门狗 goroutine。

#### Scenario: 两个节点各自绑定独立端口并共存

- **WHEN** 在同一进程内注册 `bind=127.0.0.1:5060` 与 `bind=127.0.0.1:5061` 的两个节点并同时启动
- **THEN** 两者均启动成功并各自持有独立 列出ener（本 capability 交付的状态为 `Registering`，
  `Online` 需由身份实现推进，见 design D9）；各自 列出ener 独立收发互不串扰；
  `registry.列出()` 返回 2 个节点

#### Scenario: 停止单个节点不影响其他节点

- **WHEN** 停止其中一个节点
- **THEN** 该节点状态变为 `Offline` 且其端口被释放（可立即重新绑定）；
  另一节点的状态与 列出ener 均不受影响，可继续收发

#### Scenario: 注册表并发安全

- **WHEN** 多个 goroutine 并发注册/注销/查找节点
- **THEN** 在 `-race` 下无数据竞争；无重复 id；查找结果与实际注册一致

#### Scenario: 节点停止取消所有 pending 看门狗（本 change 新增）

- **WHEN** 一个 platform-large 节点有 50 个 pending INVITE 对话在等 ACK，节点被停止
- **THEN** 全部 50 个看门狗 goroutine 退出，均不记录 "expired" 日志、不触发对话拆除

#### Scenario: 看门狗注册表按节点隔离（本 change 新增）

- **WHEN** 同一进程内两个 platform-large 节点各有一个 Call-ID 相同的 pending INVITE
- **THEN** 确认其中一个节点的对话不会取消另一节点的看门狗，各自计时器独立到期

#### Scenario: 已确认对话及时释放看门狗（本 change 新增）

- **WHEN** 一个 pending INVITE 的 ACK 在 30s 到期前到达
- **THEN** 看门狗 goroutine 立即退出（不等定时器触发），且计时器被停止

#### Scenario: 重复 id 注册被拒绝

- **WHEN** 以已存在的 id 再次注册
- **THEN** 返回错误且不覆盖既有节点

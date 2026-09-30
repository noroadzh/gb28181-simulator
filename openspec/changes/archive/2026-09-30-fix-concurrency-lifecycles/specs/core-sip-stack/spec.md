# Spec Delta: core-sip-stack

## MODIFIED Requirements

### Requirement: 进程内多传输监听器

系统 MUST 支持在同一进程内绑定多个 SIP 传输监听器，各自绑定独立的本地地址，每个监听器拥有独立的 TCP/UDP socket 和隔离的发送/接收队列。

传输队列的并发关闭契约（本 change 新增的不变量）：队列的 `Close()` MUST 与消息分发及外部注册的事务 handler 并发安全；`Close()` 返回后任何对内部队列的 send MUST NOT panic——与关闭竞争的 send 要么成功投递要么被静默丢弃。队列关闭顺序 MUST 为：先置 closed 标志 → 再 close channel → 最后等待 reader 退出。重复调用 `Close()` MUST 幂等。

#### 场景：同一进程内两个 UDP 监听器

- **WHEN** 上层创建 `Transport(bind=127.0.0.1:5060)` 与 `Transport(bind=127.0.0.1:5061)` 两个实例
- **THEN** 两实例可同时 `Listen()` 且互不干扰；监听端口不同的请求可路由到对应的 listener 实例

#### 场景：监听器关闭后端口立即释放

- **WHEN** 调用 `Transport.Close()`
- **THEN** 原绑定端口可被后续 `Transport(bind=<same>)` 立即重新绑定（无 TIME_WAIT 阻塞）

#### 场景：关闭与事务 handler 并发不 panic（本 change 新增）

- **WHEN** 外部注册的事务 handler 恰好在节点停止、队列被关闭的同一时刻被触发
- **THEN** 不发生 `send on closed channel` panic；竞争中的消息要么投递成功要么被静默丢弃

#### 场景：重复 Close 幂等（本 change 新增）

- **WHEN** 对同一 splitter/transport 队列实例连续调用两次 `Close()`
- **THEN** 第二次调用返回 nil，channel 与 reader 均不再被触碰

# Design

## Context

See `proposal.md` — Why. 现状约束：

- `internal/adapter/cascade.Handler.Forward` 已经能算出 next hop 与需要附加的头，但 `internal/app/acceptor_run.go:routeOutbound` 只取 `extra` 头，丢弃了 `next`。
- `acceptor` 的读循环按方法分派请求，`peer` 是报文来源地址；当前没有「响应」分派路径，响应到达时无人认领。
- 节点 ID → 信令地址的映射已在 `port.NodeRegistry`（profile 的 `Addr()`），无需新端口。
- 本 change 不引入新依赖，不改 DB schema / HTTP API。

## Goals / Non-Goals

**Goals**
- 让 MANSCDP MESSAGE 在两个及以上节点的链路里真正被投递与回传。
- 失败可见：环路、无地址、发送失败、下游超时都变成明确的 SIP 响应 + 日志。

**Non-Goals**
- 不做 SIP 代理级事务状态机（不重写 Via/branch，不做分支级重传）。
- 不中继 INVITE 对话与媒体（见 proposal Non-goals）。
- 不新增配置项；超时先用常量。

## Decisions

### Decision 1: 中继留在 acceptor，不新建 adapter

`acceptor` 已持有传输、读循环与 peer 信息；把中继放这里能复用现成的 `tr.Send`、`withExtraHeaders` 与日志字段（node_id / peer）。备选：新增 `port.CascadeRelay` 适配器——会为了一个只有一处调用的能力引入新端口与装配，收益不足。

### Decision 2: next hop 地址从 NodeRegistry 解析

cascade handler 返回的是 deviceID；用 `registry.Get(ctx, next)` 取 profile 的 `Addr()`。备选：让 `Topology` 直接携带地址——会把传输层细节塞进纯函数的 handler，破坏它「无 socket」的设计。

### Decision 3: 前置自身 Via + (Call-ID, CSeq) 在途表

转发时必须把响应引回中继节点，否则响应会按原始 Via 直接回到发起方，中继表永远等不到它、还会在超时后补发一个 504。因此投递时构造转发请求：

- **Via**：新增一条指向本节点监听地址的 `Via`（新 branch）作为**最顶层**，原始 Via 原样保留在其下；
- **Call-ID / CSeq**：保持原值，作为关联键；
- **X-RoutePath**：由 cascade handler 追加减（复用 `withExtraHeaders`）。

acceptor 维护 `map[callID+cseq]pendingRelay{originalPeer, deadline}`。响应路径：

1. 响应到达且命中 → **剥掉本节点那条顶层 Via**，把余下报文发给 `originalPeer`，删除表项；
2. 未命中（超时后迟到的响应、重传）→ 丢弃并记 debug；
3. deadline 到期 → 本节点构造 `504 Server Time-out` 回给 `originalPeer`。

剥除顶层 Via 是必需的：SIP 客户端按**顶层 Via 的 branch** 匹配事务，留着本节点的 Via 会让发起方匹配不上。

备选：不前置 Via、让响应走原生 Via 回发起方——实现更简单，但中继无法判断下游是否应答，无法兑现「超时返回 504」，排除。完整代理事务（branch 级重传、失败分支）仍不做。

### Decision 4: 响应不参与 cascade handler

环路检测与 `X-RoutePath` 只对**请求**执行；响应按表项直接回送，避免把回程误判成环路。

### Decision 5: 失败语义固定

| 情况 | 响应 |
|---|---|
| handler 判定环路 | `482 Loop Detected` |
| next hop 无可用 addr / 发送失败 | `500 Server Internal Error` |
| 下游超时 | `504 Server Time-out` |

均为立即应答，不留静默超时。备选：静默丢弃（仿真模式）——与 spec「MUST NOT 静默超时」冲突。

### Decision 6: 中继超时用常量 5s

单跳等待 5 秒，随后发 504。备选：复用注册事务超时或加配置项——前者语义不匹配（注册是长事务），后者在需求明确前属于过度设计。

## Risks / Trade-offs

- **R1 同一 Call-ID/CSeq 并发复用** → 表项键为 `callID|cseq`，并带 deadline；冲突时后写覆盖，日志记录。
- **R2 下游重传导致重复转发** → 首个响应到达即删除表项，后续同键响应按「无在途请求」丢弃并记 debug。
- **R3 多跳累计超时超过请求方耐心** → 每跳独立 5s；最坏情况发起方先超时，属预期，日志可定位卡在哪一跳。
- **R4 Via 处理出错会静默破坏回程** → 忘记前置自身 Via 会让响应绕过中继表；回程忘记剥除顶层 Via 会让发起方匹配不到事务。两者都不会报错，只表现为「发起方一直等到超时」。缓解：为「响应确实回到中继节点」和「回给发起方的报文不含本节点 Via」各写一条断言，两跳 e2e 同时覆盖。
- **R5 环路检测依赖 `X-RoutePath`** → 发起方若不携带该头，多跳仍靠 handler 逐跳追加；无头时只能防直接自环。

## Migration Plan

无数据迁移。上线即生效；回滚按「不配置 `cascade_children`」即可恢复旧的直接应答行为，无需回退二进制。

## Open Questions

- 中继超时是否需要按链路长度放大（`5s × 剩余跳数`）？当前按单跳固定值实现，留待实测后再定。

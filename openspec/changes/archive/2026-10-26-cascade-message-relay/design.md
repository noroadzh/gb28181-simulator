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

### Decision 2b: 跨层投递依赖 X-PreferredPath

cascade handler 只会选**直接下级**或上级作为 next hop，它不认识孙节点。因此「大平台 → 小平台 → 设备」这样的多跳链路必须由发起方（或上级平台）在报文里给出 `X-PreferredPath`，节点按该头逐跳消费并投递：每一跳只负责把它交给列表中的下一个邻居。没有 PreferredPath 时，节点对非直接下级的目标不做下行投递，落到本地处理——这是既有行为，不在本 change 改变。

备选：节点维护「哪个子节点通向某个孙节点」的映射——那属于目录聚合（需要先查询下级目录），远超本 change 范围。

### Decision 3: 用 (Call-ID, CSeq) 在途表 + 用原请求重建回程响应

投递时保持 Call-ID / CSeq / From / To / body 不变，只追加 cascade 头。Via 不在这里拼：出站适配器（`internal/adapter/siptransport`）对**请求**一律重新生成 Via，模型层的 Via 不会上线，任何"前置自身 Via"的方案在这一层都不成立。

回程因此不转发下游响应的原始字节，而是**用保存的原始请求重建**：`buildResponse(downstream.status, downstream.reason, entry.request, ...)`。这样响应携带的 Via 正是最初请求方发来的那一条，能匹配上它的事务；中间每一跳都用自己的 entry 重建，逐跳回退。

acceptor 维护 `map[nodeID+callID+cseq]pendingRelay{peer, request, deadline}`。节点维度不能省：一个 Acceptor 服务进程内所有节点，而中继请求的 Call-ID/CSeq 逐跳不变，少了节点键第二跳会覆盖第一跳的登记（两跳 e2e 就是这么暴露出来的）。

1. 响应到达且命中 → 重建响应并发给 `entry.peer`，删除表项；
2. 未命中（超时后迟到、重传）→ 丢弃并记 debug；
3. deadline 到期 → 重建 `504 Server Time-out` 发给 `entry.peer`。

备选：前置自身 Via 并剥除——在本栈上不可行（适配器重生成 Via，且下游按 UDP 源地址回包，不读 Via），已放弃；完整代理事务（branch 级重传）仍不做。

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

### Decision 7: split 传输把 serving 半发出请求的应答送回 serving 半

`platform-small` 的传输是把一个 socket 拆给两个读者（serving / upstream）。原实现把所有**响应**默认投给 upstream 半，这对注册应答是对的，但对节点自己向下游发起的请求（目录查询、级联中继）是错的：没有声明注册的 platform-small 根本不读 upstream 半，应答会被直接丢弃。现在 serving 半每发出一条请求，就按其 Call-ID 登记一次性路由，应答到达时回到 serving 半并自删登记。

## 实现中补齐的既有缺陷

- **无注册的 device 节点不服务**：`Start` 只把这类节点推进到 online，从未调用 `ServeDevice`，于是「被动的 UAS」（`node-abstraction` 早有要求）实际上收不到任何请求——两跳链路的叶子因此不应答。现已在 passive 分支补上。
- **响应只回显第一条 Via**：`buildResponse` 原来只复制第一个 Via，违反 RFC 3261 §8.2.6；多跳请求的应答因此会丢掉后续 Via。现按序复制全部 Via。

## Risks / Trade-offs

- **R1 同一 Call-ID/CSeq 并发复用** → 表项键为 `callID|cseq`，并带 deadline；冲突时后写覆盖，日志记录。
- **R2 下游重传导致重复转发** → 首个响应到达即删除表项，后续同键响应按「无在途请求」丢弃并记 debug。
- **R3 多跳累计超时超过请求方耐心** → 每跳独立 5s；最坏情况发起方先超时，属预期，日志可定位卡在哪一跳。
- **R4 回程重建丢失下游响应头** → 重建只保留状态码、reason、body 与 Content-Type；下游若在响应里带了额外头（如自定义扩展），会被丢弃。缓解：`buildResponse` 复制原始请求的 Via/From/To/Call-ID/CSeq，回程语义完整；额外头的需求出现时再在白名单里加。
- **R5 环路检测依赖 `X-RoutePath`** → 发起方若不携带该头，多跳仍靠 handler 逐跳追加；无头时只能防直接自环。

## Migration Plan

无数据迁移。上线即生效；回滚按「不配置 `cascade_children`」即可恢复旧的直接应答行为，无需回退二进制。

## Open Questions

- 中继超时是否需要按链路长度放大（`5s × 剩余跳数`）？当前按单跳固定值实现，留待实测后再定。

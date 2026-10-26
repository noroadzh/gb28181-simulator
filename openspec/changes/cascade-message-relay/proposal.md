# Proposal

## Why

路线图第 9 阶段 `cascade-and-multi-instance` 归档时，级联只做到「在出站报文上注入 `X-RoutePath`」：`internal/app/acceptor_run.go` 的 `routeOutbound` 丢弃了 cascade handler 给出的下一跳，报文始终发回原 peer。因此同进程内的多级拓扑（大平台 → 小平台 → 设备）无法真正跑通——下游节点永远收不到转发的 MANSCDP 请求，`cascade-routing` 里的「级联转发」只有单跳可验证。归档 change `runtime-node-configuration` 的任务 8.2 正是因此把验收从两跳下调为单跳。

## What Changes

- **下行投递**：节点收到面向其下游子节点的 MANSCDP MESSAGE 时，按 cascade handler 给出的 next hop 把报文真正发到该子节点的信令地址，而不是发回请求方。
- **响应回传**：下游的应答关联回原始请求，并送回最初的请求方，使发起方看到一次完整的请求/响应。
- **头逐跳维护**：复用现有 handler 追加 `X-RoutePath`、按跳消费 `X-PreferredPath`；会话内既有响应路径不得被误判为环路。
- **失败处理**：下一跳地址不可解析、下游无应答或 handler 判定环路时，丢弃并记录明确日志，同时向请求方返回可辨识的错误响应，而不是静默超时。
- **验收升级**：把 `internal/test/e2e/cascade_runtime_test.go` 的单跳断言升级为两跳——device 收到的消息其 `X-RoutePath` 含前两个平台 ID。

**BREAKING**：无。未配置 cascade 的节点行为不变。

## Capabilities

### New Capabilities

无。行为落在既有的级联路由边界内。

### Modified Capabilities

- `cascade-routing`：新增「级联下行投递」「级联响应回传」「级联投递失败处理」三组要求，并把两跳拓扑纳入可验证范围。

## Impact

- `internal/app/acceptor_run.go`：`routeOutbound` 不再丢弃 next hop；新增在途请求表（Call-ID + CSeq → 原始 peer）。
- `internal/app/acceptor_messages.go` / `acceptor_subscribe.go`：应答按在途表回传。
- 可能新增 `internal/app/cascade_relay.go` 与 `internal/app/cascade_relay_test.go`。
- `internal/domain/port/`：解析子节点 ID → 信令地址（复用 `NodeRegistry` 的 profile，不新增端口）。
- `internal/test/e2e/cascade_runtime_test.go`：升级为两跳验收。
- 不改 DB schema、不改 HTTP API、不改 WebUI。

## Non-goals

- 不实现 INVITE 对话级中转：媒体面仍在本地终结，本 change 只覆盖 MANSCDP MESSAGE 及其响应。
- 不做跨进程级联（多进程共享 DB），仍是单进程多节点。
- 不实现 GB/T 28181-2022 增量能力在级联链路上的特殊处理（X-GB-Ver 透传保持现状）。
- 不实现 GB35114（SM2/SM3）在级联链路上的端到端安全透传。
- 不实现附录 H.3 的完整自动路径发现，仅消费报文已携带的 `X-PreferredPath`。
- 不实现级联链路的主动重传/带宽控制，可靠性依赖既有 SIP 事务超时。

# Tasks

## 1. 中继核心（app 层）

- [ ] 1.1 新增 `internal/app/cascade_relay.go`：在途表 `map[callID|cseq]pendingRelay{peer, deadline}`，提供登记、按响应取走、超时清理与重复响应识别；验收：单测覆盖「命中取走」「未命中返回 false」「超时后清理」「同一键第二个响应被丢弃」四个分支，`go test -race ./internal/app/...` 通过
- [ ] 1.2 实现转发报文构造：在原始请求之上前置一条指向本节点监听地址的 `Via`（新 branch），原始 Via 保留在其下，叠加 cascade handler 给出的 `X-RoutePath` / `X-PreferredPath`；验收：**字节级 golden test** 断言转发报文的起始行、两条 Via 的顺序与 branch 不同、`X-RoutePath` 值符合预期
- [ ] 1.3 实现回程报文构造：剥除本节点那条顶层 `Via` 后发给 `originalPeer`；验收：单测断言回送报文不含本节点 Via、Call-ID/CSeq/状态码/body 与下游应答一致

## 2. acceptor 接入

- [ ] 2.1 下行投递：`acceptor` 在 cascade handler 给出 next hop 时，用 `NodeRegistry` 取该节点的 `Addr()` 并 `Send` 到该地址（不再回送原 peer）；验收：单测断言报文的目的地址是 next 节点、且不会同时发出第二个报文
- [ ] 2.2 响应分派：读循环新增响应分支，按在途表把响应回送 `originalPeer`，未命中只记 debug；验收：单测覆盖「命中回送」「未命中不发送」两条路径
- [ ] 2.3 超时终结：在途项超过 5s 未收到响应时，构造 `504 Server Time-out` 回 `originalPeer` 并清理表项；验收：单测用可注入时钟断言 504 与表项清理，且迟到响应被丢弃
- [ ] 2.4 失败语义：handler 判定环路 → `482 Loop Detected` 且不发出下游报文；next hop 无 addr 或发送失败 → `500 Server Internal Error`；三者均记录含 node_id 与原因的 `warn`；验收：三条错误路径各有单测

## 3. 端到端

- [ ] 3.1 升级 `internal/test/e2e/cascade_runtime_test.go` 为两跳验收：外部探针 → platform-large → platform-small → device，device 收到的 MANSCDP 请求其 `X-RoutePath` 含两个平台 ID；探针最终收到 200 且 body 与 device 应答一致；验收：`go test ./internal/test/e2e/...` 通过
- [ ] 3.2 失败路径 e2e：下一跳无可用地址时探针收到 500；`X-RoutePath` 已含自身时收到 482 且无下游报文；验收：两条用例通过，`make test` 通过

## 4. 文档与收尾

- [ ] 4.1 在 `docs/architecture.md` 增补「级联中继」一节：下行投递与响应回传的路径、Via 前置/剥除规则、482/500/504 语义，并注明不中继 INVITE 与媒体；验收：人工审阅，与 spec 一致
- [ ] 4.2 收尾：`openspec validate cascade-message-relay --strict --type change`、`make lint`、`make test` 全部通过；验收：三条命令退出码为 0

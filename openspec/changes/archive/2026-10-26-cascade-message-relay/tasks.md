# Tasks

## 1. 中继核心（app 层）

- [x] 1.1 新增 `internal/app/cascade_relay.go`：在途表 `map[callID|cseq]pendingRelay{peer, deadline}`，提供登记、按响应取走、超时清理与重复响应识别；验收：单测覆盖「命中取走」「未命中返回 false」「超时后清理」「同一键第二个响应被丢弃」四个分支，`go test -race ./internal/app/...` 通过
- [x] 1.2 实现转发报文构造：保持起始行、Call-ID、CSeq、From/To 与 body 不变，追加 cascade handler 给出的 `X-RoutePath` / `X-PreferredPath`（出站 Via 由传输适配器生成，不在模型层拼）；验收：**字节级 golden test** 断言转发报文的起始行、头顺序与 `X-RoutePath` 值
- [x] 1.3 实现回程报文构造：用保存的原始请求 + 下游应答的状态码/reason/body 重建响应后发给 `originalPeer`；验收：**字节级 golden test** 断言回送报文携带原始请求的 Via、Call-ID/CSeq 不变、状态码与 body 取自下游

## 2. acceptor 接入

- [x] 2.1 下行投递：`acceptor` 在 cascade handler 给出 next hop 时，用 `NodeRegistry` 取该节点的 `Addr()` 并 `Send` 到该地址（不再回送原 peer）；验收：单测断言报文的目的地址是 next 节点、且不会同时发出第二个报文
- [x] 2.2 响应分派：读循环新增响应分支，按在途表把响应回送 `originalPeer`，未命中只记 debug；验收：单测覆盖「命中回送」「未命中不发送」两条路径
- [x] 2.3 超时终结：在途项超过 5s 未收到响应时，构造 `504 Server Time-out` 回 `originalPeer` 并清理表项；验收：单测用可注入时钟断言 504 与表项清理，且迟到响应被丢弃
- [x] 2.4 失败语义：handler 判定环路 → `482 Loop Detected` 且不发出下游报文；next hop 无 addr 或发送失败 → `500 Server Internal Error`；三者均记录含 node_id 与原因的 `warn`；验收：三条错误路径各有单测

## 3. 端到端

- [x] 3.1 升级 `internal/test/e2e/cascade_runtime_test.go` 为两跳验收：外部探针向 platform-large 发送带 `X-PreferredPath: <small>` 的 MANSCDP 请求 → platform-large 投递给 platform-small → platform-small 投递给 device；device 收到的请求其 `X-RoutePath` 含两个平台 ID；探针最终收到 200 且 body 与 device 应答一致；验收：`go test ./internal/test/e2e/...` 通过
- [x] 3.2 失败路径 e2e：下一跳无可用地址时探针收到 500；`X-RoutePath` 已含自身时收到 482 且无下游报文；验收：两条用例通过，`make test` 通过

## 4. 文档与收尾

- [x] 4.1 在 `docs/architecture.md` 增补「级联中继」一节：下行投递与响应回传的路径、出站 Via 由适配器生成/回程按原始请求重建、在途表按 (nodeID, Call-ID, CSeq) 记账、482/500/504 语义，并注明不中继 INVITE 与媒体；验收：人工审阅，与 spec 一致
- [x] 4.2 收尾：`openspec validate cascade-message-relay --strict --type change`、`make lint`、`make test` 全部通过；验收：三条命令退出码为 0

# Design

## Context

Change 4 `node-abstraction` 交付了节点骨架：节点是 20 位编码的不可变值对象，生命周期为 6 态显式状态机，进程内多节点各自绑定独立 listener，`NodeService` 只依赖 domain 端口。它刻意没有实现任何协议行为——`internal/app/node_service.go` 的注释写明"registration is driven by the identity implementations in Change 5/6/7"，`nodereg.Lifecycle.Start` 只推进到 `StatusRegistering` 并绑定 listener，`Transport(id)` 是留给身份实现取用 transport 的接缝，`MarkRegistered` / `MarkOnline` 是留给身份实现推进状态的接缝。

因此本 change 不需要重塑架构，只需沿已有接缝填充 device 一侧的注册行为。三条既有约束决定了做法：

1. **app 层不得 import adapter**（`node-abstraction` spec 用 `go list -deps ./internal/app/...` 断言），所以注册编排在 app，报文构造只能用 `model.NewRequest` 这类 domain 类型，Digest 计算必须通过端口注入。
2. **传输层只认 `model.Message`**：`port.SIPTransport{Send(ctx,msg,dst) / Receive(ctx) (msg,peer,err)}`；`Receive` 返回的对端地址可直接回传给 `Send`。
3. **底层是 gosip**：`internal/adapter/sip/builder.go` 的 `BuildRequest` 已自动补齐 Via/Max-Forwards/User-Agent/Content-Length/CSeq，并支持 `WithFrom/WithTo/WithCallID/WithCSeq/WithHeader/...`；`internal/adapter/auth` 已实现 Challenge 生成、Authorization 解析、`Responder.ComputeResponse`（客户端 response 公式）、`HashFunc` 替换点。

两个已暴露的实现缺陷是注册能否成立的前置阻塞，必须在本 change 内修掉：`modelToGosip`（`internal/adapter/siptransport/transport_port.go`）只透传 body 与 Content-Type，调用方塞进 `model.Message` 的 Contact/Expires/Authorization 等头全部丢失（`nodereg/e2e_test.go` 已用注释记录该限制）；`BuildRequest` 把 Via 的 transport 硬编码为 UDP。

动机与范围见 proposal.md，行为契约见 `specs/*/spec.md`。

## Goals / Non-Goals

**Goals:**

- 让一个 device 节点在进程内真实完成一次 GB/T 28181 注册事务，并能被第三方平台校验通过。
- 保持"依赖向内"：注册编排只用 domain 端口；密码学与报文编码留在 adapter。
- 让注册流程可测：app 层用替身测编排与状态推进，adapter 层用真 UDP 回环测字节与完整事务。
- 配置向后兼容：不声明注册参数的节点行为与今天完全一致。

**Non-Goals:**

- 心跳保活、注销/unregister、注册过期重注册与退避重试、目录上报、点播/回放、报警——它们是同一 capability 后续增量的内容。
- platform-large / platform-small 身份（路线图 #6 / #7）。
- GB35114（SM2/SM3）与 2022 版 `X-GB-Ver` 协商；本 change 只支持把已配置的 GB 版本原样写入头。
- 异步注册、后台重连、注册状态持久化到 SQLite。
- TLS 信令（Change 12）、抓包与 Web UI。

## Decisions

### D1. 注册编排放在 app 层，作为独立用例

新增 app 层注册用例（device registrar），只依赖 domain 端口：`SIPTransport`（由 lifecycle 提供）、新增的 `Authorizer`、`Clock` 与日志。`NodeService.Start` 在 `lifecycle.Start` 成功（已到 `registering`、listener 已绑定）之后：若该节点携带注册配置且身份为 device，则执行注册事务；成功依次推进 `registered` → `online`，失败推进 `fault` 并释放 listener。

- **备选 A**：注册逻辑写在 adapter（如 `internal/adapter/device`）→ 拒绝。注册是"节点启动后做什么"的用例，属编排；协议细节才是 adapter 的事，且 adapter 无法推进节点状态。
- **备选 B**：新增 HTTP `POST /v1/nodes/{id}/register` 单独触发 → 用户已选定"启动即注册"（真实设备上电即注册），故注册并入 `/start`；B 的代价是 UI 要两个动作且状态机多一个入口。
- **代价**：`/start` 变成有网络往返的同步调用，最坏耗时等于超时上限。用可配超时（默认 5s）+ 尊重请求 ctx 收敛；异步化留作后续演进，不预先抽象。

### D2. 新增 domain 端口 `Authorizer`（客户端侧 Digest），不复用 `Authenticator`

现有 `port.Authenticator`（`Verify(req, cred)`）与 `port.Challenger`（`Challenge(realm)`）都是**服务端**语义，方向相反。新增：

```go
type Authorizer interface {
    // Authorize 依据服务端挑战与本地凭据，产出可直接放进请求的 Authorization 头。
    Authorize(ch model.Challenge, cred model.Credentials, method, uri string) (model.Header, error)
}
```

adapter 侧在 `internal/adapter/auth` 增加三块能力：解析 `WWW-Authenticate`（新增 `ParseChallenge`，产出 `model.Challenge`）、生成 cnonce 与 nc、组装完整 `Authorization` 头值（复用既有 `Responder.ComputeResponse`）。

- **备选**：在 domain 层直接用 `crypto/md5` 算 → 拒绝。domain 不实现密码学；`HashFunc` 替换点已在 adapter 中，未来 SM3（Change 12）走同一接缝，且现在把算法放错层会迫使未来改 domain。
- **备选**：让 app 直接调用 adapter 的 `ComputeResponse` 自己拼头 → 拒绝。违反 D1 的"app 不 import adapter"，且引号转义/nc 计数这类易错细节不该散在用例里。

### D3. 注册配置是独立可选值对象，不塞进 `NodeProfile`

新增不可变值对象承载注册配置（平台地址、鉴权用户名/密码、请求 expires、transport、可选 GB 版本、可选平台 ID），并承载注册结果（服务端授予的 expires、注册完成时间、平台地址）。零值/`nil` 即"该节点不注册"，从而天然满足"缺省行为不变"。

- **备选**：把 server/password/expires 作为 `NodeProfile` 的散字段 → 拒绝。`NodeProfile` 是身份值对象（id/kind/addr/domain/vendor），注册是可选能力；塞进去会让"没配置注册"只能靠零值散字段表达，语义含糊且破坏既有构造函数校验。

### D4. 注册报文由 app 用 `model.NewRequest` 直接构造，不新增"消息构造端口"

app 可以纯用 domain 类型拼出 REGISTER：Request-URI `sip:<平台ID或host>@<域>`、`From`/`To` 为设备 ID@域、`Contact` 指向节点自身绑定地址、`Expires`、`Call-ID`、`CSeq 1 REGISTER`；`Authorization` 头由 D2 的端口产出后并入。传输层转换时把这些头完整带上（D5）。

- **备选**：再抽一个 `port.MessageBuilder` → 拒绝。多一层端口却只是转发 `model.NewRequest` 的能力，属于"为万一而抽象"。
- **约定**：`server_id` 缺省时 Request-URI 使用平台地址主机部分；GB28181 平台通常要求平台 ID，故配置里显式给出 `server_id` 即可覆盖，不需要额外代码路径。

### D5. 修复 `modelToGosip`：调用方给出的头必须全部上线

改造 `internal/adapter/siptransport/transport_port.go` 的转换：把 `model.Message` 的每个头映射到 builder 的对应能力——`From`/`To`/`Call-ID`/`CSeq` 走 `WithFrom/WithTo/WithCallID/WithCSeq`（调用方给值即覆盖 builder 自动生成的默认，避免双头），`Content-Type` 走 `WithContentType`，其余（Contact / Expires / Authorization / X-GB-Ver / Allow 等）用 `WithHeader` 逐条追加。`Content-Length` 仍由 builder 依 body 计算，不重复追加。

- **备选**：绕过 `model.Message`、在传输层直接收发 gosip 原生消息 → 拒绝。会拆掉 domain 端口抽象、牵动既有全部测试，并把 domain 层从"报文领域模型"降级。
- 该修复同时消除 `nodereg/e2e_test.go` 里为绕开限制而写的注释与断言，相关用例按新行为更新。

### D6. `Via` 的传输协议随实际传输取值

`BuildRequest` 新增 transport 选项；`PortAdapter` 持有内部 transport 的 protocol（udp/tcp），转换时传入，TCP 节点的 `Via` 即为 `SIP/2.0/TCP`。未指定时沿用默认 UDP，既有行为不变。

### D7. 事务循环：最多两轮，按 Call-ID 与对端匹配响应

一次注册事务 = 首包 REGISTER +（若收到 401）带 `Authorization` 的重发。事务内 `Call-ID` 保持不变、`CSeq` 递增、`Via` branch 每包不同；`cnonce` 每事务新生成、`nc` 从 `00000001` 起。等待响应时只接受"Call-ID 匹配且来自目标对端"的报文，其余丢弃并继续等待（不匹配者既不算成功也不算失败）。终态判定：2xx 成功；401 触发重发（第二次仍 401 视为失败）；403/404/5xx 与超时直接失败。

- **备选**：用 SIP 事务层（gosip transaction）管理重传 → 拒绝。会引入"重传/定时器"这类本 change 不承诺的语义，且当前 transport 层是明文收发，无事务状态机；两轮循环已覆盖 GB28181 注册的真实交互。

### D8. 失败时新增 lifecycle 的"失败并释放"路径

`nodereg.Lifecycle` 目前只有 `Start`（推进 registering）与 `Stop`（推进 offline 并释放）。新增一条失败路径：在持有 lifecycle 内部锁的前提下推进 `registering → fault`、释放该节点 listener、以 `node_id` 记录失败阶段与原因，然后返回错误。app 层在任一失败分支调用它。

- **备选**：只推进 `Fault` 不释放端口 → 拒绝。spec 要求失败后端口可立即复用；占着端口会让"修正配置后重启"失败，是最容易被用户撞上的坑。
- **备选**：失败时走 `Stop`（推进 offline）→ 拒绝。`offline` 语义是"正常停止"，与"注册失败"不可混；状态机允许 `registering → fault`，且 `fault → idle → registering` 可再次启动。

### D9. 配置：`nodes[]` 增加可选注册段

`NodeConfig` 增加可选注册子结构（指针/可判空）：`server`（`host:port`，给出即视为要注册）、`server_id`（可选）、`username`（缺省为设备 ID）、`password`、`expires`（缺省 3600）、`transport`（`udp` / `tcp`，缺省 `udp`）、`timeout`（缺省 5s）、`gb_version`（可选）。`Config.ValidateNodes` 扩展：server 缺端口、`expires` 非正、`transport` 非法、给出 server 却无 password、非法 `server_id` 均报错，并指出 `nodes[i].<field>`；错误消息不含密码明文。配置示例同步更新；密码字段已被既有 `log.redact_keys` 覆盖。

### D10. 测试用 UAS 新建在 adapter 层，app 层用替身

- app 层：注册编排测试用进程内替身（脚本化的 `port.SIPTransport` 与 `Authorizer`，写在测试文件里，不新增生产代码），断言状态推进、Call-ID/CSeq 行为、失败回落。
- adapter 层：新增 `internal/adapter/siptest` 提供一个测试用 UAS——收 REGISTER → 回 401 + `WWW-Authenticate` → 校验 `Authorization`（复用现有 `Responder.Verify`）→ 回 200 OK；用真 UDP 回环跑完整事务，断言报文字节与头完整性（含 D5 修复的回归）。
- **备选**：扩展 `sipprobe` 的 `Answer` 模式让它发 401 → 拒绝。`sipprobe` 是诊断 CLI 而非测试库，且它不会校验 `Authorization`，无法证明"密码算对了"。

### D11. 日志与审计沿用既有设施，不新增通道

注册事件用既有 `slog`（`node_id`、`server`、`expires`、`stage` 字段），报文级审计由 `core-sip-stack` 的 wire 事件覆盖，不新增事件类型；`password` / `response` / `nonce` / `cnonce` 走既有 `log.redact_keys` 脱敏，`node_id` 保留以便按节点过滤。

## Risks / Trade-offs

- **同步注册让 `/start` 变慢**（一次往返 + 最坏超时）→ 超时可配、默认 5s，且绑定 ctx；后续若需要可改为异步 + 状态查询，不改变本 change 的对外契约。
- **修复头透传会改变既有断言**：`nodereg/e2e_test.go` 与 `port_adapter_test.go` 中依赖"头被丢弃"的用例需按新行为更新 → 在同一任务内一并更新，并把"头不丢失"写成回归测试。
- **头覆盖语义可能引入双头**（builder 自动生成的 From/To/Call-ID/CSeq 与调用方提供值重复）→ 转换层对这四类头采用"覆盖"而非"追加"，并用字节级断言验证每类头恰好出现一次。
- **第三方平台的 401 方言**：部分平台要求重发沿用同一 Call-ID（RFC 3261 标准做法，本设计采用），少数实现可能不同 → 先用标准；若后续实测不兼容，可在注册配置里加开关，不影响本 change 的 spec。
- **只支持 MD5 / MD5-sess**：`WWW-Authenticate` 声明其它算法时显式报错而非静默降级 → 这是 spec 要求的行为；SM3/GB35114 在 Change 12 经同一 `Authorizer` 端口扩展。
- **失败路径的并发竞态**（注册进行中收到 stop）→ 失败处理在 lifecycle 内持锁推进状态；推进被拒时把错误返回给调用方，不静默吞掉（spec 有对应场景）。

## Migration Plan

1. 修 `modelToGosip` 头透传与 Via transport（adapter 内部，行为向后兼容：此前只是"少了头"，没有调用方依赖丢头这一契约）。
2. 新增 `Authorizer` 端口与其 adapter（纯新增）。
3. 新增注册配置值对象与 `nodes[]` 注册段（缺省为空 → 现有配置无需改动即可继续运行）。
4. 新增 app 层注册用例并接入 `NodeService.Start`（仅对"device + 有注册配置"的节点改变行为）。
5. 更新 HTTP `/start` 的错误语义与 `configs/config.example.yaml`。
6. 补充测试（app 替身、adapter 真 UDP 回环、字节级 golden），更新受 D5 影响的既有断言。

**回滚**：整体回退本 change 即可。没有数据迁移——SQLite 中不保存注册状态，配置文件不含新字段时行为等同于回退后。

## Open Questions

- 无会改变 spec、方案或任务拆分的未决问题。第三方平台 401 方言（同一 Call-ID 之外的实现）与注册重试策略已明确推迟到后续 change，不影响本次交付。

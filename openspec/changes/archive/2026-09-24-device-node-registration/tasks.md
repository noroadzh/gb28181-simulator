# Tasks

> 路线图阶段 5 `device-node`（第一部分）：device 身份注册全流程 —— REGISTER → 401 挑战 → Digest 重发 → 200 OK → `registered` → `online`，失败回落 `fault`。
> 任务粒度 ≤ 2 小时；每任务含明确验收标准；涉及 SIP 字节格式的任务必须含 golden test。
> 关键设计见 `design.md`：D1（注册在 app 层、启动即注册）、D2（新增 `Authorizer` 端口）、D3（注册配置独立值对象）、D5/D6（头透传与 Via transport 修复）、D8（失败释放端口）、D9（配置可选）、D10（测试 UAS）。

## 1. 传输层：修复头透传与 Via transport（D5 / D6）

- [x] 1.1 在 `internal/adapter/sip/builder.go` 新增 `WithContact`、`WithExpires`、`WithTransport`（Via 传输协议）
      构建选项；不传选项时产物与今天逐字节一致
      - 验收：`go test ./internal/adapter/sip` 通过；新增 golden 用例证明未传选项时字节不变
- [x] 1.2 重写 `internal/adapter/siptransport/transport_port.go` 的 `modelToGosip`：`From` / `To` /
      `Call-ID` / `CSeq` 走 `WithFrom` / `WithTo` / `WithCallID` / `WithCSeq`（**覆盖**而非追加，避免双头），
      `Content-Type` 走 `WithContentType`，其余头（Contact / Expires / Authorization / X-GB-Ver / Allow）
      逐条 `WithHeader` 追加；`Content-Length` 仍由 body 推导，不重复追加
      - 验收：golden test —— 构造含 Contact / Expires / Authorization / X-GB-Ver 的 REGISTER，
        对端收到的字节含全部这些头，且 From / To / Call-ID / CSeq / Via 各恰好出现一次
- [x] 1.3 `PortAdapter` 把内部 transport 的 protocol 传给 builder（TCP → `SIP/2.0/TCP`），缺省 UDP
      - 验收：TCP 用例断言 Via 含 `TCP`；UDP 用例断言仍为 `UDP`
- [x] 1.4 更新受影响的既有测试：`nodereg/e2e_test.go`、`port_adapter_test.go` 中依赖"头被丢弃"的
      注释与断言按新行为改写
      - 验收：`go test ./internal/adapter/... ./internal/app/...` 全绿

## 2. Adapter：客户端侧 Digest 能力（D2）

- [x] 2.1 在 `internal/adapter/auth` 新增 `ParseChallenge(headerValue string) (model.Challenge, error)`：
      解析 `realm` / `nonce` / `opaque` / `qop` / `algorithm`，容忍多余空白、方案名大小写、带或不带引号；
      缺 `realm` 或 `nonce` 返回错误（不返回零值结构）
      - 验收：表驱动用例覆盖第三方平台常见变体并全部通过；与 `model.NewChallenge` 往返一致
- [x] 2.2 新增 cnonce 生成（每事务不同、可注入以便测试）与 nc 计数（事务内从 `00000001` 起）
      - 验收：两次生成结果不相等；注入后结果可预测，测试可精确断言
- [x] 2.3 新增 `BuildAuthorization(cred, ch, method, uri, nc, cnonce) (string, error)`：复用既有
      `Responder.ComputeResponse`，按 `qop` 有无分走 RFC 7616 §3.4 / RFC 2617 §3 两种公式，字段引号转义，
      `algorithm` 非 MD5 / MD5-sess 时返回"算法不支持"错误
      - 验收：golden test —— 固定输入下 Authorization 字符串逐字节等于 golden；
        `ParseAuthorization(BuildAuthorization(...))` 往返字段一致；服务端 `Responder.Verify` 判定通过

## 3. Domain 端口与 adapter：`Authorizer`（D2）

- [x] 3.1 在 `internal/domain/port/auth.go` 新增 `Authorizer` 接口：
      `Authorize(ch model.Challenge, cred model.Credentials, method, uri string) (model.Header, error)`
      - 验收：编译通过；`go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...`
        仍只输出 `internal/domain/*`
- [x] 3.2 在 `internal/adapter/auth/port.go` 新增 `AuthorizerAdapter`（cnonce 默认随机、可注入）
      - 验收：`var _ port.Authorizer = (*AuthorizerAdapter)(nil)` 编译断言；单测覆盖
        qop=auth / 无 qop / 不支持算法 / 挑战缺字段四类

## 4. Domain：注册配置与注册结果值对象（D3）

- [x] 4.1 在 `internal/domain/model` 新增不可变注册配置值对象（server、server_id、username、password、
      expires、transport、timeout、gb_version）与注册结果值对象（服务端授予的 expires、注册完成时间、
      平台地址）；零值表示"该节点不注册"
      - 验收：构造校验表驱动测试通过（缺 server、expires 非正、transport 非 udp/tcp、timeout 非正均报错）；
        不可变性与既有值对象一致
- [x] 4.2 `Node` / `NodeProfile` 提供携带注册配置与记录注册结果的路径（`With...` 语义，不破坏既有构造函数）
      - 验收：`go test ./internal/domain/model` 全绿；既有 Node 用例无需改动即通过

## 5. Config：`nodes[]` 注册段与校验（D9）

- [x] 5.1 `internal/platform/config` 的 `NodeConfig` 增加可选注册字段
      （server / server_id / username / password / expires / transport / timeout / gb_version）；
      `expires` 缺省 3600、`transport` 缺省 udp、`timeout` 缺省 5s、`username` 缺省设备 ID
      - 验收：示例配置解析用例通过，缺省值按约定生效
- [x] 5.2 扩展 `ValidateNodes`：server 缺端口、expires 非正、transport 非法、给出 server 却无 password、
      server_id 非法均报错并指出 `nodes[i].<field>`；错误消息不含密码明文
      - 验收：表驱动用例通过；错误消息断言不含明文密码
- [x] 5.3 `configs/config.example.yaml` 增加一个带注册段的 device 节点示例
      - 验收：示例文件可被 `config.Load` 成功加载

## 6. App：device 注册用例（D1 / D4 / D7）

- [x] 6.1 新增注册用例结构，依赖 `SIPTransport`、`Authorizer`、`Clock`、日志；用
      `model.NewRequest` 构造 REGISTER（Request-URI `sip:<server_id|host>@<域>`、From/To 为设备 ID@域、
      Contact 指向节点自身绑定地址、Expires、Call-ID、`CSeq 1 REGISTER`、可选 `X-GB-Ver`）
      - 验收：替身 transport 断言首包字段与头集合；`go list -deps ./internal/app/...` 不含 adapter
- [x] 6.2 事务循环：发送 → 等待响应（仅接受 Call-ID 匹配且来自目标对端的报文，其余丢弃继续等待）→
      401 时解析挑战、生成 Authorization、同 Call-ID 递增 CSeq 重发 → 2xx 成功 / 终态非 2xx 失败 /
      超时失败
      - 验收：替身脚本覆盖"直接 200 OK"、"401 → 200 OK"、"401 → 403"、"超时"、"不匹配响应被忽略"五条路径
- [x] 6.3 成功时返回注册结果：服务端 200 OK 中的 `Expires` 优先于请求值，注册完成时间取自注入的 Clock
      - 验收：服务端回 `Expires=600` 而请求 3600 时结果为 600；Clock 注入用例无需 sleep 即可断言时间

## 7. Adapter：lifecycle 失败路径（D8）

- [x] 7.1 `nodereg.Lifecycle` 新增失败路径：持锁推进 `registering → fault`、释放该节点 listener、
      以 `node_id` 记录失败阶段与原因
      - 验收：单测断言节点变为 `fault` 且其端口可被立即重新绑定；状态推进被拒时返回错误
- [x] 7.2 该路径对未运行（无 listener）的节点幂等安全
      - 验收：对未启动节点调用不 panic，返回明确错误

## 8. App：`NodeService.Start` 接入注册（D1）

- [x] 8.1 `Start` 在 lifecycle 绑定成功后：仅当节点身份为 device 且携带注册配置时执行注册；成功依次
      `MarkRegistered` → `MarkOnline`，失败走 7.1 并返回错误
      - 验收：四类用例 —— 无注册配置（停在 `registering`）、注册成功（`online`）、
        注册失败（`fault` 且端口已释放）、身份非 device（不注册）
- [x] 8.2 扩展 `NewNodeService` 以注入注册用例与 `Authorizer`，保持"只接受端口"的约束；nil 依赖报错
      - 验收：参数校验用例通过；既有装配点（cmd / main）同步更新且可编译

## 9. HTTP：`/start` 语义与状态呈现

- [x] 9.1 `POST /v1/nodes/{id}/start` 在注册失败时返回非 2xx 与 JSON 错误体（含失败阶段与原因，不含凭据）
      - 验收：`internal/interface/http/nodes_test.go` 用例通过；错误体断言不含 password / nonce / response
- [x] 9.2 `GET /v1/nodes` 与 `GET /v1/nodes/{id}` 能呈现 `online` / `fault` 状态
      - 验收：状态字段用例通过

## 10. 测试用 UAS 与端到端验证（D10）

- [x] 10.1 新增 `internal/adapter/siptest`：测试用 UAS —— 收 REGISTER 回 401 + `WWW-Authenticate`
      （复用 `Challenger`），校验 `Authorization`（复用 `Responder.Verify`），通过后回 200 OK
      - 验收：UAS 自身单测通过（正确凭据 → 200，错误凭据 → 403）
- [x] 10.2 真 UDP 回环 e2e：device 节点启动后注册到 UAS，断言收到的 REGISTER 报文 golden
      （首包与 401 重发各一份）、Call-ID 一致、CSeq 递增、最终状态 `online`
      - 验收：`go test ./internal/adapter/... -run Register` 通过；golden 文件字节级比对
- [x] 10.3 两 device 节点并行注册到两个 UAS，断言互不串扰（各自只收到自己事务的 401 / 200）
      - 验收：并发用例通过；`go test -race` 无告警

## 11. 收尾与全量验证

- [x] 11.1 更新 `docs/architecture.md` 与 README 中"注册由 Change 5/6/7 填充""start 只到 registering"
      一类表述，改为现状并说明注册配置方式
      - 验收：文档中不再有与本 change 交付结果矛盾的陈述
- [x] 11.2 全量验证：`go build ./...`、`go vet ./...`、`go test ./...`（含 `-race`）、golangci-lint
      - 验收：全部通过；`go list -deps ./internal/app/...` 仍不含 `internal/adapter/...`

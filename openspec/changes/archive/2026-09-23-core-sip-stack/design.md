# Design

## Context

`gb28181-simulator` 已完成 Change 1 (仓库骨架 + Web 空壳)；本 change 在此之上交付第二阶段——节点无关的 GB/T 28181 SIP/SDP/Digest 底层库。

技术约束（来自 openspec/config.yaml 的 project context，不在此复述）：

- 纯 Go，无 CGO（CGO_ENABLED=0 五平台编译）
- 后续 14 个 change 复用本 change 的 API
- 媒体层（MANSCDP+ XML、PS、RTP、媒体源、级联）一律不在本 change 范围
- 节点身份（device / platform-small / platform-large）一律不在本 change 范围
- 国标安全扩展（GB35114 SM2/SM3）一律不在本 change 范围（仅留 hook）

## Goals / Non-Goals

**Goals:**

- 给出 5 个内部包 + 1 个 CLI 命令的可测试实现（`internal/sip`、`internal/sdp`、`internal/auth`、`internal/siptransport`、`internal/sip/audit`、`cmd/sipprobe`）
- 单元测试 + golden fixture 双轨覆盖，确保字节级一致性
- 与 Change 1 的 `internal/logger` 无侵入对接（仅调用，不改接口）
- 跨平台矩阵构建通过

**Non-Goals:**

- 不实现 UAC/UAS 行为；不实现 REGISTER/INVITE/SUBSCRIBE/MESSAGE 等业务编排
- 不实现 MANSCDP+ XML 报文体（Change 3）
- 不实现媒体层（Change 8）
- 不实现级联路径 / X-RoutePath / X-PreferredPath（Change 9）
- 不实现 GB/T 28181-2022 扩展字段（Change 11）
- 不实现 GB35114 SM2/SM3（Change 12）
- 不实现异常流业务编排与抓包（Change 13）
- 不实现 Web 端对 SIP 流的呈现（Change 14）

## Decisions

### D1. SIP 底层：直接使用 `github.com/ghettovoice/gosip`（emiago 当前 owner）

- **选择**：SIP 消息模型、builder、parser、transport、transaction 直接走 `ghettovoice/gosip`，不重写。
- **理由**：
  - 路线图原文钦定该库；emiago 是当前 maintainer（仓库仍在 emiago 个人名下），go.mod module 路径历史保留为 `github.com/ghettovoice/gosip`
  - 已在 `/tmp/go-1.25.5` 环境用 `CGO_ENABLED=0 go build` 验证可编译
  - 库本身纯 Go；间接依赖（logrus、prefixed-formatter 等）不引入 CGO
  - 库已实现 RFC 3261 的 builder + RFC 2617 §3 Digest 计算，与本 change 的核心需求对齐
- **权衡**：
  - `ghettovoice/gosip` 仍把 `logrus` 当作日志默认通道；本 change 通过 `internal/sip/audit` 包装一个 `Logger interface`，把它的 trace 输出接到 `internal/logger` 的 slog hub 上，绕开 logrus 默认 sink
  - `ghettovoice/gosip` 的 transport 在某些边界场景对 `WS` 协议实现有限，本 change 仅用 `UDP/TCP/TLS`，跳过 `WS/WSS`
- **替代方案**：
  - `sip-stack/govsip` 等较新 fork：生态更小、文档缺、维护者单点；风险更高
  - 自研 SIP 栈：3 周起步；与本 change 范围严重不匹配

### D2. SDP：基于 `github.com/pion/sdp` + 自研 §K 兼容层

- **选择**：在 `internal/sdp` 中：
  1. 解析时**先做行级预处理**——把 `y=...` 与 `f=...` 整行剥离出来单独解析，剩余内容交 `pion/sdp.Unmarshal`
  2. 把剥离结果回填到 `gb.Session` 结构体（`SSRC string`、`MediaOption string`）
  3. marshal 时**先**用 `pion/sdp` 渲染 RFC 4566 部分，**再**在每个 media block 后插入 `y=`/`f=`
- **理由**：
  - `pion/sdp` 是事实标准 RFC 4566 实现，活跃维护
  - `pion/sdp` **不**支持 GB/T 28181 §K 扩展（`y=`/`f=` 会被忽略），需在外层做兼容层
  - 完全兼容且字段不被塞入 `a=` 误名删除
- **权衡**：
  - 性能：每次解析多一次行级扫描，开销可忽略
  - API 复杂度：暴露 `gb.Session` 与 `pion/sdp.SessionDescription` 双层；上层只应使用 `gb.Session`
- **替代方案**：
  - 自研 SDP 解析器：与 SIP 选择 `ghettovoice/gosip` 同样的"不重写已知协议"原则相悖
  - `github.com/gortc/stun`（非 SDP）：错配

### D3. Digest 认证：自研最小 RFC 2617 §3 + qop=auth + RFC 7616 §3.4

- **选择**：`internal/auth` 单独实现 `Challenger`（生成 `WWW-Authenticate`）与 `Responder`（计算/校验 `Authorization`）。
- **理由**：
  - `ghettovoice/gosip` 的 `Authorization` 仅解析头字段，**不**重新计算 response；必须自研校验侧
  - `DefaultAuthorizer.AuthorizeRequest` 把 username/password 直接放进 `Authorization` 字段——我们的需求是"server 用密码校验 client 的 response"，不是 client 计算
  - GB28181 §L.2.1 要求 `qop=auth` + `algorithm=MD5`；后续 Change 12 在同一接口上扩展 `algorithm=SM3`，实现成本最低
- **权衡**：
  - 接口稳定性：`Challenger.Challenge(realm)` 与 `Responder.Verify(req, password)` 两函数保持稳定；Change 12 仅在 `Challenger` 上加一个可选 `Algorithm string` 字段（向后兼容）
  - 算法可插拔：使用 `HashFunc func(string) string` 字段，默认 `MD5`，Change 12 替换为 SM3
- **替代方案**：
  - 复用 `ghettovoice/gosip.DefaultAuthorizer`：API 方向不对（client 侧），且会暴露 username/password 给 SIP 库
  - 引入 `github.com/xinsnake/go-http-digest-auth-client`：HTTP 场景，与 SIP 不匹配

### D4. Transport listener 抽象：`siptransport.Transport` 包装 `gosip/transport`

- **选择**：在 `internal/siptransport` 包定义：
  ```go
  type Transport struct { /* wraps gosip/transport.Layer */ }
  func New(bind string, opts ...Option) (*Transport, error)
  func (t *Transport) Send(req sip.Request, dst string) error
  func (t *Transport) Receive(ctx context.Context) (sip.Message, string, error) // returns (msg, remote-addr, nil)
  func (t *Transport) Close() error
  ```
- **理由**：
  - 节点无关即一个 Node 模型持有一个 `*Transport`；Change 4 直接做 `node.transport = siptransport.New(bind, ...)` 即可
  - `gosip/transport.Layer` 的 `SendRequest`/`SendResponse` 与 SIP 事务（client/server tx）耦合——本 change **不暴露事务**；仅暴露 `Send(已构造好的 sip.Request, 目标地址)`，事务层留给 Change 4+ 按需接入
  - 多实例隔离：`Transport` 实例持有独立的 `*transport.Layer`；`Listen()` 调用内部 `Layer.ListenUDP("udp", addr)` / `ListenTCP("tcp", addr)`
- **权衡**：
  - 第一版不接 `gosip/transaction`；变更需要时由 Change 4 引入 client_tx/server_tx
  - `Receive()` 用一个 goroutine 从 listener 拉包并放入 channel；上游 `Receive(ctx)` 取出。channel 容量默认 64，溢出时丢弃并记 warn（与 Change 1 hub 的"慢订阅者丢消息"行为一致）
- **替代方案**：
  - 直接让 Change 4+ 持有 `*transport.Layer`：耦合度高，Change 4 工作量翻倍
  - 用 `gosip/sip.Server`/`Client`：属于高层 API，携带 UAC/UAS 语义，与"节点无关底层"原则冲突

### D5. 审计日志：`internal/sip/audit` 包 + slog handler 包装

- **选择**：
  - 在 `internal/sip/audit` 定义 `WireEvent struct { Direction, Local, Remote string; Bytes []byte }` 与 `Emitter interface { Emit(WireEvent) }`
  - 在 `internal/siptransport` 的 `Send`/`Receive` 路径上构造 `WireEvent`，调用 `audit.Global()` 发出
  - `audit.Global()` 默认实现把事件转写为 `internal/logger` 的 trace 记录（含 `password`/`response` 字段脱敏）
- **理由**：
  - 与 Change 1 的 `internal/logger` 解耦：本 change 仅依赖"trace 级别日志 + 脱敏"，**不**改 logger 的接口
  - `gosip/transport.Layer` 内置日志是 logrus，无法直接替换；`siptransport` 在外层拦截包字节，自行记录
- **权衡**：
  - 4 KiB 截断：避免超长 body（如 PS RTP 包被错误地放进 SIP 流）撑大日志；上层若需要全量 bytes，应直接读 `msg.Body()` 而不依赖日志
- **替代方案**：
  - 直接给 logrus 加 hook 转写 slog：侵入性强，影响 gosip 升级

### D7. 诊断工具 `cmd/sipprobe`

- **选择**：在 `cmd/sipprobe/main.go` 实现 `flag`-driven 单一职责 CLI：
  - `--bind` 必填，格式 `udp|tcp|tls://host:port`
  - `--send-to` 可选，发送一条预先构造的 INVITE
  - `--expect-status` 可选，等待该状态码
  - `--timeout` 默认 5s
- **理由**：
  - golden 测试与开发者冒烟测试刚需
  - 不引入任何测试框架（不引 testify、不引 ginkgo），仅 stdlib `testing` + golden fixture 文件
- **权衡**：
  - 第一版仅做"等待单个响应"；多个响应的场景留待 Change 13 抓包工具

## Risks / Trade-offs

- **R1**：`ghettovoice/gosip` 上游若升级到带 CGO 的版本 → 本 change 的 `CGO_ENABLED=0` 矩阵会破。**Mitigation**：在 CI `go.mod` 锁定到当前 tag（`v0.0.0-20260919124345-798b72cc95a2`），并加 `// +build` 守卫探针：`go list -m github.com/ghettovoice/gosip` 输出版本号断言
- **R2**：`pion/sdp` 升级可能引入对 `y=`/`f=` 的不一致处理（如错把 `y=` 当作未知名属性的 `a=`）。**Mitigation**：`internal/sdp` 的兼容性测试覆盖一组手工构造的"恶意 SDP"——含 `a=y:12345` 的、缺 `y=` 的、多 `y=` 的，期望行为稳定
- **R3**：自研 Digest 实现可能与某些不规范客户端的 `Authorization` 解析不一致。**Mitigation**：golden fixture 包含 (a) 标准浏览器侧 qop=auth (b) 仅 RFC 2617 §3 不带 qop (c) nc 字段大写/小写三种典型形态
- **R4**：进程内多 listener 时，本机 UDP socket 的 `SO_REUSEADDR` 在 Windows 下语义不同（Windows 不支持端口共享）。**Mitigation**：在 Windows 上禁止"同端口多实例"；文档化此行为
- **R5**：`internal/siptransport.Receive()` 在 channel 满时丢包——上层关心则需保证 channel 容量充足。**Mitigation**：默认容量 64 + 丢包时记 `warn` 级日志（**不**记 `error`，避免误导）
- **R6**：golden fixture 在不同 Go 版本下可能因 `fmt`/strconv 行为差异产生字节差。**Mitigation**：golden 文件按"平台无关"原则手工构造（不依赖运行时序列化），并在 README 标注 fixture 哈希

## Migration Plan

- 无运行时迁移：本 change 是**纯新增**内部包；既有 `internal/logger`、`internal/config`、`internal/storage`、`internal/api` 的对外接口零变化
- 依赖安装：`go get github.com/ghettovoice/gosip github.com/pion/sdp`，`go mod tidy`
- CI：`ci.yml` 不变（`go test ./...` 自动覆盖新包）；`release.yml` 不变
- 回滚：`git revert` 即可；不修改已发布的 API

## Open Questions

（无——所有可能影响 spec/approach 的问题已在 SPEC §13 末尾列出的契约点中固定）
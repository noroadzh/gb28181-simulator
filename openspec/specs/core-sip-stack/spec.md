# core-sip-stack Specification

## Purpose

节点无关的 GB/T 28181 SIP/SDP/Digest 底层库 + 多实例 transport listener + 审计日志 + sipprobe 诊断工具。本 capability 为 Change 4-15（Node 抽象、三种身份、媒体、级联、2022 扩展、GB35114、异常流、Web、场景引擎）提供可被 embed 的协议底座；任何身份、注册、心跳、媒体、级联行为一律不在本 capability 范围内。

## Requirements

### Requirement: SIP message round-trip preserves wire bytes

The system SHALL build and parse SIP request and response messages whose on-the-wire byte
representation matches GB/T 28181 §L.1 (RFC 3261) for the message-start line, header fields,
and SDP body, **without** lossy reformatting. Headers explicitly supplied by the caller MUST
survive every conversion the stack performs, including the hand-off from the domain message
type to the wire encoder: dropping a caller-supplied header (e.g. `Contact`, `Expires`,
`Authorization`) is a defect, not a permitted simplification.

#### Scenario: 构建并解析一条 REGISTER 后字节一致

- **WHEN** 上层传入完整头部（`Via`、`From`、`To`、`Call-ID`、`CSeq`、`Contact`、`Max-Forwards`、`Expires`、`User-Agent`、`Allow`、`Content-Length`）与无 body 的 REGISTER 请求
- **THEN** 经序列化-反序列化循环后，原始 start-line、所有头字段名/值（保留参数顺序与原大小写）与 body 长度保持与构造时一致

#### Scenario: 解析含 SDP body 的 INVITE 后字段完整

- **WHEN** 收到一条 `Content-Type: Application/SDP` 的 INVITE 请求
- **THEN** 解析后 `Body()` 返回的字符串与入参 byte 字节一致，`GetHeader("Content-Type")` 返回 `Application/SDP`，`ContentLength()` 等于入参 body 字节数

#### Scenario: 调用方显式给出的头部经传输层转换后不丢失

- **WHEN** 上层以领域消息类型携带 `Contact`、`Expires`、`Authorization`、`From`、`To`、`Call-ID` 等头部交给传输层发送
- **THEN** 对端收到的报文中这些头部全部存在且取值与调用方给出的一致；不因"重建消息"而被丢弃或替换为自动生成的默认值

#### Scenario: Via 的传输协议与实际传输一致

- **WHEN** 以 TCP 传输发送一条由上层构造的请求
- **THEN** `Via` 头中的传输协议为 `TCP`（而非固定 `UDP`）；未指定传输时沿用默认 UDP，行为不变

### Requirement: GB/T 28181 mandatory headers auto-filled

The system SHALL auto-fill the headers required by GB/T 28181-2016 §L.1 §L.2 when the caller does not provide them: a unique `Via` branch (`z9hG4bK-` + 12 random base32 chars), `Max-Forwards` (default 70), `Content-Length` (when body present), and `User-Agent` (containing the simulator version).

#### Scenario: 未提供 Via 时自动生成唯一分支

- **WHEN** 上层构造请求不提供 `Via`
- **THEN** 序列化结果包含恰好一个 `Via: SIP/2.0/...;branch=z9hG4bK-...` 头，`branch` 参数长度 ≥ 13 字符且与进程内其它并发构造的请求不重复（碰撞概率 < 2^-40）

#### Scenario: 提供 Content-Type 时自动填充 Content-Length

- **WHEN** 构造请求同时提供 body 与 `Content-Type: Application/SDP`
- **THEN** 序列化结果包含 `Content-Length: <n>`，其中 `<n>` 等于 body 字节数；不重复出现上层显式给出的 Content-Length（避免双头）

### Requirement: GB/T 28181-2016 §K SDP parsing

The system SHALL parse SDP bodies that include the GB/T 28181 §K extension lines `y=` (32-bit decimal SSRC) and `f=` (media option: `v` / `AudioTrack` / `VideoTrack` / `AudioVideoTrack` / `Metadata`), exposing them via structured fields without losing the RFC 4566 sections.

#### Scenario: 解析 INVITE 中的 SDP 含 `y=`/`f=` 不丢失

- **WHEN** 收到一条 INVITE 请求，body 为 `v=0...m=video 0 RTP/AVP 96...y=1234567890...f=v...a=rtpmap:96 PS/90000`
- **THEN** 解析后 `SSRC()` 返回字符串 `1234567890`，`MediaOption()` 返回 `v`；同时 RFC 4566 字段 `Origin`/`ConnectionInformation`/`MediaName`/`MediaDescription.Attributes` 全部可访问

#### Scenario: 缺少 `y=`/`f=` 时不报错

- **WHEN** 收到的 SDP body 不含 §K 扩展（纯 RFC 4566）
- **THEN** 解析成功；`SSRC()` 返回空字符串，`MediaOption()` 返回空字符串；上层应能区分"未声明"与"声明为空"

### Requirement: GB/T 28181 §K SDP serialization

The system SHALL marshal an SDP body that includes `y=` and `f=` lines per GB/T 28181-2016 §K.2 ordering: after `m=`/`a=` (the media description block) and before the next `m=`, both lines MUST appear in the order `y=`, then `f=`.

#### Scenario: marshal 含 SSRC 与 MediaOption 的 SDP

- **WHEN** 上层调用 marshal 并传入 SSRC=`1234567890`、MediaOption=`v`
- **THEN** 输出字符串在对应 media description 内按 `m=...` → `a=...` → `y=1234567890` → `f=v` 顺序排列

#### Scenario: 未设 SSRC 时不输出 `y=` 行

- **WHEN** 上层 SSRC 为空
- **THEN** marshal 结果不含空 `y=` 行；`f=` 仍可独立输出

### Requirement: Digest challenge and response (RFC 2617 / RFC 7616 / GB28181)

The system SHALL cover both directions of GB/T 28181 Digest authentication. Server side: produce a `WWW-Authenticate` challenge containing `realm`, `nonce`, `qop=auth`, `algorithm=MD5`, and `opaque` (optional). Client side: parse such a challenge (including one received from a third-party platform), and produce the matching `Authorization` header value — computing `response` per RFC 7616 §3.4 when `qop=auth` is offered (with `nc`, `cnonce`) and per RFC 2617 §3 otherwise, and rendering the complete header with correctly quoted fields. Unsupported `algorithm` values MUST surface an explicit error instead of silently falling back.

#### Scenario: server 生成 challenge

- **WHEN** 上层以 `realm="gb28181"`、nonce 由 16 字节随机 base64 编码触发生成 challenge
- **THEN** 输出包含 `Digest realm="gb28181"`, `nonce="..."`, `qop="auth"`, `algorithm=MD5`；`nonce` 长度 ≥ 22 字符，每次调用与进程内其它调用不重复

#### Scenario: client 正确计算 response（qop=auth）

- **WHEN** 上层以 username/password/realm/nonce/qop=auth/method=REGISTER/uri 触发 response 计算
- **THEN** 输出 `response` 字段等于 `MD5(MD5(username:realm:password) : nonce : nc : cnonce : qop : MD5(method:uri))`（hex 大写），符合 RFC 7616 §3.4

#### Scenario: client 校验 server 返回的 response

- **WHEN** server 收到带 `Authorization` 的请求，并能用客户端持有的密码重新计算
- **THEN** 两者 `response` 字段逐字节相等时返回 `nil`，否则返回 `auth.ErrInvalidResponse` 且**不**回写 `WWW-Authenticate` 重试（GB28181 标准要求 server 拒绝一次）

#### Scenario: username/realm 字段非 ASCII 字节按 UTF-8 规范化

- **WHEN** 客户端送上 `Authorization` 中 `username`/`realm` 字段含合法非 ASCII 字节（例 `username="用户-001"`），且密码与服务端一致
- **THEN** RFC 7616 §3.3 规定的 UTF-8 规范化路径生效：`HA1 = MD5(<规范化 username>:<规范化 realm>:password)` 与对端计算结果逐字节相等 → 返回 `nil`；若 UTF-8 校验不过则返回 `ErrInvalidResponse`
- **AND** GB28181 标准设备 ID 全部 ASCII（不变场景）：UTF-8 路径与原 RFC 2617 路径对相同输入产生相同哈希（向后兼容验证）

#### Scenario: qop 字段缺失时回退 RFC 2617

- **WHEN** 客户端送上 `Authorization` 中**没有** `qop` 字段（旧客户端实现）
- **THEN** `expected = MD5(MD5(username:realm:password) : nonce : MD5(method:uri))`，与请求 `response` 字段逐字节相等时返回 `nil`，否则返回 `ErrInvalidResponse`

#### Scenario: 解析第三方平台下发的 WWW-Authenticate

- **WHEN** 收到 `WWW-Authenticate: Digest realm="3402000000", nonce="...", qop="auth", algorithm=MD5, opaque="..."`
- **THEN** 解析结果给出 realm、nonce、opaque、qop、algorithm 各字段，与原文逐字段相等；带引号与不带引号的取值、字段间多余空白、大小写不同的 `Digest` 方案名均可正确解析

#### Scenario: 挑战缺少必需字段时报错

- **WHEN** `WWW-Authenticate` 缺少 `realm` 或 `nonce`，或其值无法解析
- **THEN** 返回错误并指出缺失/非法的字段，不返回零值结构让上层静默发出错误凭据

#### Scenario: 组装完整的 Authorization 头

- **WHEN** 上层以凭据、方法、请求 URI 与已解析的挑战触发 Authorization 生成
- **THEN** 输出形如 `Digest username="...", realm="...", nonce="...", uri="...", response="...", algorithm=MD5, qop=auth, nc=00000001, cnonce="..."` 的完整头值
- **AND** `nc` 与 `cnonce` 由实现生成（`cnonce` 每次事务不同），`username` / `realm` / `nonce` / `uri` / `opaque` 中的特殊字符被引号包裹并转义
- **AND** 该头值可被本 capability 的解析路径反向解析且字段一致

#### Scenario: 挑战声明不支持的算法时报错

- **WHEN** `WWW-Authenticate` 的 `algorithm` 为 MD5 / MD5-sess 之外的值
- **THEN** 生成 Authorization 返回"算法不支持"的错误，不静默按 MD5 计算

### Requirement: Multiple in-process transport listeners

The system SHALL support binding more than one SIP transport listener in the same process on independent local addresses, each with its own TCP/UDP socket and isolated send/receive queue.

#### Scenario: 同一进程内两个 UDP listener

- **WHEN** 上层创建 `Transport(bind=127.0.0.1:5060)` 与 `Transport(bind=127.0.0.1:5061)` 两个实例
- **THEN** 两实例可同时 `Listen()` 且互不干扰；监听端口不同的请求可路由到对应的 listener 实例

#### Scenario: listener 关闭后端口立即释放

- **WHEN** 调用 `Transport.Close()`
- **THEN** 原绑定端口可被后续 `Transport(bind=<same>)` 立即重新绑定（无 TIME_WAIT 阻塞）

### Requirement: SIP transport exposes peer and destination addresses

The system SHALL make remote endpoints explicit: sending a message SHALL accept a destination
address, and receiving SHALL report the address the message arrived from. Addresses are
`host:port` strings (optionally prefixed by a transport scheme). A transport that cannot
determine the peer address MUST surface an explicit error rather than returning an empty string.

#### Scenario: Send 使用显式目标地址

- **WHEN** 调用传输层发送接口并传入目标地址 `127.0.0.1:5060`
- **THEN** 报文被发往该地址；不再依赖从报文 URI 反推目标；返回值仅表达传输层错误

#### Scenario: Receive 返回对端地址

- **WHEN** 传输层收到一条来自 `127.0.0.1:5061` 的报文
- **THEN** 接收结果同时给出报文与该对端地址 `127.0.0.1:5061`；UDP 与 TCP 两种传输均如此

#### Scenario: 对端地址可用于直接回包

- **WHEN** 以接收到的对端地址作为目标发回报文
- **THEN** 报文到达原发送方，无需额外配置路由

#### Scenario: 无法判定地址时显式报错

- **WHEN** 传输实现无法确定对端地址
- **THEN** 返回错误（而非返回空地址让调用方静默发往错误目标）

### Requirement: SIP audit log produces wire-byte events

The system SHALL emit a structured log record for every received and transmitted SIP message, containing the direction (`rx`/`tx`), the transport endpoint (local/remote address), and the raw bytes (clipped to 4 KiB) trace-level; sensitive Digest credentials in headers SHALL be redacted.

#### Scenario: 接收报文产生一条 rx trace 记录

- **WHEN** listener 收到一条 INVITE
- **THEN** `internal/logger` 在 trace 级别写入 `msg="sip rx"`、包含 `local`、`remote`、`bytes`（原文）字段；`Authorization` 头中的 `response` 字段若出现在日志中显示为 `***REDACTED***`

#### Scenario: 发送报文产生一条 tx trace 记录

- **WHEN** 上层调用 `Transport.Send(req, dst)` 成功
- **THEN** 写入 `msg="sip tx"`，包含 `local`、`remote`、`bytes` 字段

### Requirement: Diagnostic CLI `sipprobe` for golden tests

The system SHALL provide a `sipprobe` subcommand capable of binding a UDP/TCP listener and sending a single SIP message, then printing the first received response to stdout, used by CI golden tests and developer smoke checks. In receive-only mode it SHALL additionally be able to answer inbound SIP requests with a 200 OK response when invoked with `--answer`, so that two `sipprobe` processes can complete a request/response exchange without any third process. Answering SHALL be opt-in: without `--answer` the observable behaviour is unchanged.

#### Scenario: sipprobe 接收一条 INVITE 后打印 200 OK

- **WHEN** 执行 `gb28181-simulator sipprobe --bind udp:127.0.0.1:5060 --expect-status 200` 并由外部 client 发送 INVITE
- **THEN** sipprobe 等待 ≤ 5 秒后退出 0，并将 `Response.StatusCode()` 与 `Response.StartLine()` 写入 stdout 一行（TSV 形式）

#### Scenario: sipprobe 超时退出非零

- **WHEN** 5 秒内未收到响应
- **THEN** sipprobe 退出码为 2，stderr 输出 `timeout waiting for status=200`，stdout 为空

#### Scenario: 以 --answer 回应入站请求

- **WHEN** 执行 `gb28181-simulator sipprobe --bind udp://127.0.0.1:15060 --answer` 并收到一条入站 `sip.Request`
- **THEN** sipprobe 向该请求的对端地址回送一条 200 OK（`sip.NewResponseFromRequest`），请求方收到后退出 0

#### Scenario: 两个 sipprobe 互发 INVITE/200 OK

- **WHEN** 进程 A 执行 `sipprobe --bind udp://127.0.0.1:15060 --answer`，
  进程 B 执行 `sipprobe --bind udp://127.0.0.1:15061 --send-to udp://127.0.0.1:15060 --expect-status 200`
- **THEN** B 收到 200 OK 并退出 0；A 回应后退出 0；`scripts/smoke-sip.sh` 的原始断言通过

### Requirement: CGO-free build on all supported platforms

The system SHALL compile and pass `CGO_ENABLED=0 go build ./...` on Linux amd64/arm64, macOS amd64/arm64, and Windows amd64, with no new CGO dependency introduced by this change.

#### Scenario: 跨平台无 CGO 编译成功

- **WHEN** 在五平台分别执行 `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build ./cmd/sipprobe`
- **THEN** 产物为静态链接二进制，无 `cgo` 警告

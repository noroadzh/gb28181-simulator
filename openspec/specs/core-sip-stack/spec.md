# core-sip-stack Specification

## Purpose

节点无关的 GB/T 28181 SIP/SDP/Digest 底层库 + 多实例 transport listener + 审计日志 + sipprobe 诊断工具。本 capability 为 Change 4-15（Node 抽象、三种身份、媒体、级联、2022 扩展、GB35114、异常流、Web、场景引擎）提供可被 embed 的协议底座；任何身份、注册、心跳、媒体、级联行为一律不在本 capability 范围内。

## Requirements

### Requirement: SIP message round-trip preserves wire bytes

The system SHALL build and parse SIP request and response messages whose on-the-wire byte representation matches GB/T 28181 §L.1 (RFC 3261) for the message-start line, header fields, and SDP body, **without** lossy reformatting.

#### Scenario: 构建并解析一条 REGISTER 后字节一致

- **WHEN** 上层传入完整头部（`Via`、`From`、`To`、`Call-ID`、`CSeq`、`Contact`、`Max-Forwards`、`Expires`、`User-Agent`、`Allow`、`Content-Length`）与无 body 的 REGISTER 请求
- **THEN** 经序列化-反序列化循环后，原始 start-line、所有头字段名/值（保留参数顺序与原大小写）与 body 长度保持与构造时一致

#### Scenario: 解析含 SDP body 的 INVITE 后字段完整

- **WHEN** 收到一条 `Content-Type: Application/SDP` 的 INVITE 请求
- **THEN** 解析后 `Body()` 返回的字符串与入参 byte 字节一致，`GetHeader("Content-Type")` 返回 `Application/SDP`，`ContentLength()` 等于入参 body 字节数

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

The system SHALL produce a Digest `WWW-Authenticate` challenge containing `realm`, `nonce`, `qop=auth`, `algorithm=MD5`, and `opaque` (optional), and compute the matching `Authorization` response including the `qop=auth` mode (with `nc`, `cnonce`, `response`).

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

### Requirement: Multiple in-process transport listeners

The system SHALL support binding more than one SIP transport listener in the same process on independent local addresses, each with its own TCP/UDP socket and isolated send/receive queue.

#### Scenario: 同一进程内两个 UDP listener

- **WHEN** 上层创建 `Transport(bind=127.0.0.1:5060)` 与 `Transport(bind=127.0.0.1:5061)` 两个实例
- **THEN** 两实例可同时 `Listen()` 且互不干扰；监听端口不同的请求可路由到对应的 listener 实例

#### Scenario: listener 关闭后端口立即释放

- **WHEN** 调用 `Transport.Close()`
- **THEN** 原绑定端口可被后续 `Transport(bind=<same>)` 立即重新绑定（无 TIME_WAIT 阻塞）

### Requirement: SIP audit log produces wire-byte events

The system SHALL emit a structured log record for every received and transmitted SIP message, containing the direction (`rx`/`tx`), the transport endpoint (local/remote address), and the raw bytes (clipped to 4 KiB) trace-level; sensitive Digest credentials in headers SHALL be redacted.

#### Scenario: 接收报文产生一条 rx trace 记录

- **WHEN** listener 收到一条 INVITE
- **THEN** `internal/logger` 在 trace 级别写入 `msg="sip rx"`、包含 `local`、`remote`、`bytes`（原文）字段；`Authorization` 头中的 `response` 字段若出现在日志中显示为 `***REDACTED***`

#### Scenario: 发送报文产生一条 tx trace 记录

- **WHEN** 上层调用 `Transport.Send(req, dst)` 成功
- **THEN** 写入 `msg="sip tx"`，包含 `local`、`remote`、`bytes` 字段

### Requirement: Diagnostic CLI `sipprobe` for golden tests

The system SHALL provide a `sipprobe` subcommand capable of binding a UDP/TCP listener and sending a single SIP message, then printing the first received response to stdout, used by CI golden tests and developer smoke checks.

#### Scenario: sipprobe 接收一条 INVITE 后打印 200 OK

- **WHEN** 执行 `gb28181-simulator sipprobe --bind udp:127.0.0.1:5060 --expect-status 200` 并由外部 client 发送 INVITE
- **THEN** sipprobe 等待 ≤ 5 秒后退出 0，并将 `Response.StatusCode()` 与 `Response.StartLine()` 写入 stdout 一行（TSV 形式）

#### Scenario: sipprobe 超时退出非零

- **WHEN** 5 秒内未收到响应
- **THEN** sipprobe 退出码为 2，stderr 输出 `timeout waiting for status=200`，stdout 为空

### Requirement: CGO-free build on all supported platforms

The system SHALL compile and pass `CGO_ENABLED=0 go build ./...` on Linux amd64/arm64, macOS amd64/arm64, and Windows amd64, with no new CGO dependency introduced by this change.

#### Scenario: 跨平台无 CGO 编译成功

- **WHEN** 在五平台分别执行 `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build ./cmd/sipprobe`
- **THEN** 产物为静态链接二进制，无 `cgo` 警告

# Spec Delta

## MODIFIED Requirements

### Requirement: Digest challenge and response (RFC 2617 / RFC 7616 / GB28181)

The system SHALL cover both directions of GB/T 28181 Digest authentication. Server side:
produce a `WWW-Authenticate` challenge containing `realm`, `nonce`, `qop=auth`,
`algorithm=MD5`, and `opaque` (optional). Client side: parse such a challenge (including one
received from a third-party platform), and produce the matching `Authorization` header value —
computing `response` per RFC 7616 §3.4 when `qop=auth` is offered (with `nc`, `cnonce`) and per
RFC 2617 §3 otherwise, and rendering the complete header with correctly quoted fields.
Unsupported `algorithm` values MUST surface an explicit error instead of silently falling back.

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

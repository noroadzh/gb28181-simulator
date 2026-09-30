# core-sip-stack 规范

## Purpose

节点无关的 GB/T 28181 SIP/SDP/Digest 底层库 + 多实例 transport listener + 审计日志 + sipprobe 诊断工具。本 capability 为 Change 4-15（Node 抽象、三种身份、媒体、级联、2022 扩展、GB35114、异常流、Web、场景引擎）提供可被 embed 的协议底座；任何身份、注册、心跳、媒体、级联行为一律不在本 capability 范围内。

## Requirements

### Requirement: SIP 消息往返保持线上字节一致

系统 MUST 能构建与解析 SIP 请求和响应消息，其线上字节表示在 start-line、头字段和 SDP body 上与 GB/T 28181 §L.1（RFC 3261）一致，**不得**进行有损重格式化。调用方显式提供的头部 MUST 在 stack 执行的每次转换中均保留，包括从领域消息类型传递到线编码器的过程：丢弃调用方提供的头部（如 `Contact`、`Expires`、`Authorization`）是缺陷，不被允许的简化。

#### 场景：构建并解析一条 REGISTER 后字节一致

- **WHEN** 上层传入完整头部（`Via`、`From`、`To`、`Call-ID`、`CSeq`、`Contact`、`Max-Forwards`、`Expires`、`User-Agent`、`Allow`、`Content-Length`）与无 body 的 REGISTER 请求
- **THEN** 经序列化-反序列化循环后，原始 start-line、所有头字段名/值（保留参数顺序与原大小写）与 body 长度保持与构造时一致

#### 场景：解析含 SDP body 的 INVITE 后字段完整

- **WHEN** 收到一条 `Content-Type: Application/SDP` 的 INVITE 请求
- **THEN** 解析后 `Body()` 返回的字符串与入参 byte 字节一致，`GetHeader("Content-Type")` 返回 `Application/SDP`，`ContentLength()` 等于入参 body 字节数

#### 场景：调用方显式给出的头部经传输层转换后不丢失

- **WHEN** 上层以领域消息类型携带 `Contact`、`Expires`、`Authorization`、`From`、`To`、`Call-ID` 等头部交给传输层发送
- **THEN** 对端收到的报文中这些头部全部存在且取值与调用方给出的一致；不因"重建消息"而被丢弃或替换为自动生成的默认值

#### 场景：Via 的传输协议与实际传输一致

- **WHEN** 以 TCP 传输发送一条由上层构造的请求
- **THEN** `Via` 头中的传输协议为 `TCP`（而非固定 `UDP`）；未指定传输时沿用默认 UDP，行为不变

### Requirement:  GB/T 28181 mandatory headers auto-filled

系统 MUST 在调用方未提供时自动填充 GB/T 28181-2016 §L.1 §L.2 要求的头部：唯一的 `Via` 分支（`z9hG4bK-` + 12 位随机 base32 字符）、`Max-Forwards`（默认 70）、`Content-Length`（有 body 时填充）和 `User-Agent`（包含模拟器版本号）。

#### 场景：未提供 Via 时自动生成唯一分支

- **WHEN** 上层构造请求不提供 `Via`
- **THEN** 序列化结果包含恰好一个 `Via: SIP/2.0/...;branch=z9hG4bK-...` 头，`branch` 参数长度 ≥ 13 字符且与进程内其它并发构造的请求不重复（碰撞概率 < 2^-40）

#### 场景：提供 Content-Type 时自动填充 Content-Length

- **WHEN** 构造请求同时提供 body 与 `Content-Type: Application/SDP`
- **THEN** 序列化结果包含 `Content-Length: <n>`，其中 `<n>` 等于 body 字节数；不重复出现上层显式给出的 Content-Length（避免双头）

### Requirement:  GB/T 28181-2016 §K SDP parsing

系统 MUST 解析包含 GB/T 28181 §K 扩展行的 SDP body：`y=`（32 位十进制 SSRC）与 `f=`（媒体选项：`v` / `AudioTrack` / `VideoTrack` / `AudioVideoTrack` / `Metadata`），以结构化字段暴露且不丢失 RFC 4566 各段内容。

#### 场景：解析 INVITE 中的 SDP 含 `y=`/`f=` 不丢失

- **WHEN** 收到一条 INVITE 请求，body 为 `v=0...m=video 0 RTP/AVP 96...y=1234567890...f=v...a=rtpmap:96 PS/90000`
- **THEN** 解析后 `SSRC()` 返回字符串 `1234567890`，`MediaOption()` 返回 `v`；同时 RFC 4566 字段 `Origin`/`ConnectionInformation`/`MediaName`/`MediaDescription.Attributes` 全部可访问

#### 场景：缺少 `y=`/`f=` 时不报错

- **WHEN** 收到的 SDP body 不含 §K 扩展（纯 RFC 4566）
- **THEN** 解析成功；`SSRC()` 返回空字符串，`MediaOption()` 返回空字符串；上层应能区分"未声明"与"声明为空"

### Requirement:  GB/T 28181 §K SDP serialization

系统 MUST 按 GB/T 28181-2016 §K.2 的顺序 marshal 包含 `y=` 与 `f=` 行的 SDP body：两行位于 `m=`/`a=`（媒体描述块）之后、下一个 `m=` 之前，且 MUST 按 `y=`、再 `f=` 的顺序出现。

#### 场景：marshal 含 SSRC 与 MediaOption 的 SDP

- **WHEN** 上层调用 marshal 并传入 SSRC=`1234567890`、MediaOption=`v`
- **THEN** 输出字符串在对应 media description 内按 `m=...` → `a=...` → `y=1234567890` → `f=v` 顺序排列

#### 场景：未设 SSRC 时不输出 `y=` 行

- **WHEN** 上层 SSRC 为空
- **THEN** marshal 结果不含空 `y=` 行；`f=` 仍可独立输出

### Requirement:  Digest challenge and response (RFC 2617 / RFC 7616 / GB28181)

系统 MUST 覆盖 GB/T 28181 Digest 认证的双向流程。Server 侧：生成包含 `realm`、`nonce`、`qop=auth`、`algorithm=MD5` 与可选 `opaque` 的 `WWW-Authenticate` 挑战。Client 侧：解析此类挑战（包括从第三方平台收到的），并生成匹配的 `Authorization` 头值——提供 `qop=auth` 时按 RFC 7616 §3.4 计算 `response`（携带 `nc`、`cnonce`），否则按 RFC 2617 §3 计算，并以正确的引号包裹方式渲染完整头部。不支持的 `algorithm` 取值 MUST 显式报错，不得静默回退。

#### 场景：服务端生成 challenge

- **WHEN** 上层以 `realm="gb28181"`、nonce 由 16 字节随机 base64 编码触发生成 challenge
- **THEN** 输出包含 `Digest realm="gb28181"`, `nonce="..."`, `qop="auth"`, `algorithm=MD5`；`nonce` 长度 ≥ 22 字符，每次调用与进程内其它调用不重复

#### 场景：客户端正确计算 response（qop=auth）

- **WHEN** 上层以 username/password/realm/nonce/qop=auth/method=REGISTER/uri 触发 response 计算
- **THEN** 输出 `response` 字段等于 `MD5(MD5(username:realm:password) : nonce : nc : cnonce : qop : MD5(method:uri))`（hex 大写），符合 RFC 7616 §3.4

#### 场景：客户端校验服务端返回的 response

- **WHEN** server 收到带 `Authorization` 的请求，并能用客户端持有的密码重新计算
- **THEN** 两者 `response` 字段逐字节相等时返回 `nil`，否则返回 `auth.ErrInvalidResponse` 且**不**回写 `WWW-Authenticate` 重试（GB28181 标准要求 server 拒绝一次）

#### 场景：username/realm 字段非 ASCII 字节按 UTF-8 规范化

- **WHEN** 客户端送上 `Authorization` 中 `username`/`realm` 字段含合法非 ASCII 字节（例 `username="用户-001"`），且密码与服务端一致
- **THEN** RFC 7616 §3.3 规定的 UTF-8 规范化路径生效：`HA1 = MD5(<规范化 username>:<规范化 realm>:password)` 与对端计算结果逐字节相等 → 返回 `nil`；若 UTF-8 校验不过则返回 `ErrInvalidResponse`
- **AND** GB28181 标准设备 ID 全部 ASCII（不变场景）：UTF-8 路径与原 RFC 2617 路径对相同输入产生相同哈希（向后兼容验证）

#### 场景：qop 字段缺失时回退 RFC 2617

- **WHEN** 客户端送上 `Authorization` 中**没有** `qop` 字段（旧客户端实现）
- **THEN** `expected = MD5(MD5(username:realm:password) : nonce : MD5(method:uri))`，与请求 `response` 字段逐字节相等时返回 `nil`，否则返回 `ErrInvalidResponse`

#### 场景：解析第三方平台下发的 WWW-Authenticate

- **WHEN** 收到 `WWW-Authenticate: Digest realm="3402000000", nonce="...", qop="auth", algorithm=MD5, opaque="..."`
- **THEN** 解析结果给出 realm、nonce、opaque、qop、algorithm 各字段，与原文逐字段相等；带引号与不带引号的取值、字段间多余空白、大小写不同的 `Digest` 方案名均可正确解析

#### 场景：挑战缺少必需字段时报错

- **WHEN** `WWW-Authenticate` 缺少 `realm` 或 `nonce`，或其值无法解析
- **THEN** 返回错误并指出缺失/非法的字段，不返回零值结构让上层静默发出错误凭据

#### 场景：组装完整的 Authorization 头

- **WHEN** 上层以凭据、方法、请求 URI 与已解析的挑战触发 Authorization 生成
- **THEN** 输出形如 `Digest username="...", realm="...", nonce="...", uri="...", response="...", algorithm=MD5, qop=auth, nc=00000001, cnonce="..."` 的完整头值
- **AND** `nc` 与 `cnonce` 由实现生成（`cnonce` 每次事务不同），`username` / `realm` / `nonce` / `uri` / `opaque` 中的特殊字符被引号包裹并转义
- **AND** 该头值可被本 capability 的解析路径反向解析且字段一致

#### 场景：挑战声明不支持的算法时报错

- **WHEN** `WWW-Authenticate` 的 `algorithm` 为 MD5 / MD5-sess 之外的值
- **THEN** 生成 Authorization 返回"算法不支持"的错误，不静默按 MD5 计算

### Requirement: 进程内多传输监听器

系统 MUST 支持在同一进程内绑定多个 SIP 传输监听器，各自绑定独立的本地地址，每个监听器拥有独立的 TCP/UDP socket 和隔离的发送/接收队列。

传输队列的并发关闭契约（2026-09-30 fix-concurrency-lifecycles 新增的不变量）：队列的 `Close()` MUST 与消息分发及外部注册的事务 handler 并发安全；`Close()` 返回后任何对内部队列的 send MUST NOT panic——与关闭竞争的 send 要么成功投递要么被静默丢弃。队列关闭顺序 MUST 为：先置 closed 标志 → 再 close channel → 最后等待 reader 退出。重复调用 `Close()` MUST 幂等。

#### 场景：同一进程内两个 UDP 监听器

- **WHEN** 上层创建 `Transport(bind=127.0.0.1:5060)` 与 `Transport(bind=127.0.0.1:5061)` 两个实例
- **THEN** 两实例可同时 `Listen()` 且互不干扰；监听端口不同的请求可路由到对应的 listener 实例

#### 场景：监听器关闭后端口立即释放

- **WHEN** 调用 `Transport.Close()`
- **THEN** 原绑定端口可被后续 `Transport(bind=<same>)` 立即重新绑定（无 TIME_WAIT 阻塞）

#### 场景：关闭与事务 handler 并发不 panic（fix-concurrency-lifecycles 新增）

- **WHEN** 外部注册的事务 handler 恰好在节点停止、队列被关闭的同一时刻被触发
- **THEN** 不发生 `send on closed channel` panic；竞争中的消息要么投递成功要么被静默丢弃

#### 场景：重复 Close 幂等（fix-concurrency-lifecycles 新增）

- **WHEN** 对同一 splitter/transport 队列实例连续调用两次 `Close()`
- **THEN** 第二次调用返回 nil，channel 与 reader 均不再被触碰

### Requirement: SIP 传输层显式暴露对端与目标地址

系统 MUST 将远端端点地址显式化：发送消息时 MUST 接受目标地址，接收消息时 MUST 报告报文到达的地址。地址为 `host:port` 字符串（可选以传输 scheme 为前缀）。无法确定对端地址的传输层 MUST 显式返回错误，而非返回空字符串。

#### 场景：发送使用显式目标地址

- **WHEN** 调用传输层发送接口并传入目标地址 `127.0.0.1:5060`
- **THEN** 报文被发往该地址；不再依赖从报文 URI 反推目标；返回值仅表达传输层错误

#### 场景：接收返回对端地址

- **WHEN** 传输层收到一条来自 `127.0.0.1:5061` 的报文
- **THEN** 接收结果同时给出报文与该对端地址 `127.0.0.1:5061`；UDP 与 TCP 两种传输均如此

#### 场景：对端地址可用于直接回包

- **WHEN** 以接收到的对端地址作为目标发回报文
- **THEN** 报文到达原发送方，无需额外配置路由

#### 场景：无法判定地址时显式报错

- **WHEN** 传输实现无法确定对端地址
- **THEN** 返回错误（而非返回空地址让调用方静默发往错误目标）

### Requirement: SIP 审计日志产生线上字节事件

系统 MUST 对每条接收和发送的 SIP 消息写入结构化日志记录，包含方向（`rx`/`tx`）、传输端点（本地/远端地址）以及原始字节（trace 级别，裁剪至 4 KiB）；头部中的敏感 Digest 凭据 MUST 脱敏。

#### 场景：接收报文产生一条 rx trace 记录

- **WHEN** listener 收到一条 INVITE
- **THEN** `internal/logger` 在 trace 级别写入 `msg="sip rx"`、包含 `local`、`remote`、`bytes`（原文）字段；`Authorization` 头中的 `response` 字段若出现在日志中显示为 `***REDACTED***`

#### 场景：发送报文产生一条 tx trace 记录

- **WHEN** 上层调用 `Transport.Send(req, dst)` 成功
- **THEN** 写入 `msg="sip tx"`，包含 `local`、`remote`、`bytes` 字段

### Requirement: 诊断 CLI `sipprobe` 供 golden 测试使用

系统 MUST 提供 `sipprobe` 子命令，能够绑定 UDP/TCP 监听器并发送一条 SIP 消息，然后将收到的第一条响应打印到 stdout，供 CI golden 测试与开发者冒烟测试使用。在仅接收模式下，通过 `--answer` 调用时 MUST 能以 200 OK 响应入站 SIP 请求，使两个 `sipprobe` 进程无需第三方进程即可完成请求/响应交换。应答功能 MUST 显式启用：不带 `--answer` 时可观察行为不变。

#### 场景：sipprobe 接收一条 INVITE 后打印 200 OK

- **WHEN** 执行 `gb28181-simulator sipprobe --bind udp:127.0.0.1:5060 --expect-status 200` 并由外部 client 发送 INVITE
- **THEN** sipprobe 等待 ≤ 5 秒后退出 0，并将 `Response.StatusCode()` 与 `Response.StartLine()` 写入 stdout 一行（TSV 形式）

#### 场景：sipprobe 超时退出非零

- **WHEN** 5 秒内未收到响应
- **THEN** sipprobe 退出码为 2，stderr 输出 `timeout waiting for status=200`，stdout 为空

#### 场景：以 --answer 回应入站请求

- **WHEN** 执行 `gb28181-simulator sipprobe --bind udp://127.0.0.1:15060 --answer` 并收到一条入站 `sip.Request`
- **THEN** sipprobe 向该请求的对端地址回送一条 200 OK（`sip.NewResponseFromRequest`），请求方收到后退出 0

#### 场景：两个 sipprobe 互发 INVITE/200 OK

- **WHEN** 进程 A 执行 `sipprobe --bind udp://127.0.0.1:15060 --answer`，
  进程 B 执行 `sipprobe --bind udp://127.0.0.1:15061 --send-to udp://127.0.0.1:15060 --expect-status 200`
- **THEN** B 收到 200 OK 并退出 0；A 回应后退出 0；`scripts/smoke-sip.sh` 的原始断言通过

### Requirement: 所有支持平台 CGO-free 构建

系统 MUST 在 Linux amd64/arm64、macOS amd64/arm64 与 Windows amd64 上编译并通过 `CGO_ENABLED=0 go build ./...`，本变更不引入新的 CGO 依赖。

#### 场景：跨平台无 CGO 编译成功

- **WHEN** 在五平台分别执行 `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build ./cmd/sipprobe`
- **THEN** 产物为静态链接二进制，无 `cgo` 警告

### Requirement: INFO 方法按 Content-Type 分发

节点必须（MUST）对入站 `INFO` 请求按 `Content-Type` 路由：
1. `Application/MANSRTSP`（或 body 首行为 `PLAY` / `PAUSE`）→ 解析为 MANSRTSP 回放控制命令，路由到 `PlaybackPort`，返回 `200 OK`（port 不支持时回 `481 Call/Transaction Does Not Exist` 或 `400 Bad Request`）。
2. `Application/MANSCDP+XML` 且 body 的 `CmdType` 为 `MediaStatus` → 复用 `handleMediaStatus`，返回 `200 OK`。
3. 其余 body → `200 OK`（宽容处理），记录 debug 日志。

未知 body 形状必须（MUST）不使节点崩溃或中断事务。

#### Scenario: INFO + MANSRTSP PLAY 路由到 playback port

- **WHEN** 对端发送 `INFO`，body 为 `PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: 2.0\r\nRange: npt=10-\r\n\r\n`，`Content-Type: Application/MANSRTSP`
- **THEN** 节点解析出 `Command=PLAY, Scale=2.0, Seek=10`，调用 `PlaybackPort.Play(scale=2.0)`，回 `200 OK`

#### Scenario: INFO + MANSCDP MediaStatus 走 handleMediaStatus

- **WHEN** 对端发送 `INFO`，`Content-Type: Application/MANSCDP+XML`，body 为 `<Notify><CmdType>MediaStatus</CmdType><DeviceID>...</DeviceID></Notify>`
- **THEN** 节点调用 `MediaStatusPort.HandleMediaStatus` 并回 `200 OK`，与 MESSAGE 路径行为一致

#### Scenario: 未知 INFO body 宽容应答

- **WHEN** 对端发送 `INFO`，body 为任意二进制
- **THEN** 节点回 `200 OK`，记录 debug 日志指出无法识别的 body 形状

### Requirement: MANSRTSP 解析器（2022 §G）

`internal/adapter/mansrtsp` 包必须（MUST）提供解析器，把 MANSRTSP body 解析为结构化命令：
- 请求行：`PLAY` / `PAUSE`（method）。
- `Scale` 头：浮点倍速（负数为倒放，0 表示暂停）；缺失时默认 1.0。
- `Range: npt=<start>-<end>` 头：起止时间（秒或 `now`）；缺失时不 seek。
- 无法解析的 body 返回清晰错误。

解析器必须（MUST）被 golden test 覆盖（字节级断言 body → 字段）。

#### Scenario: 解析含 Scale 与 Range 的 PLAY

- **WHEN** 输入 body 为 `PLAY RTSP/1.0\r\nCSeq: 7\r\nScale: 4.0\r\nRange: npt=3600-\r\n\r\n`
- **THEN** 解析结果为 `Command=PLAY, CSeq=7, Scale=4.0, SeekFrom=3600s`

#### Scenario: 解析 PAUSE 无附加头

- **WHEN** 输入 body 为 `PAUSE RTSP/1.0\r\nCSeq: 8\r\n\r\n`
- **THEN** 解析结果为 `Command=PAUSE, Scale=1.0, SeekFrom=0`（默认值）

### Requirement: PlaybackPort 扩展 Scale 语义

`PlaybackPort` 接口必须（MUST）扩展：`Play` 方法新增 `scale float64` 参数（1.0 为正常倍速，0 为暂停，负数为倒放）；`PlaybackState` 新增 `Scale float64` 字段（当前生效倍速）。stub adapter 对非法 scale（NaN 或 ±Inf）返回 `ErrPlaybackUnsupported`。2016 风格 `PlaybackControl` MESSAGE 路径必须（MUST）同步接通到 `PlaybackPort`（当前实现仅回 200 OK 不调 port）。

#### Scenario: PlaybackControl MESSAGE 调用 PlaybackPort

- **WHEN** 设备发送 `MESSAGE`，body 为 `<CmdType>PlaybackControl><PlaybackControl>Play</PlaybackControl><MediaID>...</MediaID></Notify>`
- **THEN** 节点调用 `PlaybackPort.Play(scale=1.0)`，回 `200 OK`

#### Scenario: 非法 scale 被拒绝

- **WHEN** MANSRTSP 命令携带 `Scale: NaN`
- **THEN** `PlaybackPort.Play` 返回 `ErrPlaybackUnsupported`，acceptor 回 `400 Bad Request`

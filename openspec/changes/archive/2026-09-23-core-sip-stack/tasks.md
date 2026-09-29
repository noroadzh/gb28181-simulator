# Tasks

> 本 change 是 Change 2（路线图 15 步中的第二步），目标是把"节点无关的 GB/T 28181 SIP/SDP/Digest 底层库 + 多实例 transport listener + 审计日志 + sipprobe 诊断工具"一次性落地。
> 任务粒度 ≤ 2 小时；涉及 SIP/SDP/PS 字节格式的任务必须含 golden test（字节级断言）。
> 本 change 不实现任何节点身份行为（UAC/UAS/REGISTER/INVITE 等业务编排），留待 Change 4-7。

## 1. 依赖 & 编译基线

- [x] 1.1 在 `go.mod` 中加入 `github.com/ghettovoice/gosip`（emiago 维护、go.mod module 路径仍为 `github.com/ghettovoice/gosip`）与 `github.com/pion/sdp`，运行 `go mod tidy` 验证 `go.sum` 无错误；并在仓库根目录用 `go list -m github.com/ghettovoice/gosip github.com/pion/sdp` 输出两个固定版本号（记录到本任务的 commit message）
- [x] 1.2 验证 `CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test ./...` 在 darwin/amd64 上通过；在 Linux 容器或交叉编译（GOOS=linux/arm64 GOOS=windows/amd64）中至少保证 `go build ./cmd/sipprobe` 通过——命令预期输出"无 cgo 警告"

## 2. 内部包：`internal/sdp`

- [x] 2.1 定义 `gb.Session` 结构体：`Origin`、`SessionName`、`ConnectionInformation`、`TimeDescriptions`、`Attributes`（pion `SessionDescription` 原样字段）+ 顶层 `SSRC string`（会话级 SSRC）+ `Media []*MediaBlock`，每个 `MediaBlock` 含 pion `MediaDescription` + `SSRC string` + `MediaOption string`；并验证 `go test ./internal/sdp/... -run TestSession_RoundTrip -v` 通过
- [x] 2.2 实现 `Parse(text string) (*gb.Session, error)`：行级预扫描抽出 `y=`/`f=` 行 → pion `Session.Unmarshal` 解析剩余部分 → 回填；并验证 golden fixture `testdata/gb28181-invite-ps.sdp` 解析后 SSRC/MediaOption 与属性正确（字节级断言）
- [x] 2.3 实现 `Marshal(s *gb.Session) (string, error)`：先 marshal pion `SessionDescription` → 在每个 `m=` block 末尾按 GB28181 §K.2 行序插入 `y=`/`f=`；并验证黄金样本 `testdata/gb28181-invite-ps.sdp` marshal 输出与原始文本字段顺序一致（允许 `norm=`/`origin=` 数值随机化用正则断言）
- [x] 2.4 兼容测试：恶意/边界 SDP（缺 `y=`、多 `y=`、`a=y:12345`、空 `f=`、CRLF 与 LF 混用）期望均解析成功或返回明确错误；并验证 5 个子用例单测通过
- [x] 2.5 性能基准：`go test -bench BenchmarkParse -benchmem ./internal/sdp/...` 给出基线数字并 commit 时记录；预期 `Parse` < 50 µs / 1 KiB SDP

## 3. 内部包：`internal/auth`

- [x] 3.1 实现 `Challenger`：`Challenge(realm string, opts ...ChallengeOption) (wwwAuthenticate string, nonce string, err error)`，输出 `Digest realm="...", nonce="<base64(16-byte)>", qop="auth", algorithm=MD5[, opaque="..."]`；并验证 `TestChallenger_GeneratesUniqueNonce`（100 次调用 nonce 互不重复）、`TestChallenger_FormatMatchesRFC7616`（golden fixture 字节级断言）
- [x] 3.2 实现 `Responder.Verify(req, password) error`：从 `Authorization` 头解析 `username`、`realm`、`nonce`、`qop`、`nc`、`cnonce`、`response`、`uri`，按 RFC 7616 §3.4 重新计算 `expected = MD5(MD5(user:realm:password) : nonce : nc : cnonce : qop : MD5(method:uri))`，与请求 `response` 字节比较；并验证：(a) 正确凭据返回 nil；(b) 错误 password 返回 `ErrInvalidResponse` 且**不**重发 401
- [x] 3.3 暴露可插拔 `HashFunc func(string) string`（默认 `MD5`）；为 Change 12 预留；并验证 `TestResponder_HashFuncOverride`（用 SHA1 替换后 Golden 哈希同步更新）
- [x] 3.4 不带 qop 的兼容模式（纯 RFC 2617 §3）：当请求 `qop` 字段缺失时仍能校验 `response = MD5(MD5(user:realm:password):nonce:MD5(method:uri))`；并验证 `TestResponder_NoQopLegacyClient` 通过
- [x] 3.5 UTF-8 规范化（RFC 7616 §3.3）：当 `Authorization` 头中 `username`/`realm` 含非 ASCII 字节时按 UTF-8 规范化路径计算 HA1，且当输入全部 ASCII 时与 RFC 2617 公式产生相同哈希；并验证 `TestResponder_UTF8Normalization_Pass`（合法非 ASCII 通过）与 `TestResponder_UTF8Normalization_ASCIIIdentity`（ASCII 输入两公式等价）通过

## 4. 内部包：`internal/sip`（基于 `ghettovoice/gosip`）

- [x] 4.1 在 `internal/sip` 暴露 `BuildRequest(method, uri, opts)`、`BuildResponse(status, opts)`、`ParseMessage(buf []byte)` 三函数；并验证：(a) `BuildRequest`+`Serialize`+`ParseMessage` round-trip 后 `Method/URI/Headers/Body` 与构造时一致；(b) `BuildRequest` 自动填充 `Via` 分支唯一 + `Max-Forwards=70` + `User-Agent="gb28181-simulator/<version>"`（构造时未显式提供）
- [x] 4.2 `Via` branch 唯一性：实现 `BranchGenerator` 接口（默认用 crypto/rand 12 字节 base32）；并验证 `TestBranchGenerator_CollisionResistance` —— 1000 次并发调用无重复
- [x] 4.3 `Content-Length` 自动填充：当 body 非空且未显式提供 `Content-Length` 时自动添加；显式提供时不重复；并验证 `TestContentLength_AutoAndExplicit` 字节级通过
- [x] 4.4 黄金样本 `testdata/real-register.pcap.txt`（含 Content-Type/Allow/User-Agent/Expires/Authorization）与 `testdata/real-invite-200bye.pcap.txt`（含 SDP body）：解析 + 重新序列化 → 与原始字节比对（允许 `Via` branch 不同，断言除 `Via` 外字段一致）
- [x] 4.5 `X-GB-Ver` 透传：在 `BuildRequest(opts)` 与 `ParseMessage` 中支持可选 `X-GB-Ver` 头解析/写入（为 Change 11 留 hook）；并验证 `TestXGBVer_HeaderPassthrough` 通过
- [x] 4.6 `Authorization` 解析辅助：`ParseAuthorization(value string) (username, realm, nonce, qop, nc, cnonce, response, uri string, err error)`；为 `internal/auth.Responder.Verify` 提供解析能力；并验证 golden fixture 覆盖 qop=auth / 无 qop / 大写 nc 三种

## 5. 内部包：`internal/sip/audit`

- [x] 5.1 定义 `WireEvent{Direction, Local, Remote string; Bytes []byte; Timestamp time.Time}` 与 `Emitter interface{ Emit(WireEvent) }`；默认全局 emitter `Default = slogEmitter{}` 把事件转写为 `internal/logger` 的 trace 记录（含 `password`/`response`/`Authorization` 字段脱敏）
- [x] 5.2 4 KiB 截断：`Emit` 中 `Bytes` 长度 > 4096 时仅保留前 4096 字节并加 `truncated=true` 字段；并验证 `TestAudit_ClipsAt4096` 通过
- [x] 5.3 全局 emitter 可注入：测试可通过 `audit.SetEmitter(custom)` 替换；并验证 `TestAudit_ReplaceEmitter` 验证替换生效

## 6. 内部包：`internal/siptransport`

- [x] 6.1 实现 `New(bind string, opts ...Option) (*Transport, error)`：`bind` 形如 `udp://127.0.0.1:5060` / `tcp://0.0.0.0:5060` / `tls://...`，解析后调 `gosip/transport.Layer` 创建 UDP/TCP/TLS 监听；并验证 `TestTransport_BindUDP` / `TestBindTCP` 在随机端口通过
- [x] 6.2 实现 `(t *Transport) Send(req sip.Request, dst string) error`：序列化后通过 `Layer.Send([]byteString)` 发送；并验证 1 路实例可对目标地址发包（loopback 用 iotest 抓包验证）
- [x] 6.3 实现 `(t *Transport) Receive(ctx context.Context) (sip.Message, string, error)`：内部 goroutine 从 `Layer` 拉包 → 放入 64 容量 channel；并验证 `(a)` 100 条包 1 个订阅者全收到；`(b)` 100 条包 1 个订阅者但 channel 满 60% 时记 warn 日志（用 `audit.SetEmitter` 探针）
- [x] 6.4 审计钩子：每次 `Send`/`Receive` 路径调 `audit.Global().Emit(WireEvent{...})`；并验证 `TestTransport_EmitsAuditEvents` 通过——5 包收发产生 10 条 audit
- [x] 6.5 多实例共存：`TestTransport_MultipleInstances` 在同一进程内启动两个 UDP listener（不同端口），分别收发互不干扰；并验证 `Close()` 后端口立即可重用（`net.Listen("udp", sameAddr)` 不报 `address already in use`）

## 7. CLI 入口：`cmd/sipprobe`

- [x] 7.1 `cmd/sipprobe/main.go`：`flag.String("bind", "", "udp|tcp|tls://host:port")`、`flag.String("send-to", "", "target address")`、`flag.Int("expect-status", 0, "...")`、`flag.Duration("timeout", 5*time.Second, "...")`；并验证 `gb28181-simulator sipprobe --help` 输出 4 个 flag 说明
- [x] 7.2 `--send-to` 与 `--expect-status` 同时给定时，构造一条最小 INVITE（带 SDP）→ 等待首个响应 → stdout 打印 `StatusCode\tStartLine`；并验证 `TestSipprobe_ReplyIfMissing` 5 秒无响应时退出码 2、stderr 含 `timeout waiting for status=...`
- [x] 7.3 仅 `--bind` 时（不发送）：等待任意 60 秒首条入向包 → 同样格式输出；超时退出 2；并验证 `TestSipprobe_ReceiveOnly` 通过
- [x] 7.4 把 `cmd/sipprobe` 加入 `Makefile` 的 `release-matrix` 矩阵构建列表（与 `cmd/gb28181-simulator` 共存），验证 `make release-matrix` 仍产出 5 平台二进制 + sha256

## 8. Golden fixture 与冒烟脚本

- [x] 8.1 在 `internal/sip/testdata/` 与 `internal/sdp/testdata/` 与 `internal/auth/testdata/` 各放 ≥ 3 份手工构造的 `.sip` / `.sdp` / `.auth` 黄金样本（涵盖 REGISTER、INVITE+SDP、200 OK、401、BYE、CANCEL），并附 `.sha256` 与简短来源说明；并验证 `find . -name 'testdata' -type d | xargs -I{} sh -c 'cd {} && sha256sum -c *.sha256'` 通过
- [x] 8.2 `scripts/smoke-sip.sh`：本地启 `cmd/sipprobe --bind udp:127.0.0.1:5060 --expect-status 200` + `cmd/sipprobe --send-to --expect-status 200` 双进程互发 INVITE 与 200 OK；并验证脚本退出 0、两个进程退出码 0、stdout 行数 ≥ 2

## 9. 跨平台 & CI

- [x] 9.1 在 `Makefile` 新增目标 `make sip-test`（仅跑 SIP/SDP/Auth 子包测试，更快），并把 `make test` 维持全包测试；并验证 `make sip-test` < 5s 完成
- [x] 9.2 验证 `ci.yml`（无需修改）：五平台 `go build ./cmd/sipprobe` + `go test ./...` 全绿；本地用 `make release-matrix` 在 darwin/amd64 产出 5 平台二进制无 cgo 警告；sha256 与 Change 1 的 release 流程一致
- [x] 9.3 在 `README.md` 的"技术栈速览"小节补充 `ghettovoice/gosip` 与 `pion/sdp` 两行，并在"开发指南"补充 `make sip-test` 与 `scripts/smoke-sip.sh` 两条命令

## 10. 端到端验证

- [x] 10.1 启动 `bin/gb28181-simulator sipprobe --bind udp:127.0.0.1:5060 --expect-status 200 &` 与 `bin/gb28181-simulator sipprobe --bind udp:127.0.0.1:5061 --send-to udp:127.0.0.1:5060 --expect-status 200`，验证两个进程在 ≤ 5 秒内均收到对方 INVITE/200 OK、退出码 0
- [x] 10.2 用 `tcpdump -i lo0 -s0 -w /tmp/sipprobe.pcap udp portrange 5060-5061` 抓包，验证 pcap 中 INVITE 与 200 OK 字节与手工构造的 golden fixture 一致（用 `tcpdump -r` + `xxd | diff`）
- [x] 10.3 在 Linux 容器内（若可用）重复 10.1/10.2；不可用则在文档限制说明

---

## Evidence

按包分别产出 6 份测试报告 + 1 份索引页（全部为 2026-09-23 本轮重新执行；每份含命令块原文、表格汇总、"实施过程中的设计与缺陷记录"小节）：

- **索引页（先读）**：`reports/change2-summary.md` — 6 份报告导航、总览表、归档状态
- §2 SDP 详细：`reports/change2-sdp.md`
- §3 Auth 详细：`reports/change2-auth.md`
- §4–§5 SIP + audit 详细：`reports/change2-sip.md`
- §6 siptransport 详细：`reports/change2-siptransport.md`
- §7 sipprobe 详细：`reports/change2-sipprobe.md`
- §8–§10 端到端 + Makefile/CI/README：`reports/change2-e2e.md`

预期基线：
- `go test -race ./...` — 100% pass（含 SDP/auth/SIP/audit/transport 各包黄金测试 + race 检测）
- `make release-matrix` — 5 平台二进制含 `sipprobe`，CGO_ENABLED=0 无 cgo 警告
- `scripts/smoke-sip.sh` — exit 0，双进程 stdout ≥ 2 行
- Golden fixture：`find . -name 'testdata' -type d | xargs -I{} sh -c 'cd {} && sha256sum -c *.sha256'` 全绿
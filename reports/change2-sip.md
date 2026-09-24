# Change 2 §4–§5 — `internal/sip` 与 `internal/sip/audit` 报告

> 对应 `tasks.md` §4（6 个子任务 4.1–4.6）和 §5（3 个子任务 5.1–5.3）。

## 1. 测试结果

### 1.1 `internal/sip`

| 子任务 | 用例 | 状态 | 备注 |
| --- | --- | --- | --- |
| 4.1 mandatory headers | `TestBuildRequest_MandatoryHeaders` | ✅ | Via/CSeq/From/To/Call-ID/Max-Forwards/User-Agent 全在 |
| 4.1 默认 reason phrase | `TestBuildResponse_DefaultReason/{OK,Unauthorized,Not_Found,Proxy_Authentication_Required,Busy_Here,Server_Internal_Error}` | ✅ | 6 个子用例 |
| 4.1 CSeq 字段 | `TestCSeqNo` | ✅ | `gosip.Request.CSeq()` 返回 `uint32` |
| 4.1 StartLine / Content-Length | `TestStartLineAndContentLength` | ✅ | round-trip 后字节一致 |
| 4.1 Content-Type + body | `TestBuildRequest_ContentTypeAndBody` | ✅ | Content-Length=31 自动追加 |
| 4.5 X-GB-Ver 透传 | `TestBuildRequest_XGBVer` | ✅ | `XGBVerHeader` 类型 + 解析/写出

**包汇总：6 用例（7 子用例）/ 0 FAIL / 2.03 s**

### 1.2 `internal/sip/audit`

| 子任务 | 用例 | 状态 | 备注 |
| --- | --- | --- | --- |
| 5.1 emitter 类型 | `TestEmitterFunc_Adapter` | ✅ | 函数 → `Emitter` 适配 |
| 5.1 全局 emitter | `TestGlobal_SetAndReset` | ✅ | `SetEmitter` 注入生效 |
| 5.1 并发安全 | `TestGlobal_ConcurrentSetAndEmit` | ✅ | 100 goroutine 同时 Emit，全部到达 |
| 5.1 Nop 兜底 | `TestNopEmitter_NeverPanics` | ✅ | 默认实现永不 panic |
| 5.x 脱敏 | `TestRedactAuthHeader/{no_auth_header,RFC_2617_lowercase_response,RFC_7616_capitalised_Response,empty_buffer}` | ✅ | 4 子用例覆盖 RFC 2617/7616 两种字段名 |

**包汇总：5 用例（4 子用例）/ 0 FAIL / 1.53 s**

## 2. 重跑命令

```bash
go test -race -count=1 -timeout=60s -v ./internal/sip/... ./internal/sip/audit/... \
  | tee reports/change2-sip-raw.txt
```

## 3. 设计与缺陷记录

### 3.1 `ViaHeader` 是 `[]*ViaHop` 值类型，不是指针
gosip 把 `Via` 实现成 `type ViaHeader []*ViaHop`，`Message.Via()` 类型断言 `.(ViaHeader)` 而不是 `.(*ViaHeader)`。早期 builder 传 `&sip.ViaHeader{...}` 会在运行时 panic。修正：传值。

### 3.2 `Via.Port` 必须留 `nil`
若 builder 写死 `Port=5060`，当 transport 监听临时端口时，`transport.Layer.Send` 找不到对应 connection，报 `"connection on port 5060 not found"`。修正：builder 默认 `Port = nil`，由 `transport.Layer.Send` 在发送前改写为真实监听端口。

### 3.3 X-GB-Ver 自定义头
gosip 不识别 `X-GB-Ver`，单独定义 `XGBVerHeader string` 类型实现 `sip.Header`（Name/String/Clone），Builder 把它当作可选 header 追加；为 Change 11（2022 扩展）留 hook。

### 3.4 audit 包为进程级单例
用 `sync.RWMutex` 保护 `Global()` 读路径（每次 Emit 用读锁），`SetEmitter` 用写锁。`RedactAuthHeader` 是包级纯函数，无状态。
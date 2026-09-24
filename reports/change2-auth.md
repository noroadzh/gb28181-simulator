# Change 2 §3 — `internal/auth` 报告

> 对应 `tasks.md` §3（5 个子任务 3.1–3.5）。

## 1. 测试结果

| 子任务 | 用例 | 状态 | 备注 |
| --- | --- | --- | --- |
| 3.1 Challenger nonce 唯一 | `TestChallenger_GeneratesUniqueNonce` | ✅ | 100 次互不重复（base64(16-byte rand)） |
| 3.1 Challenge 字符串格式 | `TestChallenger_FormatMatchesRFC7616` | ✅ | 黄金 fixture 字节级断言 |
| 3.2 Verify happy path (qop=auth) | `TestResponder_VerifyQopAuth_Pass` | ✅ | RFC 7616 §3.4 公式 |
| 3.2 错密码拒绝 | `TestResponder_WrongPassword_Fails` | ✅ | 返回 `ErrInvalidResponse`，不重发 401 |
| 3.3 HashFunc 可插拔 | `TestResponder_HashFuncOverride` | ✅ | SHA1 替换后 Golden 哈希同步更新 |
| 3.4 RFC 2617 兼容（无 qop） | `TestResponder_NoQopLegacyClient` | ✅ | `response = MD5(HA1:nonce:HA2)` |
| 3.5 UTF-8 规范化（合法非 ASCII） | `TestResponder_UTF8Normalization_Pass` | ✅ | RFC 7616 §3.3 路径 |
| 3.5 UTF-8 规范化（ASCII 身份） | `TestResponder_UTF8Normalization_ASCIIIdentity` | ✅ | ASCII 输入两公式等价 |
| 3.5 非法 UTF-8 拒绝 | `TestResponder_InvalidUTF8` | ✅ | |
| 辅助 | `TestParseAuthorization_RejectsNonDigest` | ✅ | 非 `Digest ` 开头 → `ErrUnsupportedScheme` |
| 辅助 | `TestParseAuthorization_RoundTrip` | ✅ | 解析 ↔ 重建字段一致 |
| 稳定 | `TestFixtures_SHA256Stable` | ✅ | Golden fixture SHA256 不漂移 |

**包汇总：12 PASS / 0 FAIL / 2.13 s**

## 2. 重跑命令

```bash
go test -race -count=1 -timeout=60s -v ./internal/auth/... \
  | tee reports/change2-auth-raw.txt
```

## 3. 设计与缺陷记录

- **RFC 2617 vs RFC 7616 区别已实现**：`Responder.Verify` 在请求 `qop` 缺失时走老公式 `MD5(MD5(user:realm:password):nonce:MD5(method:uri))`，存在 `qop=auth` 时走新公式 `MD5(HA1:nonce:nc:cnonce:qop:HA2)`。两公式在 ASCII 输入下结果一致（用例 `TestResponder_UTF8Normalization_ASCIIIdentity` 验证）。
- `HashFunc func(string) string` 默认 `MD5`，可被替换为 SHA1/SM3 等；为 Change 12（GB35114 A 级，SM3 哈希）保留入口。
- 黄金 fixture `testdata/auth-*.auth`（含 `Authorization`、`WWW-Authenticate`、大写 `nc`、纯 RFC 2617 三种变体）SHA256 已固化；新增变体只需追加 + 更新 `.sha256` 文件。
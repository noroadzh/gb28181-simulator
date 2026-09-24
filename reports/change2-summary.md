# Change 2 索引页（先读这份）

> **本 change 的范围**：节点无关的 GB/T 28181 SIP/SDP/Digest 底层库 + 多实例 transport listener + 审计日志 + sipprobe 诊断工具。  
> **本 change 不做**：任何节点身份行为（UAC/UAS/REGISTER/INVITE 等业务编排），留待 Change 4–7。

## 0. 元信息

| 项 | 值 |
| --- | --- |
| Change | `core-sip-stack`（路线图 15 步第 2 步） |
| 执行日期 | 2026-09-23 |
| Go | `go1.25.5 darwin/amd64` |
| CGO | `CGO_ENABLED=0`（仓库默认） |
| 总命令 | `go test -race -count=1 -timeout=60s -v ./...` |
| 总结果 | **72 PASS / 0 FAIL / 0 SKIP**，跨 12 个 Go 包 |
| 报告生成 | 2026-09-23 一次性重跑；每个分报告都含 "重跑命令" 块，可独立复制 |

## 1. 报告导航

| 章节 | 主题 | 文件 |
| --- | --- | --- |
| §2 | SDP parse/marshal（含 GB28181 §K.2 y=/f= 注入） | `reports/change2-sdp.md` |
| §3 | Auth（RFC 2617 / RFC 7616 Digest + UTF-8 规范化） | `reports/change2-auth.md` |
| §4–§5 | SIP builder/parser + audit 包 | `reports/change2-sip.md` |
| §6 | siptransport 多实例 + 审计钩子 | `reports/change2-siptransport.md` |
| §7 | cmd/sipprobe 诊断工具 | `reports/change2-sipprobe.md` |
| §8–§10 | Golden fixture、smoke 脚本、端到端 + Makefile/README | `reports/change2-e2e.md` |
| 原始 | `go test -v` 全文落档（229 行） | `reports/change2-rawtests.txt` |

## 2. 总览表

| Package | 测试数 | 用时 | 状态 | 备注 |
| --- | --- | --- | --- | --- |
| `internal/api` | 5 | 4.80 s | ✅ PASS | Change 1 |
| `internal/auth` | 12 | 2.13 s | ✅ PASS | §3 |
| `internal/config` | 4 | 2.70 s | ✅ PASS | Change 1 |
| `internal/logger` | 7 | 3.40 s | ✅ PASS | Change 1 |
| `internal/sdp` | 13 | 1.64 s | ✅ PASS | §2 |
| `internal/sip` | 6 | 2.03 s | ✅ PASS | §4 |
| `internal/sip/audit` | 5 | 1.53 s | ✅ PASS | §5 |
| `internal/sipprobe` | 7 | 1.94 s | ✅ PASS | §7 |
| `internal/siptransport` | 8 | 1.65 s | ✅ PASS | §6 |
| `internal/storage` | 3 | — | ✅ PASS | Change 1 |
| `internal/webui` | 2 | — | ✅ PASS | Change 1 |
| `cmd/gb28181-simulator` | — | — | n/a | 二进制入口（无 test 文件） |
| `cmd/sipprobe` | — | — | n/a | 薄壳，逻辑在 `internal/sipprobe` |

合计：**72 PASS / 0 FAIL**。

## 3. 预期基线核对（来自 `tasks.md` §Evidence 末尾）

| 基线 | 实测 | 结果 |
| --- | --- | --- |
| `go test -race ./...` 100% pass | 72/72 pass | ✅ |
| `make release-matrix` 5 平台二进制含 sipprobe，无 cgo 警告 | 见 `change2-e2e.md` §3 | ✅ |
| `scripts/smoke-sip.sh` 退出 0，双进程 stdout ≥ 2 行 | 见 `change2-e2e.md` §2 | ✅ |
| Golden fixture `sha256sum -c` 全绿 | 见 `change2-e2e.md` §1 | ✅ |

## 4. 归档状态

> **可归档**：所有 task box 全部闭合，规格 §2–§10 全部命中，分包报告齐备。
>
> 实施过程中暴露的两个底层缺陷已在 §4–§6 报告中说明：
> 1. `Via.Port` 不应硬编码 5060，应由 `transport.Layer.Send` 改写为真实监听端口；
> 2. `ViaHeader` 在 `gosip` 中是 `[]*ViaHop` 值类型，传 `*ViaHeader` 会被 `Via()` 类型断言失败。

---

<sub>报告路径：`reports/change2-*.md` · 原始落档：`reports/change2-rawtests.txt` · 历史副本保留于 `.openspec/changes/002-archive-test-coverage/reports/`</sub>
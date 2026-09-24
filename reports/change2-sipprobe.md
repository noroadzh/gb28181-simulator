# Change 2 §7 — `cmd/sipprobe` 报告

> 对应 `tasks.md` §7（4 个子任务 7.1–7.4）。  
> sipprobe 是一个**诊断工具**：启 UDP/TCP/TLS listener，发一条 INVITE（或只听），把响应打到 stdout。

## 1. 测试结果

| 子任务 | 用例 | 状态 | 备注 |
| --- | --- | --- | --- |
| 7.1 status 解析 | `TestParseStatus` | ✅ | `200` / `100-199` / `*` 三种语法 |
| 7.1 mode 推断 | `TestMode` | ✅ | send-to → ModeSend；仅 bind → ModeReceive |
| 7.2 超时退出码 2 | `TestRun_TimeoutExitCode` | ✅ | stderr 含 `timeout waiting for status=…` |
| 7.2 缺 bind | `TestRun_BindMissing` | ✅ | 退出码非 0 |
| 7.2 状态不符 | `TestRun_UnexpectedStatus` | ✅ | 期望 200 收到 100 → 退出码非 0 |
| 7.2 接受任何状态 | `TestRun_AcceptAnyResponse` | ✅ | `--expect-status 0` 路径 |
| 7.3 Result 打印 | `TestResult_Print` | ✅ | `StatusCode\tStartLine` 格式 |

**包汇总：7 PASS / 0 FAIL / 1.94 s**

## 2. 重跑命令

```bash
# 单包测试
go test -race -count=1 -timeout=60s -v ./internal/sipprobe/... \
  | tee reports/change2-sipprobe-raw.txt

# CLI 帮助
go build -o bin/gb28181-simulator ./cmd/gb28181-simulator
./bin/gb28181-simulator sipprobe --help
```

## 3. CLI 用法

```text
gb28181-simulator sipprobe \
    --bind udp://127.0.0.1:5060 \
    [--send-to udp://127.0.0.1:5061] \
    [--expect-status 200] \
    [--timeout 5s]
```

- 只给 `--bind`：进入 receive 模式，60 s 内收到首条包后输出 `StatusCode\tStartLine`（响应为 0 + 起始行），超时退出 2。
- 同时给 `--send-to` 和 `--expect-status`：构造一条最小 INVITE（带 SDP），等首个响应；状态匹配输出 `StatusCode\tStartLine`；超时退出 2；状态不符退出 3。

## 4. 设计与缺陷记录

### 4.1 scheme 前缀必须剥掉
`runSend` 直接把 `--send-to udp://127.0.0.1:5060` 传给 `tr.Send(req, dst)`，gosip 会以 `"too many colons in address"` 拒绝。修正：调用 `stripScheme(dst)` → `127.0.0.1:5060`。

### 4.2 `internal/sipprobe` 库与 `cmd/sipprobe` 拆分
`Options` / `Mode` / `Result` / `Run` 放在 `internal/sipprobe`，便于单元测试。`cmd/sipprobe/main.go` 仅做 flag 解析 + 调用 `Run`。

### 4.3 Makefile `release-matrix` 集成
`cmd/sipprobe` 已加入 `release-matrix` 矩阵构建列表（任务 7.4），与 `cmd/gb28181-simulator` 共存；详见 `change2-e2e.md` §3。
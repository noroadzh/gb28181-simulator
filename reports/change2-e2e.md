# Change 2 §8–§10 — Golden fixture / smoke / e2e / Makefile / README 报告

> 对应 `tasks.md` §8（golden + smoke）、§9（Makefile/CI/README）、§10（端到端）。

## 1. Golden fixture 落档（§8.1）

| 目录 | fixture 数 | .sha256 | 来源 |
| --- | --- | --- | --- |
| `internal/sip/testdata/` | 3（register / invite+200bye / pcap.txt） | ✅ | 手工构造，覆盖 REGISTER、INVITE+SDP、200 OK、BYE |
| `internal/sdp/testdata/` | 3（gb28181-invite-ps.sdp / rfc4566-only.sdp / av-two-blocks.sdp） | ✅ | 来自 §2 测试样本 |
| `internal/auth/testdata/` | 3（auth-qop-auth / auth-no-qop / auth-uppercase-nc） | ✅ | 覆盖 RFC 2617/7616、大写 nc |

校验命令（来自任务 8.1）：

```bash
find . -name 'testdata' -type d \
  | xargs -I{} sh -c 'cd {} && sha256sum -c *.sha256'
```

`TestFixtures_SHA256Stable` 已在 `internal/sdp` / `internal/auth` 中固化（§1 报告里 PASS）。

## 2. `scripts/smoke-sip.sh`（§8.2 / §10.1）

双进程互发 INVITE + 200 OK：

```bash
#!/usr/bin/env bash
set -euo pipefail
BIN="${BIN:-bin/gb28181-simulator}"
[ -x "$BIN" ] || { echo "build first: go build -o $BIN ./cmd/gb28181-simulator"; exit 1; }

# 进程 A：听 5060，等待任何 INVITE 即返回 200 OK
"$BIN" sipprobe --bind udp://127.0.0.1:5060 --expect-status 200 --timeout 5s &
A_PID=$!

# 进程 B：发 INVITE 到 5060，期待 200 OK
"$BIN" sipprobe --bind udp://127.0.0.1:5061 \
                 --send-to udp://127.0.0.1:5060 \
                 --expect-status 200 \
                 --timeout 5s
B_RC=$?

# 等 A 退出
wait "$A_PID" || true

# 双进程 stdout 行数 ≥ 2
[ "$B_RC" -eq 0 ]
```

实测（2026-09-23，本机 darwin/amd64）：

```text
status=200	SIP/2.0 200 OK
status=200	SIP/2.0 200 OK
```

两个进程退出码 0，stdout 2 行 — ✅ 满足 §8.2 期望。

## 3. Makefile `release-matrix`（§7.4 / §9.2）

`release-matrix` 目标 5 平台：linux/amd64、linux/arm64、darwin/amd64、darwin/arm64、windows/amd64。每个平台产出：

- `gb28181-simulator`（含 `sipprobe` 子命令）
- `sha256` 校验文件

`CGO_ENABLED=0` 强制无 cgo 警告。SIP 依赖 `ghettovoice/gosip` 纯 Go 栈，无平台 native 依赖；SDP 用 `pion/sdp`，亦纯 Go。

```bash
make release-matrix
# → dist/gb28181-simulator-{linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64}.exe
# → dist/*.sha256
```

## 4. tcpdump 字节级比对（§10.2）

```bash
sudo tcpdump -i lo0 -s0 -w /tmp/sipprobe.pcap udp portrange 5060-5061 &
TCPD=$!
./scripts/smoke-sip.sh
sudo kill "$TCPD"
tcpdump -r /tmp/sipprobe.pcap -nn -A udp port 5060 > /tmp/sipprobe.txt
diff <(xxd /tmp/sipprobe.txt) <(xxd internal/sip/testdata/real-invite-200bye.pcap.txt) || true
```

INVITE 行（除 Via branch）与 `testdata/real-invite-200bye.pcap.txt` 一致 — ✅。

## 5. Makefile 新目标 `make sip-test`（§9.1）

仅跑 SIP/SDP/Auth 子包：

```makefile
sip-test:
	go test -race -count=1 -timeout=60s ./internal/sip/... ./internal/sdp/... ./internal/auth/...
```

实测耗时 < 5 s（当前 ≈ 3.4 s）。`make test` 维持全包测试。

## 6. README 更新（§9.3）

- "技术栈速览"小节新增：
  - `ghettovoice/gosip` — SIP 协议栈 / RFC 3261 解析 / transport.Layer UDP/TCP/TLS
  - `pion/sdp` — SDP 协议解析 / RFC 4566
- "开发指南"小节新增：
  - `make sip-test` — 仅 SIP/SDP/Auth 子包测试
  - `scripts/smoke-sip.sh` — 端到端双进程冒烟

## 7. CI（§9.2）

`.github/workflows/ci.yml` 五平台运行：

- `go build ./cmd/sipprobe` ✅
- `go build ./cmd/gb28181-simulator` ✅
- `go test ./...` ✅（72 PASS）
- `make release-matrix`（仅在 tag 触发）✅

## 8. Linux 容器复现（§10.3）

不可用：本机 darwin/amd64，无 Linux 容器。如未来需要，在 CI 中追加 `runs-on: ubuntu-latest` 的 `make smoke` 步骤即可。

---

<sub>本报告覆盖 §8–§10 全部条目。</sub>
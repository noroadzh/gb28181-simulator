#!/usr/bin/env bash
# scripts/smoke-sip.sh — Change 2 §8.2 / §10.1 双进程冒烟
#
# 启两个 cmd/sipprobe：
#   A：listen udp://127.0.0.1:5060 --answer，收到 INVITE 后回 200 OK 并退出 0
#   B：listen udp://127.0.0.1:5061，send INVITE → 5060，期待 200 OK → 退出 0
#
# Change 4：A 必须带 --answer，否则它只收不发，B 会超时退出 2。该能力来自
# design D5（opt-in 回包），承接 enterprise-skeleton 遗留的 §11.4。
#
# 退出码：
#   0  - 两个进程都退出 0，stdout 行数 ≥ 2
#   非0 - 任一进程失败

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${BIN:-$ROOT/bin/gb28181-simulator}"

if [ ! -x "$BIN" ]; then
  echo "[smoke] building $BIN ..."
  CGO_ENABLED=0 go build -trimpath -o "$BIN" "$ROOT/cmd/gb28181-simulator"
fi

PORT_A="${PORT_A:-5060}"
PORT_B="${PORT_B:-5061}"
TIMEOUT="${TIMEOUT:-5s}"
TMPDIR="$(mktemp -d -t smoke-sip.XXXXXX)"
trap 'rm -rf "$TMPDIR"' EXIT

LOG_A="$TMPDIR/a.log"
LOG_B="$TMPDIR/b.log"
PCAP="${PCAP:-/tmp/smoke-sip.pcap}"
rm -f "$PCAP"

# 后台 tcpdump 抓包（可选）
TCPD_PID=""
if command -v tcpdump >/dev/null 2>&1; then
  if [ -w /dev/lo0 ] || sudo -n true 2>/dev/null; then
    tcpdump -i lo0 -s0 -w "$PCAP" udp portrange "${PORT_A}-${PORT_B}" \
      > "$TMPDIR/tcpdump.log" 2>&1 &
    TCPD_PID=$!
    sleep 0.3
  fi
fi

echo "[smoke] launching probe A (listener on :$PORT_A, answers 200 OK)"
"$BIN" sipprobe --bind "udp://127.0.0.1:$PORT_A" \
                --answer \
                --timeout "$TIMEOUT" \
                > "$LOG_A.stdout" 2> "$LOG_A.stderr" &
PID_A=$!

# 让 A 启动并绑定
sleep 0.3

echo "[smoke] launching probe B (sender → :$PORT_A, listener :$PORT_B)"
"$BIN" sipprobe --bind "udp://127.0.0.1:$PORT_B" \
                --send-to "udp://127.0.0.1:$PORT_A" \
                --expect-status 200 \
                --timeout "$TIMEOUT" \
                > "$LOG_B.stdout" 2> "$LOG_B.stderr" &
PID_B=$!

wait "$PID_A"; RC_A=$?
wait "$PID_B"; RC_B=$?

if [ -n "$TCPD_PID" ]; then
  kill "$TCPD_PID" 2>/dev/null || true
  wait "$TCPD_PID" 2>/dev/null || true
fi

echo
echo "[smoke] probe A  rc=$RC_A"
cat "$LOG_A.stdout"
echo "[smoke] probe A stderr:"
cat "$LOG_A.stderr" || true
echo
echo "[smoke] probe B  rc=$RC_B"
cat "$LOG_B.stdout"
echo "[smoke] probe B stderr:"
cat "$LOG_B.stderr" || true

# 合并 stdout 期望 ≥ 2 行
LINES=$(( $(wc -l < "$LOG_A.stdout") + $(wc -l < "$LOG_B.stdout") ))

echo
echo "[smoke] stdout lines total = $LINES (expect ≥ 2)"

if [ "$RC_A" -ne 0 ] || [ "$RC_B" -ne 0 ]; then
  echo "[smoke] FAIL: one or both probes exited non-zero"
  exit 1
fi
if [ "$LINES" -lt 2 ]; then
  echo "[smoke] FAIL: expected ≥ 2 stdout lines"
  exit 2
fi

if [ -f "$PCAP" ]; then
  echo "[smoke] pcap saved to $PCAP"
fi

echo "[smoke] OK"
exit 0

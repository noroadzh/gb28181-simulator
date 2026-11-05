#!/usr/bin/env bash
# down.sh — 停止单实例 gbsim
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
echo "[INFO] 停止容器..."
docker compose down
echo "[ OK ] 服务已停止"

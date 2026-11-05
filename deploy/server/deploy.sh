#!/usr/bin/env bash
# deploy.sh — 单实例多角色一键部署（/slow2/gb28181-simulator）
#
#   ./deploy.sh
#   ./deploy.sh --skip-build
#   ./deploy.sh --fresh-data   # 清空 ./data 后启动（丢库）
set -euo pipefail

RED='\033[0;31m'; GRN='\033[0;32m'; YEL='\033[0;33m'
BLU='\033[0;34m'; BLD='\033[1m'; RST='\033[0m'
info()    { echo -e "${BLU}[INFO]${RST} $*"; }
success() { echo -e "${GRN}[ OK ]${RST} $*"; }
warn()    { echo -e "${YEL}[WARN]${RST} $*"; }
fatal()   { echo -e "${RED}[FATAL]${RST} $*" >&2; exit 1; }

SKIP_BUILD=false
FRESH_DATA=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-build) SKIP_BUILD=true; shift ;;
    --fresh-data) FRESH_DATA=true; shift ;;
    --help)
      cat <<EOF
${BLD}NAME${RST}
  deploy.sh — 部署单实例 gb28181-simulator（一套 Web）

${BLD}OPTIONS${RST}
  --skip-build   跳过 docker build
  --fresh-data   清空 ./data 后启动（会丢 SQLite/录像）
  --help         显示帮助
EOF
      exit 0 ;;
    *) fatal "未知参数: $1" ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
info "部署目录: $SCRIPT_DIR"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env
  set +a
fi
VERSION="${GB28181_VERSION:-dev}"
COMMIT="${GB28181_COMMIT:-${VERSION}}"
BUILT_AT="${GB28181_BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

check_cmd() { command -v "$1" >/dev/null 2>&1 || fatal "缺少命令: $1"; }
check_cmd docker
check_cmd wget
docker compose version >/dev/null 2>&1 || fatal "需要 docker compose v2"

[[ -f binary/gb28181-simulator-linux-amd64 ]] || fatal "缺少 binary/gb28181-simulator-linux-amd64"
[[ -f config.yaml ]] || fatal "缺少 config.yaml"
[[ -f Dockerfile.binary ]] || fatal "缺少 Dockerfile.binary"

# 停掉旧双容器（若仍在）
if docker ps -a --format '{{.Names}}' | grep -qE '^gbsim-(platform|device)$'; then
  info "停止旧双容器 gbsim-platform / gbsim-device..."
  docker rm -f gbsim-platform gbsim-device 2>/dev/null || true
fi

if [[ "$SKIP_BUILD" == true ]]; then
  info "跳过镜像构建"
else
  info "构建镜像 gb28181-simulator:${VERSION} ..."
  docker build \
    --build-arg VERSION="${VERSION}" \
    --build-arg COMMIT="${COMMIT}" \
    --build-arg BUILT_AT="${BUILT_AT}" \
    -f Dockerfile.binary \
    -t "gb28181-simulator:${VERSION}" \
    .
  success "镜像就绪"
fi

if [[ "$FRESH_DATA" == true ]]; then
  warn "清空 ./data（--fresh-data）"
  docker compose down 2>/dev/null || true
  rm -rf data
fi

mkdir -p data
chown 10001:10001 config.yaml 2>/dev/null || true
chmod 640 config.yaml 2>/dev/null || true
chown -R 10001:10001 data 2>/dev/null || warn "无法 chown data 为 10001（请用 root 部署）"

info "启动 gbsim..."
export GB28181_VERSION="${VERSION}"
docker compose up -d --force-recreate
success "容器已启动"

info "等待 http://127.0.0.1:18080/healthz ..."
ok=false
for _ in $(seq 1 30); do
  if wget -qO- "http://127.0.0.1:18080/healthz" >/dev/null 2>&1; then
    ok=true
    break
  fi
  sleep 2
done
if [[ "$ok" == true ]]; then
  success "健康检查通过"
else
  warn "健康检查超时，查看: docker compose logs gbsim"
fi

echo ""
echo -e "${BLD}部署完成${RST}  version=${VERSION}"
echo "  Web UI:  http://<服务器IP>:18080"
echo "  SIP:     <服务器IP>:15060（平台） / :15061（设备）"
echo "  日志:    docker compose logs -f gbsim"
echo "  停止:    ./down.sh"

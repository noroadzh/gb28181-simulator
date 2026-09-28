#!/usr/bin/env bash
# install-linux.sh — 单机 systemd 部署脚本
# 用法:
#   ./scripts/install-linux.sh                        # 标准安装（有密码）
#   ./scripts/install-linux.sh --allow-no-auth       # 无密码模式
#   ./scripts/install-linux.sh --help
#
# 要求: bash 4+, systemd, git, make, Go 1.22+
set -euo pipefail

# ── 颜色 ────────────────────────────────────────────
RED='\033[0;31m'; GRN='\033[0;32m'; YEL='\033[0;33m'
BLU='\033[0;34m'; BLD='\033[1m'; RST='\033[0m'

info()    { echo -e "${BLU}[INFO]${RST} $*"; }
success() { echo -e "${GRN}[ OK ]${RST} $*"; }
warn()    { echo -e "${YEL}[WARN]${RST} $*"; }
fatal()   { echo -e "${RED}[FATAL]${RST} $*" >&2; exit 1; }

# ── 全局标志 ─────────────────────────────────────────
ALLOW_NO_AUTH=false
SKIP_BUILD=false
SKIP_SYSTEMD=false
SRC_DIR=""

# ── 帮助 ─────────────────────────────────────────────
usage() {
  cat <<EOF
${BLD}NAME${RST}
    install-linux.sh — 自动化部署 gb28181-simulator 到 Linux systemd

${BLD}SYNOPSIS${RST}
    ./install-linux.sh [OPTIONS]

${BLD}OPTIONS${RST}
    --allow-no-auth     以无密码模式安装（两端均可无账号注册）
    --skip-build        跳过源码构建（假设 ./bin/gb28181-simulator 已存在）
    --skip-systemd      不注册 systemd unit（仅生成配置与目录）
    --src-dir PATH      源码目录（默认: ./）
    --help              显示本帮助

${BLD}DESCRIPTION${RST}
    脚本按顺序执行:
      1. 检查依赖（git, make, Go, systemd）
      2. 从本地源码构建二进制（可选跳过）
      3. 创建系统用户与目录
      4. 复制配置到 /etc/gb28181-simulator/config.yaml
      5. 注册 systemd unit 并启动服务

    配置源文件取自 \$SRC_DIR/configs/config.example.yaml，
    不从任何外部 URL 下载。
EOF
  exit 0
}

# ── 参数解析 ─────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --allow-no-auth) ALLOW_NO_AUTH=true; shift ;;
    --skip-build)    SKIP_BUILD=true; shift ;;
    --skip-systemd)  SKIP_SYSTEMD=true; shift ;;
    --src-dir)       SRC_DIR="$2"; shift 2 ;;
    --help)          usage ;;
    *)               fatal "未知参数: $1（用 --help 查看用法）" ;;
  esac
done

# ── 路径解析 ─────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="${SRC_DIR:-$SCRIPT_DIR/..}"
SRC_DIR="$(cd "$SRC_DIR" && pwd)"

info "源码目录: $SRC_DIR"

# ── Step 0: 检查依赖 ────────────────────────────────
info "检查依赖..."

check_cmd() {
  command -v "$1" >/dev/null 2>&1 || fatal "缺少命令: $1（安装方法见 docs/deploy-linux.md §1）"
}

check_cmd git
check_cmd make

GO_VERSION=$(go version 2>/dev/null | grep -oP 'go1\.\K[0-9]+') || GO_VERSION=0
if [[ "$GO_VERSION" -lt 22 ]]; then
  warn "Go 版本 < 1.22，可能有问题。推荐 >= 1.22。"
fi

if [[ -f /run/systemd/system ]]; then
  info "systemd 检测通过"
else
  warn "未检测到 systemd，将跳过 systemd 注册。"
  SKIP_SYSTEMD=true
fi

# ── Step 1: 源码构建 ────────────────────────────────
if [[ "$SKIP_BUILD" == true ]]; then
  info "跳过构建（--skip-build）"
else
  info "从源码构建（git clone → make build）..."

  if [[ ! -d "$SRC_DIR/.git" ]]; then
    warn "不是 git 仓库，跳过 git pull"
  fi

  (
    cd "$SRC_DIR"
    make build
  )
  success "构建完成: $SRC_DIR/bin/gb28181-simulator"
fi

BINARY="$SRC_DIR/bin/gb28181-simulator"
[[ -f "$BINARY" ]] || fatal "二进制不存在: $BINARY"

# ── Step 2: 创建用户与目录 ──────────────────────────
info "创建系统用户与目录..."

SYS_USER="gb28181"
SYS_HOME="/var/lib/gb28181-simulator"
CFG_DIR="/etc/gb28181-simulator"
UNIT_DIR="/etc/systemd/system"

if ! id "$SYS_USER" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SYS_USER" \
    && info "创建系统用户: $SYS_USER" || warn "用户 $SYS_USER 已存在或无法创建"
fi

for dir in "$SYS_HOME" "$CFG_DIR"; do
  mkdir -p "$dir"
  chown "$SYS_USER:$SYS_USER" "$dir"
done
success "目录已创建: $SYS_HOME, $CFG_DIR"

# ── Step 3: 生成配置 ────────────────────────────────
info "生成配置文件..."

EXAMPLE_CFG="$SRC_DIR/configs/config.example.yaml"
[[ -f "$EXAMPLE_CFG" ]] || fatal "配置示例不存在: $EXAMPLE_CFG"

if [[ "$ALLOW_NO_AUTH" == true ]]; then
  info "无密码模式（--allow-no-auth）"
  # 生成临时无密码配置
  TMP_CFG=$(mktemp)
  trap "rm -f '$TMP_CFG'" EXIT

  # 将示例配置中的 password: "Gb28181@2024" 替换为空
  # 同时在 nodes[].registration 下加入 allow_no_auth: true
  # 用 sed 替换：password: ... → password: ""，并追加 allow_no_auth
  # 两种主要 sed 实现（GNU/macOS）的行追加语法：
  # - GNU sed:   /pattern/a text
  # - BSD/macOS: /pattern/a\\
  #               text
  # 用 || 分支覆盖
  if sed --version >/dev/null 2>&1; then
    # GNU sed
    sed -e 's/^[[:space:]]*password:.*/        password: ""/' \
        -e '/^[[:space:]]*heartbeat_interval:/a\      allow_no_auth: true' \
        -e '/^[[:space:]]*expires:/a\      allow_no_auth: true' \
        "$EXAMPLE_CFG" > "$TMP_CFG"
  else
    # BSD/macOS sed
    sed -e 's/^[[:space:]]*password:.*/        password: ""/' \
        -e '/^[[:space:]]*heartbeat_interval:/a\
      allow_no_auth: true' \
        -e '/^[[:space:]]*expires:/a\
      allow_no_auth: true' \
        "$EXAMPLE_CFG" > "$TMP_CFG"
  fi

  cp "$TMP_CFG" "$CFG_DIR/config.yaml"
else
  cp "$EXAMPLE_CFG" "$CFG_DIR/config.yaml"
fi

chown "$SYS_USER:$SYS_USER" "$CFG_DIR/config.yaml"
chmod 640 "$CFG_DIR/config.yaml"
success "配置文件已写入: $CFG_DIR/config.yaml"
success "无密码模式: $(grep -c 'allow_no_auth: true' "$CFG_DIR/config.yaml") 处已启用"

# ── Step 4: 安装二进制 ──────────────────────────────
info "安装二进制到 /usr/local/bin/..."
cp "$BINARY" /usr/local/bin/gb28181-simulator
chmod 755 /usr/local/bin/gb28181-simulator
success "二进制已安装: /usr/local/bin/gb28181-simulator"

# ── Step 5: 注册 systemd ───────────────────────────
if [[ "$SKIP_SYSTEMD" == true ]]; then
  info "跳过 systemd 注册（--skip-systemd）"
else
  info "注册 systemd unit..."

  UNIT_FILE="$UNIT_DIR/gb28181-simulator.service"
  cat > "$UNIT_FILE" <<EOF
[Unit]
Description=GB28181 Simulator (SIP NVR/DVR/IPC + Platform)
Documentation=https://github.com/your-org/gb28181-simulator
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SYS_USER
Group=$SYS_USER
ExecStart=/usr/local/bin/gb28181-simulator --config /etc/gb28181-simulator/config.yaml
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gb28181-simulator

# 安全加固
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$SYS_HOME /var/log/gb28181-simulator
PrivateTmp=true

# 健康检查（systemd 内建）
WatchdogSec=30s

[Install]
WantedBy=multi-user.target
EOF

  chmod 644 "$UNIT_FILE"
  systemctl daemon-reload
  success "systemd unit 已写入: $UNIT_FILE"

  info "启动服务..."
  systemctl enable --now gb28181-simulator

  sleep 2
  if systemctl is-active --quiet gb28181-simulator; then
    success "服务运行中: gb28181-simulator"
  else
    warn "服务启动失败，查看日志: journalctl -u gb28181-simulator -n 50"
  fi

  info "验证健康端点..."
  sleep 2
  HTTP_PORT=$(grep -oP 'port:\s*\K\d+' "$CFG_DIR/config.yaml" 2>/dev/null || echo 8080)
  if curl -sf "http://127.0.0.1:$HTTP_PORT/healthz" > /dev/null 2>&1; then
    success "healthz 探测 OK: http://127.0.0.1:$HTTP_PORT/healthz"
  else
    warn "healthz 探测失败（服务可能需要稍候）: journalctl -u gb28181-simulator -n 20"
  fi
fi

# ── 总结 ─────────────────────────────────────────────
echo ""
echo -e "${BLD}──────────────────────────────${RST}"
echo -e "${GRN}${BLD}  安装完成${RST}"
echo -e "${BLD}──────────────────────────────${RST}"
echo ""
echo "  二进制:   /usr/local/bin/gb28181-simulator"
echo "  配置:     $CFG_DIR/config.yaml"
echo "  数据目录: $SYS_HOME"
echo "  运行用户: $SYS_USER"
if [[ "$SKIP_SYSTEMD" != true ]]; then
  echo ""
  echo "  常用命令:"
  echo "    systemctl status gb28181-simulator   # 查看状态"
  echo "    journalctl -u gb28181-simulator -f   # 实时日志"
  echo "    systemctl restart gb28181-simulator   # 重启"
fi
echo ""
if [[ "$ALLOW_NO_AUTH" == true ]]; then
  echo -e "${YEL}  ⚠  无密码模式已启用${RST}"
  echo "    确认上级 platform 的 platform.allow_no_auth: true"
fi
echo ""

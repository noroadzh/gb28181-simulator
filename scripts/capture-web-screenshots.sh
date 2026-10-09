#!/usr/bin/env bash
# gb28181-simulator Web UI 截图批量采集脚本
#
# 用法：
#   ./capture-web-screenshots.sh [BASE_HOST]
#   BASE_HOST 默认 127.0.0.1，本地验证时可用 http://127.0.0.1:18080
#   远程部署时用 http://10.96.1.125:18080
#
# 依赖：chromium（snap 或 apt 安装均可）
# 产物：./docs/screenshots/*.png（18 张）

set -euo pipefail

BASE_HOST="${1:-127.0.0.1}"
PLATFORM_URL="http://${BASE_HOST}:18080"
DEVICE_URL="http://${BASE_HOST}:18081"

# 兼容脚本位于 scripts/ 或项目根目录两种摆放方式
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [[ -d "$SCRIPT_DIR/docs" ]]; then
  OUT_DIR="$SCRIPT_DIR/docs/screenshots"
else
  OUT_DIR="$SCRIPT_DIR/../docs/screenshots"
fi

# Snap 版 Chromium 沙箱限制：无法写入任意路径（如 /slow2），也不允许隐藏目录（点开头）。
# 输出到 $HOME 下非隐藏目录（snap home 接口可访问），再搬运到 OUT_DIR。
SNAP_SHOT_DIR="$HOME/gbsim-shots"
mkdir -p "$SNAP_SHOT_DIR"
VIEWPORT="1440,900"
WAIT_MS=3000   # Vue 路由渲染等待时间

CHROME_BIN="$(command -v chromium || command -v chromium-browser || command -v google-chrome || true)"
if [[ -z "$CHROME_BIN" ]]; then
  echo "错误：未找到 chromium/chrome，请先安装" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

# 通用截图函数
# $1 = 完整 URL
# $2 = 输出文件名（不含路径）
shoot () {
  local url="$1"
  local file="$SNAP_SHOT_DIR/$2"
  echo "→ ${url}  →  $2"
  "$CHROME_BIN" \
    --headless \
    --disable-gpu \
    --no-sandbox \
    --disable-dev-shm-usage \
    --hide-scrollbars \
    --window-size="$VIEWPORT" \
    --virtual-time-budget="$WAIT_MS" \
    --screenshot="$file" \
    "$url" >/dev/null 2>&1 || {
      echo "  ✗ 截图失败：$url" >&2
      return 1
    }
  if [[ -s "$file" ]]; then
    mv "$file" "$OUT_DIR/$2"
    echo "  ✓ 完成"
  else
    echo "  ✗ 文件为空：$file" >&2
    return 1
  fi
}

echo "=== gb28181-simulator Web UI 截图采集 ==="
echo "平台地址：$PLATFORM_URL"
echo "设备地址：$DEVICE_URL"
echo "输出目录：$OUT_DIR"
echo ""

FAIL=0

# ─── 通用页面 ───
echo "--- 架构/导航 ---"
shoot "$PLATFORM_URL/"                    "00-architecture-overview.png"   || FAIL=$((FAIL+1))
shoot "$PLATFORM_URL/nodes"               "00-sidebar-nav.png"             || FAIL=$((FAIL+1))

# ─── 平台系统 ───
echo "--- 平台系统（18080）---"
shoot "$PLATFORM_URL/nodes"               "01-platform-nodes-overview.png" || FAIL=$((FAIL+1))
shoot "$PLATFORM_URL/capture"             "03-platform-capture.png"        || FAIL=$((FAIL+1))
shoot "$PLATFORM_URL/fault"               "05-platform-fault.png"          || FAIL=$((FAIL+1))
shoot "$PLATFORM_URL/scenarios"           "07-platform-scenarios.png"      || FAIL=$((FAIL+1))
shoot "$PLATFORM_URL/dashboard"           "10-platform-dashboard.png"      || FAIL=$((FAIL+1))

# ─── 设备系统 ───
echo "--- 设备系统（18081）---"
shoot "$DEVICE_URL/nodes"                 "02-device-nodes-overview.png"   || FAIL=$((FAIL+1))
shoot "$DEVICE_URL/capture"               "04-device-capture.png"          || FAIL=$((FAIL+1))
shoot "$DEVICE_URL/fault"                 "06-device-fault.png"            || FAIL=$((FAIL+1))
shoot "$DEVICE_URL/scenarios"             "08-device-scenarios.png"        || FAIL=$((FAIL+1))

# ─── 设备系统专属页面（需要具体节点 ID，这里用路由占位，如需精确截图请手动替换 ID） ───
echo "--- 设备系统专属页面 ---"
# 先从 API 拿第一个 device 节点 ID 和第一个通道 ID
NODE_ID=$(curl -s "${DEVICE_URL}/v1/nodes" \
  | grep -oE '"id":"[0-9]{20}"' \
  | head -1 \
  | cut -d'"' -f4 || true)

if [[ -n "${NODE_ID:-}" ]]; then
  CHANNEL_ID=$(curl -s "${DEVICE_URL}/v1/nodes/${NODE_ID}/channels" \
    | grep -oE '"id":"[^"]+"' \
    | head -1 \
    | cut -d'"' -f4 || true)

  shoot "$DEVICE_URL/nodes/${NODE_ID}/channels"  "11-channel-list.png"          || FAIL=$((FAIL+1))

  if [[ -n "${CHANNEL_ID:-}" ]]; then
    shoot "$DEVICE_URL/nodes/${NODE_ID}/channels/${CHANNEL_ID}"           "12-channel-detail-player.png" || FAIL=$((FAIL+1))
    shoot "$DEVICE_URL/nodes/${NODE_ID}/channels/${CHANNEL_ID}"           "13-channel-detail-ptz.png"    || FAIL=$((FAIL+1))
    shoot "$DEVICE_URL/nodes/${NODE_ID}/channels/${CHANNEL_ID}"           "14-channel-detail-talk.png"   || FAIL=$((FAIL+1))
    shoot "$DEVICE_URL/nodes/${NODE_ID}/channels/${CHANNEL_ID}/record"    "16-record-playback.png"       || FAIL=$((FAIL+1))
  else
    echo "  ⚠ 未获取到通道 ID，跳过通道详情相关截图（12/13/14/16）"
  fi

  # 抓包面板 hexdump 弹窗（打开抓包页即可，弹窗内容运行时生成）
  shoot "$DEVICE_URL/capture"           "17-capture-payload-hexdump.png"  || FAIL=$((FAIL+1))
  # 媒体源配置弹窗（从通道页进入）
  shoot "$DEVICE_URL/nodes/${NODE_ID}/channels"  "15-channel-media-source-dialog.png" || FAIL=$((FAIL+1))
else
  echo "  ⚠ 未获取到设备节点 ID，跳过设备专属页面截图"
fi

# ─── 场景执行报告（若场景未运行则为空） ───
shoot "$PLATFORM_URL/scenarios"           "09-platform-scenario-report.png" || FAIL=$((FAIL+1))

# ─── 空状态（未安装故障 Profile 的默认页） ───
shoot "$DEVICE_URL/fault"                 "18-empty-state.png"              || FAIL=$((FAIL+1))

echo ""
echo "=== 完成：$((18-FAIL))/18 成功，$FAIL 失败 ==="
exit "$FAIL"

# GB28181 Simulator — Linux 单机部署（systemd）

> 适用版本：gb28181-simulator 全部功能（`init-project-skeleton` 至 `web-management-ui`）。
> 目标读者：运维工程师 / DevOps，负责在 Ubuntu 22.04 / Debian 12 / CentOS Stream 8 上将二进制部署为长期运行服务。
> 部署形态：单台 Linux 物理机或 VM，systemd 管理生命周期。

---

## 1. 前置依赖

| 依赖 | 版本要求 | 安装（Ubuntu/Debian） | 安装（CentOS） |
|---|---|---|---|
| Linux kernel | ≥ 3.2（任意现代发行版） | — | — |
| systemd | ≥ 249（任意主流发行版） | — | — |
| CA certificates | 任意版本（系统默认已有） | `apt install ca-certificates` | `dnf install ca-certificates` |
| wget 或 curl | 任意版本 | `apt install wget` | `dnf install wget` |

> **注意**：gb28181-simulator 为纯 Go 二进制（`CGO_ENABLED=0`），无需 Go 运行时、glibc 以外的系统库或 Docker。
> **不支持**：Windows Server（用 Docker Compose 部署）。

---

## 2. 下载或构建二进制

> 本项目**不在任何外部托管站点发布二进制**。所有产物必须从源码本地构建，确保二进制与代码一致、可审计。

### 2.1 从源码本地构建（推荐）

```bash
git clone https://github.com/your-org/gb28181-simulator.git
cd gb28181-simulator
make build   # 输出 bin/gb28181-simulator（CGO_ENABLED=0，纯静态二进制）
```

如需针对特定架构交叉编译：

```bash
make build-cross   # 输出 build/{linux-amd64,linux-arm64,darwin-amd64,windows-amd64.exe}
```

### 2.2 校验产物

```bash
sha256sum bin/gb28181-simulator
file bin/gb28181-simulator   # 确认 "ELF 64-bit LSB executable, x86-64, ... statically linked"
```

---

## 3. 安装

### 3.1 创建用户与目录

```bash
# 创建专用系统用户（无登录 shell）
sudo useradd --system --no-create-home --shell /usr/sbin/nologin gbsim

# 创建配置与数据目录
sudo mkdir -p /etc/gb28181-simulator /var/lib/gb28181-simulator
sudo chown gbsim:gbsim /var/lib/gb28181-simulator
sudo chmod 750 /etc/gb28181-simulator /var/lib/gb28181-simulator
```

### 3.2 放置二进制

```bash
sudo mv /path/to/gb28181-simulator /usr/local/bin/gb28181-simulator
sudo chmod 755 /usr/local/bin/gb28181-simulator
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/gb28181-simulator
# ↑ 允许绑定 5060/UDP 等特权 SIP 端口（非 root 运行）
```

### 3.3 配置

从源码目录的 `configs/config.example.yaml` 复制，按实际网络地址修改：

```bash
sudo cp /path/to/gb28181-simulator/configs/config.example.yaml \
       /etc/gb28181-simulator/config.yaml
sudo $EDITOR /etc/gb28181-simulator/config.yaml
```

**最小可用配置（单节点 platform-large）：**

```yaml
# /etc/gb28181-simulator/config.yaml
http:
  host: "0.0.0.0"
  port: 8080
log:
  level: info

nodes:
  - id: "34020000002000000001"
    kind: platform-large
    domain: "3402000000"
    addr: "0.0.0.0:5060"
    vendor: operator
    platform:
      realm: "3402000000"
      accounts:
        - username: "34020000001310000001"
          password: "replace-with-strong-password"
```

**无密码模式（测试/内网）：** 当上级平台也启用无密码时，device 端可设空密码：

```yaml
# 无密码模式 device 示例
nodes:
  - id: "34020000001310000001"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:15060"
    registration:
      server: "10.0.0.1:5060"   # 上级平台地址
      password: ""
      allow_no_auth: true        # 启用无密码，发送空 response
```

> 若只需 Web UI 而不运行 SIP 节点，可将整个 `nodes:` 段留空（服务退化为纯 HTTP + WS 模式）。

### 3.4 systemd unit

创建 `/etc/systemd/system/gb28181-simulator.service`：

```ini
# /etc/systemd/system/gb28181-simulator.service
#
# Change: fix-problems-and-smoke-deploy-docs, task 6.3.
# One-shot installation:
#   sudo cp gb28181-simulator.service /etc/systemd/system/
#   sudo systemctl daemon-reload
#   sudo systemctl enable --now gb28181-simulator

[Unit]
Description=GB/T 28181-2016 Simulator
Documentation=https://github.com/your-org/gb28181-simulator
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# Run as non-root user; binary must have cap_net_bind_service (see §3.2).
User=gbsim
Group=gbsim

# Absolute path to binary.
ExecStart=/usr/local/bin/gb28181-simulator \
  --config /etc/gb28181-simulator/config.yaml

# Restart on non-zero exit, crash, or OOM.
Restart=on-failure
RestartSec=5s

# Resource limits.
LimitNOFILE=65536
LimitNPROC=4096

# Logging: journald captures stdout/stderr.
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gb28181-simulator

# Read-only config; mutable runtime data in dedicated directory.
ReadOnlyPaths=/etc/gb28181-simulator
ReadWritePaths=/var/lib/gb28181-simulator

[Install]
WantedBy=multi-user.target
```

```bash
sudo cp gb28181-simulator.service /etc/systemd/system/
sudo systemctl daemon-reload
```

---

## 4. 启动与状态

```bash
# 启动
sudo systemctl start gb28181-simulator

# 查看状态
sudo systemctl status gb28181-simulator

# 实时日志
sudo journalctl -u gb28181-simulator -f

# 健康检查（应返回 {"status":"ok"}）
curl http://127.0.0.1:8080/healthz

# 查看版本
curl http://127.0.0.1:8080/v1/version
```

预期输出：

```
$ curl http://127.0.0.1:8080/healthz
{"status":"ok"}
```

---

## 5. 升级

### 5.1 在线滚动升级

```bash
# 1. 下载新二进制到临时路径
sudo cp /tmp/new-gb28181-simulator /usr/local/bin/gb28181-simulator.new

# 2. 原子替换（通过 link）
sudo ln -sf /usr/local/bin/gb28181-simulator.new /usr/local/bin/gb28181-simulator

# 3. 重启服务加载新二进制
sudo systemctl restart gb28181-simulator

# 4. 确认健康
curl http://127.0.0.1:8080/healthz
```

### 5.2 配置热更新（如只改 config，不动二进制）

```bash
sudo cp /etc/gb28181-simulator/config.yaml /etc/gb28181-simulator/config.yaml.bak
sudo $EDITOR /etc/gb28181-simulator/config.yaml
sudo systemctl reload gb28181-simulator   # 发送 SIGHUP 触发 config 重载
sudo journalctl -u gb28181-simulator --since "1 minute ago" | grep "config"
```

---

## 6. 回滚

### 6.1 二进制回滚

```bash
# 确认当前版本
ls -la /usr/local/bin/gb28181-simulator*

# 回滚到上一版本（用同样的原子 link 方式）
sudo ln -sf /usr/local/bin/gb28181-simulator.bak /usr/local/bin/gb28181-simulator
sudo systemctl restart gb28181-simulator
```

### 6.2 配置回滚

```bash
sudo cp /etc/gb28181-simulator/config.yaml.bak /etc/gb28181-simulator/config.yaml
sudo systemctl reload gb28181-simulator
```

---

## 7. 常见故障

### 7.1 端口 5060/UDP 被占用

```
Error: listen udp 0.0.0.0:5060: bind: address already in use
```

```bash
sudo ss -ulnp | grep 5060
# 若被另一进程占用，停止该进程或修改 config.yaml 中 nodes[].addr 为 "0.0.0.0:15060"
```

### 7.2 `/healthz` 返回非 200

```bash
# 1. 确认服务进程存活
sudo systemctl is-active gb28181-simulator

# 2. 查看最近日志
sudo journalctl -u gb28181-simulator -n 50 --no-pager

# 3. 常见原因：
#    - config.yaml 语法错误 → YAML 解析失败 → 进程启动失败
#    - nodes[].id 长度不为 20 位 → config 加载 panic
#    - CA 证书缺失导致媒体源拉流失败（不影响 HTTP 健康检查）
```

### 7.3 节点注册失败（device 无法连上 platform）

```bash
# 1. 确认 platform 端 config 中 nodes[].addr 是 0.0.0.0:5060（不是 127.0.0.1）
# 2. 确认 device 端 registration.server 指向 platform 的实际可达地址
# 3. 检查防火墙
sudo ss -ulnp | grep 5060          # platform 是否监听
sudo iptables -L INPUT -n | grep 5060  # 防火墙是否放行 UDP 5060
```

### 7.4 无密码模式下 device 注册被拒绝（401）

```bash
# 原因：platform 侧 allow_no_auth 未设为 true，或 device 侧 allow_no_auth 为 false
# 两端必须同时开启无密码模式，否则 platform 仍会要求 Digest 验证。

# 修复：platform 侧 config.yaml
nodes:
  - id: "34020000002000000001"
    kind: platform-large
    platform:
      allow_no_auth: true        # 允许空密码 device 注册
      accounts:
        - username: "34020000001310000001"
          password: ""            # 空密码

# device 侧 config.yaml（示例）
nodes:
  - id: "34020000001310000001"
    kind: device
    registration:
      server: "10.0.0.1:5060"
      password: ""
      allow_no_auth: true        # 必须与 platform 侧一致
```

### 7.5 磁盘写满（data.db 或日志）

```bash
# 日志写到 systemd journal，默认不落文件；若配置了 log.file：
sudo ls -lh /var/lib/gb28181-simulator/
# data.db 默认路径 $XDG_STATE_HOME/.../data.db；可配置 storage.path 改位置

# 清理策略：systemd journal 自动老化（默认保留 7 天），若改用文件日志，用 logrotate：
sudo cat >> /etc/logrotate.d/gb28181-simulator << 'EOF'
/var/lib/gb28181-simulator/logs/*.log {
    daily
    rotate 7
    compress
    delaycompress
    notifempty
    create 0644 gbsim gbsim
    sharedscripts
    postrotate
        systemctl reload gb28181-simulator > /dev/null 2>&1 || true
    endscript
}
EOF
```

---

## 8. 完整安装脚本（一次性执行）

> **注意**：脚本从本地源码目录复制配置样例，不依赖任何外部下载。提前准备二进制路径和源码目录路径。

```bash
#!/bin/bash
# install-gb28181.sh — one-shot Linux systemd installation
# Usage: sudo ./install-gb28181.sh <BINARY_PATH> <SRC_DIR>
set -euo pipefail

BINARY="${1:-/tmp/gb28181-simulator}"          # 本地构建的二进制路径
SRC_DIR="${2:-/path/to/gb28181-simulator}"    # 源码目录（含 configs/）

echo "=== Installing gb28181-simulator ==="

# 1. User + dirs
sudo useradd --system --no-create-home --shell /usr/sbin/nologin gbsim 2>/dev/null || true
sudo mkdir -p /etc/gb28181-simulator /var/lib/gb28181-simulator
sudo chown gbsim:gbsim /var/lib/gb28181-simulator
sudo chmod 750 /etc/gb28181-simulator /var/lib/gb28181-simulator

# 2. Binary
sudo cp "$BINARY" /usr/local/bin/gb28181-simulator
sudo chmod 755 /usr/local/bin/gb28181-simulator
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/gb28181-simulator

# 3. Config — 从本地源码复制，不依赖外部下载
if [ ! -f "$SRC_DIR/configs/config.example.yaml" ]; then
    echo "ERROR: $SRC_DIR/configs/config.example.yaml not found"
    echo "  Provide the source directory as the second argument, or clone the repo first:"
    echo "    git clone https://github.com/your-org/gb28181-simulator.git"
    exit 1
fi
sudo cp "$SRC_DIR/configs/config.example.yaml" /etc/gb28181-simulator/config.yaml
echo "  Config copied to /etc/gb28181-simulator/config.yaml — edit before starting!"

# 4. Unit file (inline, same content as §3.4)
sudo tee /etc/systemd/system/gb28181-simulator.service > /dev/null << 'EOF'
[Unit]
Description=GB/T 28181-2016 Simulator
After=network-online.target Wants=network-online.target

[Service]
Type=simple
User=gbsim Group=gbsim
ExecStart=/usr/local/bin/gb28181-simulator --config /etc/gb28181-simulator/config.yaml
Restart=on-failure RestartSec=5s
LimitNOFILE=65536 LimitNPROC=4096
StandardOutput=journal StandardError=journal SyslogIdentifier=gb28181-simulator
ReadOnlyPaths=/etc/gb28181-simulator
ReadWritePaths=/var/lib/gb28181-simulator

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
echo "=== Installation complete ==="
echo ""
echo "Next steps:"
echo "  1. sudo \$EDITOR /etc/gb28181-simulator/config.yaml   # 填入实际节点地址和密码"
echo "  2. sudo systemctl enable --now gb28181-simulator"
echo "  3. curl http://127.0.0.1:8080/healthz               # 期望: {\"status\":\"ok\"}"
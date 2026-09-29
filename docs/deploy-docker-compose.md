# GB28181 Simulator — Docker Compose 多节点部署

> 适用版本：gb28181-simulator 全部功能（`init-project-skeleton` 至 `web-management-ui`）。
> 目标读者：DevOps / 平台工程师，需要在一台或多台 Docker 主机上以多节点拓扑运行模拟器集群。
> 部署形态：Docker Compose v2，单机多容器模拟多个 SIP 节点 + 多机叠加。

---

## 1. 前置依赖

| 依赖 | 版本要求 | 安装（Ubuntu/Debian） | 安装（CentOS） |
|---|---|---|---|
| Docker Engine | ≥ 24.0（Compose v2 内置） | 见 [docker.com/install](https://docs.docker.com/engine/install/) | 同上 |
| Docker Compose | v2（`docker compose` 子命令） | 内置于 Docker 24+ | 同上 |
| Linux kernel | ≥ 3.10（需要 nftables / iptables） | — | — |
| wget（用于 `docker compose exec` 健康探测） | — | `apt install wget` | `dnf install wget` |

验证：

```bash
docker --version       # Docker version 24.0+ ...
docker compose version # Docker Compose version v2.20+
```

---

## 2. 项目交付物

| 文件 | 角色 |
|---|---|
| `Dockerfile` | 多阶段镜像：node 构建前端 → go 构建二进制 → alpine 运行时 |
| `.dockerignore` | 排除 `web/node_modules`、`build/`、`openspec/`、`docs/` 等 |
| `docker-compose.yml` | 2 节点拓扑示例（1 个 platform-large + 1 个 device） |

---

## 3. 快速开始

### 3.1 准备节点配置

默认 Compose 文件假设各节点配置由挂载卷提供。先创建配置目录：

```bash
mkdir -p deploy/platform deploy/platform-data
mkdir -p deploy/device deploy/device-data
```

为 platform 节点编写 `deploy/platform/config.yaml`：

```yaml
# deploy/platform/config.yaml
# 最小可用配置：1 个 platform-large 节点 + 1 个 device 注册账号
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
    vendor: docker
    platform:
      realm: "3402000000"
      accounts:
        - username: "34020000001310000001"
          password: "compose-strong-password"
      min_expires: 60
      default_expires: 3600
      max_expires: 86400
```

为 device 节点编写 `deploy/device/config.yaml`：

```yaml
# deploy/device/config.yaml
# device 节点注册到 platform（容器内可通过 gbsim-platform DNS 访问）
http:
  host: "0.0.0.0"
  port: 8080
log:
  level: info

nodes:
  - id: "34020000001310000001"
    kind: device
    domain: "3402000000"
    addr: "0.0.0.0:5060"
    vendor: docker
    registration:
      server: "gbsim-platform:5060"     # Compose 服务名解析为内网 IP
      server_id: "34020000002000000001"
      username: "34020000001310000001"
      password: "compose-strong-password"
      expires: 3600
      transport: udp
      heartbeat_interval: 60s
      heartbeat_timeout: 5s
```
**无密码模式 device**（当上级 platform 也启用 `allow_no_auth: true` 时）：

```yaml
# deploy/device-noauth/config.yaml
# 无密码模式：两端均需设置 allow_no_auth: true
http:
  host: "0.0.0.0"
  port: 8080
log:
  level: info

nodes:
  - id: "34020000001310000002"
    kind: device
    domain: "3402000000"
    addr: "0.0.0.0:5060"
    vendor: docker
    registration:
      server: "gbsim-platform:5060"
      server_id: "34020000002000000001"
      username: "34020000001310000002"
      password: ""            # 空密码
      allow_no_auth: true    # 必须开启
      expires: 3600
      transport: udp
      heartbeat_interval: 60s
      heartbeat_timeout: 5s
      heartbeat_max_failures: 3
```

### 3.2 调整 docker-compose.yml 的卷挂载

默认 Compose 文件使用 `platform-config:/etc/gb28181-simulator:ro` 这种 named volume 模式。
若想直接绑定文件，最简方式：在 `docker-compose.yml` 中将 volumes 改为：

```yaml
    volumes:
      - ./deploy/platform/config.yaml:/etc/gb28181-simulator/config.yaml:ro
      - platform-data:/var/lib/gb28181-simulator
      - ./deploy/platform/logs:/var/lib/gb28181-simulator/logs
```

device 服务同理。

### 3.3 启动

```bash
docker compose up -d
docker compose ps     # 期望所有服务 status=Up (healthy)
docker compose logs -f gbsim-platform
```

### 3.4 健康检查

```bash
# Platform HTTP
curl http://127.0.0.1:8080/healthz
# 期望 {"status":"ok"}

# Device HTTP（注意 compose 映射到 8081）
curl http://127.0.0.1:8081/healthz

# 直接进入容器
docker compose exec gbsim-platform sh -c 'curl localhost:8080/healthz'
```

---

## 4. 网络规划

| 服务 | 容器内端口 | 宿主端口 | 协议 | 说明 |
|---|---|---|---|---|
| gbsim-platform | 5060 | 5060 | UDP/TCP | SIP 信令；与其他 SIP 端互通 |
| gbsim-platform | 8080 | 8080 | TCP | HTTP 管理 + Web UI |
| gbsim-device | 5060 | 5061 | UDP/TCP | SIP 信令；宿主侧偏移避免与 platform 端口冲突 |
| gbsim-device | 8080 | 8081 | TCP | HTTP 管理；同上偏移 |
| 媒体端口段（动态 RTP/RTCP） | 10000–30000 | 同 | UDP | config.yaml 中 `nodes[].media` 配置；按需修改 |

> **多机部署**：当 platform 与 device 部署在不同主机时，须确保：
> 1. 5060/UDP 在 platform 主机防火墙放行
> 2. device 端的 `registration.server` 改为 platform 主机的公网/内网 IP（如 `10.0.1.10:5060`）
> 3. Compose 跨主机可用 [Docker Swarm](https://docs.docker.com/engine/swarm/) 或复用 Kubernetes 替代；本项目不维护 K8s 模板

---

## 5. 持久化

| 卷 | 路径 | 用途 |
|---|---|---|
| `platform-config` | `/etc/gb28181-simulator/` | 只读 config.yaml |
| `platform-data` | `/var/lib/gb28181-simulator/` | 日志、SQLite data.db、临时文件 |
| `device-config` | 同上 | device 配置 |
| `device-data` | 同上 | device 运行时数据 |

> **生产建议**：把 data.db 与日志挂在主机持久化卷或挂载到 [Loki](https://grafana.com/oss/loki/) 等日志聚合后端，避免容器重建导致历史日志丢失。

### 备份

```bash
# 备份 platform-data
docker compose stop gbsim-platform
docker run --rm \
  -v gb28181-simulator_platform-data:/data \
  -v $(pwd)/backups:/backup \
  alpine tar czf /backup/platform-data-$(date +%Y%m%d).tgz -C /data .
docker compose start gbsim-platform
```

---

## 6. 升级

> 本项目**不依赖外部镜像仓库**。所有升级均从本地源码重新构建。

```bash
# 1. 进入源码目录，重新构建镜像
cd /path/to/gb28181-simulator
git pull origin main
docker compose build   # 本地重新构建，覆盖本地镜像

# 2. 滚动重启（先 device，再 platform，避免 SIP 心跳抖动）
docker compose up -d --no-deps gbsim-device
docker compose up -d --no-deps gbsim-platform

# 3. 验证
docker compose ps
curl http://127.0.0.1:8080/v1/version    # 应显示新版本
```

若用 `build` 方式（默认），Compose 文件中镜像引用为 `image: gb28181-simulator:latest`（本地 tag）。

---

## 7. 回滚

```bash
# 1. 切到旧 tag（本地源码仓库）
cd /path/to/gb28181-simulator
git checkout v1.2.2   # 切到旧 tag

# 2. 停服并重新构建旧版本
docker compose down
docker compose build
docker compose up -d

# 3. 验证 health
docker compose ps
curl http://127.0.0.1:8080/healthz
```

---

## 8. 常见故障

### 8.1 device 注册失败

```
ERR registration: dial tcp gbsim-platform:5060: i/o timeout
```

```bash
# 1. 确认 platform healthy
docker compose ps gbsim-platform    # Up (healthy)

# 2. 在 device 容器内测试连通性
docker compose exec gbsim-device sh
nslookup gbsim-platform             # 期望解析到 172.x.x.x
nc -uz gbsim-platform 5060          # 测试 UDP 5060 连通
```

> device 配置中 `registration.server` 必须用 **Compose 服务名**（`gbsim-platform`）而非 `localhost`，否则 device 容器无法解析到 platform 容器。

### 8.2 端口冲突（宿主 5060 已被其他 SIP 应用占用）

修改 `docker-compose.yml` 中端口映射，把 `5060:5060` 改为 `15060:5060`，同步把 device 的 `registration.server` 改为 `<host-ip>:15060`。

### 8.3 卷权限错误

```
Error: open /var/lib/gb28181-simulator/data.db: permission denied
```

容器内 UID 10001（gbsim）需要写权。检查宿主机：

```bash
docker compose exec gbsim-platform id   # 期望 uid=10001(gbsim)
ls -ld platform-data                     # 所有者应为 10001:10001
```

named volume 第一次创建时由 Docker 按镜像内的 `chown` 处理；若用 bind mount：

```bash
sudo chown -R 10001:10001 deploy/platform-data
```

### 8.4 `/healthz` 返回 5xx

```bash
docker compose logs --tail=100 gbsim-platform | grep -i error
# 常见原因：
#   - config.yaml 语法错误 → 进程退出 → Compose 显示 "Exit 1"
#   - nodes[].id 长度不为 20 位 → config 校验失败
#   - 端口已被占用 → bind error
```

### 8.5 无密码模式下 device 注册被拒绝（401）

```bash
# 原因：platform 侧 allow_no_auth 未设为 true，或 device 侧 allow_no_auth 为 false
# 两端必须同时开启无密码模式，否则 platform 仍会要求 Digest 验证。

# 修复：platform config.yaml 中加入 allow_no_auth: true
# platform 侧（deploy/platform/config.yaml）
nodes:
  - id: "34020000002000000001"
    kind: platform-large
    platform:
      allow_no_auth: true        # 允许空密码 device 注册
      accounts:
        - username: "34020000001310000002"
          password: ""            # 空密码

# device 侧（deploy/device-noauth/config.yaml）
#   registration.password = ""
#   registration.allow_no_auth = true

# 验证：重启后查看 platform 日志
docker compose logs gbsim-platform | grep -i "no.?auth\|401\|register"
```

### 8.6 跨主机部署网络不通

若 platform 与 device 在不同 Docker 主机：

1. 关闭 platform 主机防火墙或放行 5060/UDP：
   ```bash
   sudo ufw allow 5060/udp
   # 或
   sudo firewall-cmd --add-port=5060/udp --permanent
   sudo firewall-cmd --reload
   ```
2. device 配置 `registration.server` 改用 platform 公网/内网 IP（非 Compose 服务名）
3. 媒体端口段也要在防火墙放行

---

## 9. 监控与日志聚合（可选）

### 9.1 推到 Loki

`docker-compose.yml` 中追加：

```yaml
  gbsim-platform:
    # ... 既有配置 ...
    logging:
      driver: loki
      options:
        loki-url: "http://loki:3100/loki/api/v1/push"
        loki-pipeline-stages: |
          - match:
              selector: '{job="gbsim-platform"}'
              stages:
                - regex:
                    expression: 'level=(?P<level>\w+)'
```

### 9.2 暴露 Prometheus 指标

项目当前未默认暴露 `/metrics` 端点；如需 [Promtail](https://grafana.com/docs/loki/latest/clients/promtail/) 接入告警：

```bash
docker run -d \
  --name gbsim-promtail \
  -v /var/lib/docker/containers:/var/lib/docker/containers:ro \
  -v $(pwd)/promtail-config.yaml:/etc/promtail/config.yaml:ro \
  grafana/promtail:2.9.0 -config.file=/etc/promtail/config.yaml
```

---

## 10. 卸载

```bash
# 停止并删除容器、网络、image（保留数据卷）
docker compose down

# 删除全部（容器、网络、卷、image）
docker compose down --volumes --rmi all

# 清理 host 上的目录
rm -rf deploy/
```
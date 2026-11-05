# GB28181 Simulator — Docker Compose 部署

> 默认形态：**单容器、一套 Web、同进程多角色**（platform-large + device）。
> 顶栏切换「全部 / 平台 / 设备」管理两种节点，不再需要 18080/18081 双 Web。
> 可选双容器拓扑见文末附录与 `docker-compose.dual.yml`。

---

## 1. 前置依赖

| 依赖 | 版本要求 |
|---|---|
| Docker Engine | ≥ 24.0（Compose v2 内置） |
| Docker Compose | v2（`docker compose`） |

```bash
docker --version
docker compose version
```

---

## 2. 交付物

| 文件 | 角色 |
|---|---|
| `Dockerfile` | 多阶段构建（前端 + Go 静态二进制） |
| `docker-compose.yml` | **默认**：单服务 `gbsim` |
| `configs/examples/compose-single.yaml` | 同进程 platform + device + `auto_start` |
| `docker-compose.dual.yml` | 可选：双容器（两套 Web） |

---

## 3. 快速开始（推荐）

```bash
cd /path/to/gb28181-simulator
docker compose up -d --build
docker compose ps          # gbsim … Up (healthy)
docker compose logs -f gbsim
```

打开唯一 Web UI：

```
http://<宿主机IP>:18080
```

顶栏选「全部」可见 platform 与 device；「平台」/「设备」过滤菜单与节点列表。

健康检查：

```bash
curl -s http://127.0.0.1:18080/healthz
# {"status":"ok"}
```

### 无密码模式

两端都需要 `allow_no_auth: true`。可复制 `configs/examples/compose-single.yaml` 后：

- platform：`platform.allow_no_auth: true`，账号 `password: ""`
- device：`registration.allow_no_auth: true`，`password: ""`

再改 compose 挂载到你的文件。

> **首次空库**：YAML `nodes:` 会灌入 SQLite，并因 `auto_start: true` 自动启动。  
> **已有数据卷**：节点以 DB 为准，改 YAML 节点段不会覆盖；需要干净拓扑时删卷：  
> `docker compose down -v`（会丢库，谨慎）。

---

## 4. 网络规划（单实例）

| 服务 | 容器端口 | 宿主端口 | 协议 | 说明 |
|---|---|---|---|---|
| gbsim | 5060 | **15060** | UDP/TCP | 平台 SIP（第三方对接入口） |
| gbsim | 5061 | **15061** | UDP/TCP | 设备 SIP（同进程注册；可选对外） |
| gbsim | 8080 | **18080** | TCP | **唯一** Web UI + REST |
| 媒体 RTP | 动态 | 按需映射 | UDP | 拉流/推流端口段 |

设备注册地址在配置里为 `127.0.0.1:5060`（同容器 loopback 到本机 platform）。

---

## 5. 持久化

| 卷 | 路径 | 用途 |
|---|---|---|
| `gbsim-data` | `/var/lib/gb28181-simulator/` | SQLite、录像、日志等 |

```bash
docker compose stop gbsim
docker run --rm \
  -v gb28181-simulator_gbsim-data:/data \
  -v "$(pwd)/backups:/backup" \
  alpine tar czf "/backup/gbsim-data-$(date +%Y%m%d).tgz" -C /data .
docker compose start gbsim
```

---

## 6. 升级

```bash
cd /path/to/gb28181-simulator
git pull origin main
docker compose build --no-cache
docker compose up -d
docker compose ps
```

回滚：checkout 旧提交后重新 `build` + `up -d`。数据在 `gbsim-data` 卷，一般可保留。

---

## 7. 常见故障

| 现象 | 排查 |
|---|---|
| 健康检查失败 | `docker compose logs gbsim`；确认 8080 在容器内监听 |
| 设备未上线 | Web → 设备节点状态；确认空库首次导入且 `auto_start: true`；密码与 platform 账号一致 |
| 宿主 15060 冲突 | 改 `docker-compose.yml` 宿主侧端口映射 |
| 旧双容器卷残留 | `docker compose down` 后确认无 `gbsim-platform`/`gbsim-device`；新拓扑用新卷名 `gbsim-data` |
| 改 YAML 节点不生效 | DB 已有节点；Web UI 改节点，或 `down -v` 后重建（丢数据） |

---

## 附录 A：双容器拓扑（不推荐默认）

仅在需要「两个进程、两套 Web（18080/18081）」时使用：

```bash
mkdir -p deploy/platform deploy/device
# 编写 deploy/platform/config.yaml、deploy/device/config.yaml
# device.registration.server 使用 gbsim-platform:5060
docker compose -f docker-compose.dual.yml up -d --build
```

| 服务 | Web | SIP（宿主） |
|---|---|---|
| gbsim-platform | :18080 | :15060 |
| gbsim-device | :18081 | :15061 |

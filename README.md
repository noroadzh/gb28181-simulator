# gb28181-simulator

[![CI](https://img.shields.io/badge/CI-ci.yml-blue?logo=githubactions)](.github/workflows/ci.yml)
[![Release](https://img.shields.io/badge/release-v0.0.0--test-orange)](.github/workflows/release.yml)

使用 Go 编写的 GB/T 28181 协议模拟器。15 步路线图的 Change 1 交付了**项目骨架 + Web 外壳**：单一二进制，提供 health/version 端点、通过 WebSocket 流式输出结构化日志并内嵌 Vue 3 仪表盘。**尚未实现 GB28181 协议逻辑**；这部分在 Change 2 及之后到来。

## 快速开始

```sh
make run         # 先构建 web 产物，再 go run
open http://127.0.0.1:18080
```

默认配置从以下位置加载：

| 平台 | 路径 |
|------|------|
| Linux/macOS | `$XDG_CONFIG_HOME/gb28181-simulator/config.yaml`（默认 `~/.config/...`） |
| Windows | `%AppData%\gb28181-simulator\config.yaml` |

使用 `-config /path/to/config.yaml` 或环境变量 `GB28181_SIMULATOR_CONFIG` 覆盖。

## Web 管理界面

内嵌 Web UI（任意节点的 HTTP 端口）提供以下页面：

- **节点概览**：节点生命周期管理；device 节点可进入通道/媒体源，platform 节点可进入账号管理
- **通道管理**：device 节点子通道列表，支持运行时新增/删除通道（sqlite 持久化，重启恢复）、为通道配置媒体源（RTSP/HLS/文件路径/上传）
- **账号管理**：platform 节点的 SIP 注册账号维护（新增/删除/改密码），YAML `platform.accounts` 首次启动幂等 seed 入库，之后以 sqlite 为准
- **帮助**：业务系统对接本模拟器的完整流程说明（REGISTER 注册 → CATALOG 拉设备列表 → INVITE 预览 → RTP/RTSP 拉流）

## 端点

- `GET /v1/health` — `{"status":"ok"}`
- `GET /v1/version` — 版本元数据
- `WS  /v1/logs/stream` — JSON 编码的结构化日志流
- `GET /` — 内嵌仪表盘（Vue 3 + Element Plus）
- `GET /v1/nodes` — 节点清单（`id`、`kind`、`status`、`addr`）
- `GET /v1/nodes/{id}` — 单节点；未知 id 返回 `404` 与 `{"error":...}`
- `POST /v1/nodes/{id}/start` — 绑定监听口，状态 → `registering`
- `POST /v1/nodes/{id}/stop` — 释放监听口，状态 → `offline`
- `GET /v1/nodes/{id}/channels` — 通道列表（device 节点）
- `POST /v1/nodes/{id}/channels/{ch}/ptz` — PTZ 云台控制
- `GET /v1/nodes/{id}/channels/{ch}/records` — 录像查询
- `POST /v1/nodes/{id}/channels/{ch}/playback` — 录像回放控制
- `POST /v1/nodes/{id}/channels/{ch}/talk/start` — 开始语音对讲
- `POST /v1/nodes/{id}/channels/{ch}/talk/stop` — 停止语音对讲
- `GET /v1/nodes/{id}/channels/{ch}/snapshot` — 快照抓图（返回 JPEG）
- `GET /v1/flv/{id}/{ch}` — HTTP-FLV 实时流（flv.js 播放）
- `POST /v1/nodes/{id}/channels` — 动态新增子通道（落库持久化，重启恢复）
- `DELETE /v1/nodes/{id}/channels/{ch}` — 删除子通道
- `POST /v1/nodes/{id}/media/upload` — 上传媒体文件（multipart，≤2GB），返回容器内路径
- `GET /v1/platforms/{id}/accounts` — 平台 SIP 注册账号列表
- `POST /v1/platforms/{id}/accounts` — 新增账号（sqlite 持久化，即时生效）
- `DELETE /v1/platforms/{id}/accounts/{username}` — 删除账号
- `PUT /v1/platforms/{id}/accounts/{username}/password` — 修改账号密码

非法状态转换返回 `409`，并携带节点当前状态：

```json
{"error":"app: start node 34020000011310000001: model: illegal node status transition: registering -> registering","status":"registering","action":"start"}
```

## 节点

在配置中声明 GB/T 28181 节点；每个节点拥有自己的信令监听口与生命周期，启动一个不影响其他节点。完整示例见 [`configs/config.example.yaml`](configs/config.example.yaml)。

```yaml
nodes:
  - id: "34020000011310000001"   # 20 位数字；3 位类型码决定 kind
    kind: device                 # device | platform-large | platform-small
    domain: "3402000000"
    addr: "127.0.0.1:15060"      # 各节点间必须唯一
    vendor: acme                 # 可选
```

省略 `nodes:` 时以零节点运行。状态沿 `idle → registering → registered → online` 推进，stop 落到 `offline`，`fault` 只能回到 `idle` 或 `offline`。

`device` 节点启动后立即注册：添加 `registration:` 块后会发送 REGISTER，用 Digest 凭据应答平台的 401 挑战并进入 `online`；失败（超时、拒绝、5xx）会使节点 fault 并释放端口。没有该块时，启动后的节点保持 `registering`，与之前完全一致。

`platform-large` 节点是另一半：它**接受**注册。启动它会在其监听口上启动一个服务 goroutine 并使节点 `online`（含义：平台已对外服务）。无凭据的 REGISTER 会被应答 `401` 与 Digest 挑战；`Authorization` 无法解析的会再次被挑战；平台没有对应账号的用户名，或格式正确但校验失败的应答，会按 GB/T 28181 §L.2 的要求直接回 `403`，不再进行二次挑战。通过校验的设备会被授予不超过平台上限的 lifetime，并记录在节点的在线设备表里，可在 `GET /v1/nodes/{id}/devices` 读取；`Expires: 0` 会将其移除。停止平台会先结束服务 goroutine 再释放端口，并清空该表。

平台还会**接收心跳、应答目录查询并清扫过期设备**：下游的 `Keepalive` 通知会刷新其所在行但不延长被授予的 lifetime，`Catalog` 查询由该平台自己的在线设备作答，而停止重新注册的设备会在被授予的 lifetime 结束后被移除。

```yaml
  - id: "34020000002000000001"
    kind: platform-large
    domain: "3402000000"
    addr: "127.0.0.1:15061"
    platform:                      # 可选；以下为默认值
      realm: "3402000000"          # 默认：节点自身 domain
      accounts:                    # 无账号的平台不接受任何人
        - username: "34020000011310000001"
          password: "change-me"    # 绝不写入日志
      min_expires: 60              # 默认 60
      default_expires: 3600        # 默认 3600
      max_expires: 86400           # 默认 86400
```

`platform-small` 节点同时具备两半能力 —— 级联链路 `A → B → C` 中间那一环。声明 `platform:` 后它会像 platform-large 一样对下游**提供服务**；声明 `registration:` 后它会像 device 一样向上级平台**注册**。两者都声明时节点充当中继：设备向小平台注册，而小平台自身又向大平台注册。每张平台的在线表只保存紧邻其下一级，因此顶部平台可以向中间平台询问目录，并获得注册在它那里的设备信息。

两半都运行在节点唯一的监听口上，节点仍然只是一个节点：无论哪一半先到达，它只 `online` 一次；任一半失败都会使节点 fault，另一半也会被回收而不是单独残留。停止它会先向上级平台注销（`Expires: 0`），再结束下面的服务。

```yaml
  - id: "34020000002160000001"
    kind: platform-small
    domain: "3402000000"
    addr: "127.0.0.1:15062"
    platform:                      # 可选；它下面那一半
      realm: "3402000000"
      accounts:
        - username: "34020000011310000002"
          password: "change-me"
    registration:                  # 可选；它上面那一半
      server: "127.0.0.1:15061"
      server_id: "34020000002000000001"
      password: "change-me"
```

上线后节点保持活跃：每个 `heartbeat_interval` 发送一次 MANSCDP `Keepalive`（SIP `MESSAGE`），在平台授予 lifetime 的一半时续期注册，并在 `heartbeat_max_failures` 次心跳无应答后放弃（使节点 fault 并释放端口，与注册失败一致）。`POST /v1/nodes/{id}/unregister` 发送 `Expires: 0`、停止后台工作并使节点 `offline`；若平台拒绝，节点保持 `online`，错误会指出阶段。

```yaml
nodes:
  - id: "34020000011310000001"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:15060"
    registration:
      server: "127.0.0.1:15061"    # 上级平台 host:port
      server_id: "34020000002000000001"  # 可选；用于 Request-URI
      password: "change-me"        # 绝不写入日志（见 log.redact_keys）
      expires: 3600                # 可选；秒，默认 3600
      transport: udp               # 可选；udp（默认）或 tcp
      timeout: 5s                  # 可选；默认 5s
      heartbeat_interval: 60s      # 可选；心跳周期，默认 60s
      heartbeat_timeout: 5s        # 可选；默认 5s；须小于 interval
      heartbeat_max_failures: 3    # 可选；默认 3
```

## 技术栈

| 关注点 | 选型 |
|--------|------|
| 语言 | Go，纯 Go / `CGO_ENABLED=0` |
| SIP | `github.com/ghettovoice/gosip` |
| HTTP | `github.com/labstack/echo/v4` |
| WebSocket | `github.com/gorilla/websocket` |
| 配置 | `github.com/spf13/viper`（YAML + 环境变量覆盖） |
| 存储 | `modernc.org/sqlite`（纯 Go，无 CGO） |
| 日志 | `log/slog`（trace / debug / info / warn / error） |
| 追踪 | OpenTelemetry — 默认 stdout 导出器，可选 OTLP gRPC（`tracing.otlp_endpoint`） |
| 依赖装配 | 手写 `internal/platform/servicectx`（不使用 wire / fx） |
| 前端 | Vue 3 + Element Plus + Vite，经 `embed.FS` 内嵌 |

## 架构

`internal/` 采用六边形（端口与适配器）布局 —— 分层图、端口契约清单与 ServiceContext 用法见 [`docs/architecture.md`](docs/architecture.md)。

## 目录

```
cmd/gb28181-simulator      入口
cmd/sipprobe               SIP 诊断 CLI（Change 2 §7）
internal/adapter           SIP / SDP / Digest / Transport / Audit / 节点注册表适配器
internal/app               用例编排（NodeService，Change 4）
internal/domain            领域模型 + port 接口
internal/interface/http    HTTP/WS 服务器（Echo + gorilla/websocket）
internal/interface/webui   内嵌仪表盘（Vue 3 + Element Plus，embed.FS）
internal/platform          配置、日志、追踪、时钟、servicectx
internal/storage           SQLite 引导（暂无业务表）
internal/sipprobe          sipprobe 核心（可测试）
configs/                   示例配置
scripts/                   冒烟脚本（Change 2 §8.2）
web/                       Vue 3 + Vite 源码
openspec/                  OpenSpec 变更产物
```

## 构建矩阵

```
linux/amd64   linux/arm64
darwin/amd64  darwin/arm64
windows/amd64
```

所有二进制均以 `CGO_ENABLED=0` 产出。本地执行 `make release-matrix` 或触发 GitHub Actions workflow。自 Change 2 起，矩阵为每个平台同时产出 `gb28181-simulator` 与 `sipprobe`。

## 开发

```sh
make fmt            # gofmt -s -w + goimports
make lint           # go vet ./...
make test           # go test -race -cover ./...
make build          # 构建 bin/gb28181-simulator（先构建 web 产物）
make sipprobe-build # 构建 bin/sipprobe
make service-build  # 为宿主平台构建所有服务二进制
make sip-test       # 快速反馈：仅 SIP / SDP / Digest（约 3-4 秒）
make release-matrix # 5 平台二进制 + sha256，CGO_ENABLED=0
make smoke          # 冒烟基线：单元测试 + 5 平台编译 + 前端构建 + e2e 探针
./scripts/smoke-sip.sh   # 双进程 probe 交互，显式端口控制
```

`bin/gb28181-simulator sipprobe ...` 同样暴露诊断探针，因此单个二进制既能当模拟器也能当探针。

### 测试与部署文档

- [冒烟测试](docs/smoke-test.md) — 测试矩阵、本地/CI 执行、结果解读、失败排查
- [Linux 单机部署](docs/deploy-linux.md) — systemd 安装、升级与回滚
- [Docker Compose 部署](docs/deploy-docker-compose.md) — 多节点容器化部署
- [Web 管理界面操作手册](docs/web-ui-guide.md) — Dashboard / 通道列表 / PTZ / 回放 / 对讲 / 抓图

仪表盘源码位于 `web/`；生产构建产物写入 `internal/interface/webui/embed/dist/`，并通过 `//go:embed` 内嵌进 Go 二进制。

## 许可证

Apache License 2.0 —— 见 `LICENSE`。

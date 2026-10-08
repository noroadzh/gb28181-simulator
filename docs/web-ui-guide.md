# Web 管理界面操作手册

> 版本：8e55379（2026-10-08）。本手册基于 Web 前端实际源码（`web/src/views/`）编写，以代码行为为准，不臆测。

## 目录

- [1. 打开界面](#1-打开界面)
- [2. 侧边栏导航](#2-侧边栏导航)
- [3. 节点概览（/nodes）](#3-节点概览nodes)
- [4. 抓包面板（/capture）](#4-抓包面板capture)
- [5. 故障注入（/fault）](#5-故障注入fault)
- [6. 场景管理（/scenarios）](#6-场景管理scenarios)
- [7. 仪表盘（/dashboard）](#7-仪表盘dashboard)
- [8. 常见问题排查](#8-常见问题排查)

---

## 1. 打开界面

### Docker 部署

```
Platform Web UI：http://<宿主机IP>:18080
Device  Web UI：http://<宿主机IP>:18081
```

- 当前 Docker 部署使用端口偏移：Platform `18080`，Device `18081`（原默认 `8080` 端口被宿主机服务占用）。
- 根路径 `/` 自动重定向到 `/nodes`。
- 无需认证，打开即用。

### 直接运行（本地）

```bash
cd /Users/noroadzh/code/jfys/go/gb28181-simulator
go run ./cmd/gb28181-simulator -config configs/config.example.yaml
# 访问 http://127.0.0.1:8080
```

### 前端构建

```bash
cd web
npm ci && npm run build
# 产物在 internal/interface/webui/embed/dist/，Go 编译时自动嵌入二进制
```

---

## 2. 侧边栏导航

导航菜单固定在页面左侧（深色背景，宽度 200px），从上到下：

| 菜单项 | 路径 | 对应 Vue 组件 |
|--------|------|---------------|
| 节点概览 | `/nodes` | `NodesView.vue` |
| 抓包面板 | `/capture` | `CaptureView.vue` |
| 故障注入 | `/fault` | `FaultView.vue` |
| 场景管理 | `/scenarios` | `ScenarioView.vue` |
| 仪表盘 | `/dashboard` | `DashboardView.vue` |

当前菜单项中没有「媒体源」或「节点启停」的独立页面入口。

- 页面切换无需刷新，Vue Router 前端路由控制。
- 顶部 Header 显示当前面包屑、服务状态（绿/红标签）和版本号。

---

## 3. 节点概览（/nodes）

**代码位置**：`web/src/views/NodesView.vue`

### 功能

1. **刷新**：右上角「刷新」按钮，调用 `GET /v1/nodes` 重新拉取节点列表。
2. **筛选**：顶部分类筛选下拉框，可选：
   - 全部
   - `device`
   - `platform-small`
   - `platform-large`
3. **节点卡片**：每个节点以卡片形式展示：

| 字段 | 说明 |
|------|------|
| 节点 ID | 节点标识（20 位国标 ID） |
| 状态标签 | `online`（绿色）/ 其他（灰色）/ `fault`（红色） |
| 类型 | `device` / `platform-small` / `platform-large` |
| 地址 | 节点监听地址 |
| 故障计数 | 累计触发的 fault 总数（来自 `node.fault_counters`） |

4. **快捷操作按钮**：
   - 「抓包」→ 跳转到 `/capture/:id`
   - 「故障注入」→ 跳转到 `/fault/:id`

### 操作示例

1. 在浏览器打开 `http://<宿主机IP>:18080/nodes`
2. 在筛选下拉框选择 `device`，只显示设备节点。
3. 点击某个节点卡片的「抓包」按钮进入抓包面板。

### 注意事项

- 节点列表为空时显示「暂无节点」空状态。
- 节点状态为 `online` 需要节点已成功启动并完成注册。

---

## 4. 抓包面板（/capture）

**代码位置**：`web/src/views/CaptureView.vue`

### 功能

1. **选择节点**：顶部下拉框从当前节点列表选择要查看的节点。
2. **自动刷新**：每 3 秒自动拉取最新抓包数据（`GET /v1/nodes/:id/capture?limit=50`）。
3. **手动刷新**：「刷新」按钮立即重新拉取。
4. **下载 pcap**：「下载 pcap」按钮在新标签页打开 `GET /v1/nodes/:id/capture.pcap`，浏览器下载 `.pcap` 文件。
5. **查看 Payload**：点击表格中 Payload 列的字节数，弹出 Popover 显示十六进制 hexdump。

### 表格列

| 列名 | 说明 |
|------|------|
| 方向 | `t`（发送，橙色）/ `r`（接收，绿色） |
| 时间 | 抓包事件时间戳（本地时间格式） |
| 本地 | 本地地址 `IP:端口` |
| 对端 | 远端地址 `IP:端口` |
| 协议 | 传输协议（UDP / TCP 等） |
| Payload | 点击查看 hexdump |

### 操作示例

1. 进入 `/capture`，在「选择节点」下拉框选中目标节点。
2. 等待 3 秒自动刷新，或点击「刷新」立即拉取。
3. 若该节点有 SIP 消息，表格会显示抓包事件列表。
4. 点击某个事件的 Payload 列中的字节数，查看十六进制内容。
5. 点击「下载 pcap」保存完整抓包文件到本地，可用 Wireshark 打开分析。

### 常见问题

**报错 "app: capture store not configured"**：该错误说明服务端 `capture.enabled` 未启用。在 `config.yaml` 中添加：

```yaml
capture:
  enabled: true
  capacity: 2048
```

重启服务后生效。容量为每个节点环形缓冲区大小（默认 2048 条事件）。

---

## 5. 故障注入（/fault）

**代码位置**：`web/src/views/FaultView.vue`

### 功能

1. **安装故障 Profile**：左侧表单，配置故障注入规则后点击「安装」。
2. **查看当前 Profile**：右侧展示当前已安装的 fault profile 和异常计数。
3. **清除故障**：「清除故障」按钮删除当前 profile。

### 支持的故障类型

| 字段 | 说明 | 值范围 |
|------|------|--------|
| Canned（方法:状态码） | 特定 SIP 方法强制返回指定状态码 | 格式：`METHOD:code, METHOD:code`，code 范围 400–699 |
| Delay Base (ms) | 所有请求的基础延迟 | ≥ 0 |
| Delay Jitter (ms) | 延迟的随机波动范围 | ≥ 0 |
| Drop 概率 | 随机丢弃请求的概率 | 0.0 – 1.0 |
| Blackhole 方法 | 指定方法完全不响应（静默丢弃） | 逗号分隔的方法名，如 `MESSAGE,SUBSCRIBE` |
| UnsupportedMethod | 强制返回 501 的方法 | 0 – 699（状态码范围） |

### 操作示例

1. 进入 `/fault/:id`（从节点概览页点击「故障注入」或手动输入 URL）。
2. 在左侧表单填写：
   - Canned：`REGISTER:403, INVITE:486`（注册返回 403，点播返回 486）
   - Delay Base：`200`（所有请求延迟 200ms）
   - Drop：`0.1`（10% 概率丢弃）
3. 点击「安装」，显示「故障 profile 已安装」提示。
4. 右侧「当前 Profile」展示已安装的配置，以及各异常类型的触发计数。
5. 测试完成后点击「清除故障」恢复正常行为。

### 注意事项

- 未安装 profile 时右侧显示「未安装故障 Profile」空状态。
- 清除故障后所有计数器归零。
- `no fault profile installed` 错误会被静默处理（不弹提示）。

---

## 6. 场景管理（/scenarios）

**代码位置**：`web/src/views/ScenarioView.vue`

### 功能

1. **场景列表**：展示所有可用的测试场景卡片。
2. **执行场景**：点击「执行」按钮运行场景（同一时刻只能运行一个场景）。
3. **执行报告**：运行完成后显示详细报告，包含每个步骤的状态和耗时。

### 场景卡片字段

| 字段 | 说明 |
|------|------|
| Name | 场景唯一标识 |
| Description | 场景描述（YAML 文件中的描述字段） |
| 执行中标签 | 当前场景正在运行时显示黄色「执行中」标签 |

### 执行报告

| 字段 | 说明 |
|------|------|
| scenario_name | 执行的场景名称 |
| total | `passed`（绿色）/ `failed`（红色） |
| 耗时 | 场景总执行时间（毫秒） |
| steps | 步骤列表，每步包含：步骤类型、状态、耗时、错误信息 |

### 操作示例

1. 进入 `/scenarios`。
2. 找到要执行的场景卡片，点击「执行」按钮。
3. 按钮变为 loading 状态，同时显示「执行中」标签。
4. 场景执行完成后，页面下方出现执行报告。
5. 若所有步骤均 passed，显示绿色 `passed` 标签；否则显示红色 `failed` 并查看失败步骤的错误信息。

### 注意事项

- 同一时刻只能运行一个场景，其他「执行」按钮会被禁用。
- 尚无执行报告时，页面不显示报告区域。
- 后端 `scenario engine` 未装配时 `POST /v1/scenarios/run` 返回 501。

---

## 7. 仪表盘（/dashboard）

**代码位置**：`web/src/views/DashboardView.vue`

### 功能

1. **Version 卡片**：展示服务版本信息。
2. **实时日志流**：通过 WebSocket 实时推送日志到前端表格。
3. **健康状态**：顶部 Header 绿色/红色标签反映服务健康状态。

### Version 卡片字段

| 字段 | 说明 |
|------|------|
| Version | 版本号 |
| Commit | Git 短 commit |
| Go | Go 版本 |
| Platform | 操作系统 / 架构 |

### 日志流

- 通过 WebSocket 连接 `ws://<host>/v1/logs/stream` 实时推送。
- 表格最多保留 200 条日志，新日志从顶部插入。
- 日志级别：trace / debug / info / warn / error。
- 服务端日志级别可在 `config.yaml` 的 `log.level` 中配置，也支持通过 `PATCH /v1/config/log` 动态调整（非持久化，重启后恢复文件配置）。

### 操作示例

1. 进入 `/dashboard`。
2. 查看 Version 卡片确认版本和运行环境。
3. 观察日志表格的实时滚动，调试时重点关注 `internal/app` 和 `internal/adapter/media` 模块的日志。

---

## 8. 常见问题排查

### 8.1 页面打不开 / 404

| 现象 | 排查 |
|------|------|
| 连接被拒绝 | 检查容器/进程是否在运行，端口是否正确 |
| 404 页面 | 确认 URL 路径正确：`/nodes`、`/capture`、`/fault`、`/scenarios`、`/dashboard` |

### 8.2 节点列表为空

- 确认 `config.yaml` 的 `nodes:` 段配置了节点，且节点 ID 格式正确（20 位国标 ID）。
- 确认服务已启动且 HTTP 端口可访问。
- 查看 `/dashboard` 的日志流，查找配置加载错误。

### 8.3 抓包面板报错 "capture store not configured"

见第 4 节末尾的配置说明。在 `config.yaml` 中添加：

```yaml
capture:
  enabled: true
  capacity: 2048
```

重启服务。注意 Docker 部署时若只修改了 bind mount 的 config 文件，需要 `docker compose restart` 而非 `up -d`。

### 8.4 故障注入不生效

- 确认节点已启动并在线（状态为 `online`）。
- 确认安装时表单填写正确（Canned 状态码 400–699，Delay 非负，Drop 在 0–1 之间）。
- 查看 `/dashboard` 日志流，确认 acceptor 是否读到 fault profile。

### 8.5 场景执行 501

- 确认 `scenario engine` 已装配：`cmd/gb28181-simulator/main.go` 中 `scenarioRunner` 非 nil。
- 如果未装配，`POST /v1/scenarios/run` 返回 `{"error":"scenario engine not configured"}`。

### 8.6 日志不刷新

- 确认 WebSocket 连接未断开：`/dashboard` 的日志表格底部应持续有新日志。
- 若显示 `ws-closed`，尝试刷新页面。
- 检查浏览器控制台是否有 WebSocket 错误。

### 8.7 节点状态始终不是 online

- device 节点需要正确配置 `registration.server` 指向 platform 地址。
- Docker 部署中 device 的 `registration.server` 应使用 Compose 服务名：`gbsim-platform:5060`。
- 查看日志中是否有 401 挑战或注册超时错误。

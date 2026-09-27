# 设计：Web 管理界面（路线图 #14）

## 总体架构

前端与后端同存于一个二进制中，通过 echo 路由 `/web/` 提供静态文件。所有前端资源由 Vite 构建后通过 `embed.FS` 嵌入，启动时无需外部文件系统访问。

```
┌─────────────────────────────────────────────────────────┐
│                    gb28181-simulator                      │
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────┐  │
│  │  SIP Stack  │◄──►│  App Services│◄──►│  HTTP API   │  │
│  └─────────────┘    └──────────────┘    └──────┬──────┘  │
│                                                │         │
│  ┌─────────────────────────────────────────────┼───────┐ │
│  │  echo router                                │       │ │
│  │  /v1/nodes/:id          ← REST API         │       │ │
│  │  /v1/nodes/:id/capture  ← capture API      │       │ │
│  │  /v1/nodes/:id/faults   ← fault API        │       │ │
│  │  /web/*                 ← frontend SPA      ▼       │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                ▲       │
│  ┌─────────────────────────────────────────────────────┐ │
│  │  embed.FS (web/dist)                                │ │
│  │  index.html + js/ + css/ + assets/                  │ │
│  └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

## 前端技术栈

- **框架**：Vue 3 (Composition API) + TypeScript
- **UI**：Element Plus
- **构建**：Vite
- **路由**：Vue Router（Hash 模式，兼容静态文件服务）
- **状态**：Pinia（节点列表、当前选中节点、故障 profile、抓包事件流）
- **打包**：Vite 构建产物放入 `web/dist/`，main.go 通过 `//go:embed web/dist/*` 嵌入

## 页面与组件拆分

### Phase 1：基础框架 + 节点管理 + 抓包面板

1. **App 壳**：侧边栏导航（节点概览、抓包面板、故障注入、场景管理）、面包屑、全局状态。
2. **节点概览页**：`GET /v1/nodes` 拉取列表，卡片展示 ID/类型/地址/状态/故障计数。支持点击进入详情。
3. **抓包面板页**：轮询 `GET /v1/nodes/:id/capture?limit=50`，表格展示方向/端点/时间戳/大小，点击展开 hexdump，提供 `capture.pcap` 下载按钮。

### Phase 2：故障注入面板

4. **故障注入页**：表单选择 profile 类型与参数（状态码映射、延迟、drop 概率、黑洞方法列表），提交到 `POST /v1/nodes/:id/faults`；展示当前 profile 详情与计数，`DELETE /v1/nodes/:id/faults` 清除。

### Phase 3：场景管理预览

5. **场景管理页**：列表展示场景包（占位数据），执行按钮调用 `POST /v1/scenarios/run`（#15 的桩接口）。

## API 对接

前端仅与现有的 `GET /v1/nodes`、`GET /v1/nodes/:id`、`GET /v1/nodes/:id/capture`、`GET /v1/nodes/:id/capture.pcap`、`POST /v1/nodes/:id/faults`、`DELETE /v1/nodes/:id/faults` 交互。不新增后端端点（场景管理除外，留桩给 #15）。

## 配置

`config.yaml` 新增：

```yaml
web:
  enabled: true
  addr: ":8080"
  prefix: "/web"
```

`enabled` 为 `false` 时完全不挂载前端路由，保持纯 API 模式。

## 非功能性要求

- 单文件分发：前端资源嵌入二进制，部署无需 Nginx。
- 零构建依赖：最终 CI 只需 `go build`，前端构建产物作为 git 保留或由 CI 在 build 前执行 `cd web && npm run build`。
- 内存：前端资源由 OS 页面缓存，不影响模拟器内存预算。
- 安全：无认证，仅限本地回环访问（默认绑定 `127.0.0.1`）。

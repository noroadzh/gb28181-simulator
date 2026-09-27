# 提案：Web 管理界面（路线图 #14）

## 背景

路线图第 **#14 阶段（`web-management-ui`）**。模拟器后端已完成核心信令面与故障注入能力：#13 提供了故障注入 API、抓包查询与 pcap 导出 API；之前的 Change 提供了节点列表、媒体源、级联转发等端点。但这些能力目前只能通过 `curl` 或 Swagger 风格的 HTTP 调用访问，没有图形化入口。项目需要提供一个内嵌的 Web 管理界面，让测试工程师与运维人员能够直观地查看节点状态、下发故障、下载抓包、触发场景，而无需记忆端点与 JSON 格式。

## 变更内容

本次变更采用**按功能分阶段迭代**的方式交付：先交付基础页面框架与抓包面板，再逐步加入故障注入面板、场景管理、拓扑可视化等模块。每个子功能保持可独立部署、可独立回滚。

1. **基础页面框架** — 复用现有 Vue3 + ElementPlus + Vite embed.FS 前端栈，建立主布局（侧边栏、面包屑、全局状态）。所有页面资源打包进二进制，`/web/` 路径由 echo 静态文件服务提供，启动时零外部依赖。
2. **节点管理页面** — 展示所有已注册节点的概览卡片（ID、类型、地址、在线状态、keepalive 状态、故障计数）。支持筛选与刷新。
3. **抓包面板** — 选择节点后查看近期 wire events（方向、时间戳、端点、大小、hexdump 预览），支持 `limit` 分页，支持一键下载该节点的 `capture.pcap`。面板与 #13 提供的 `GET /v1/nodes/:id/capture` 与 `GET /v1/nodes/:id/capture.pcap` 对接。
4. **故障注入面板** — 选择节点后安装或清除故障 profile（canned / delay / drop / blackhole / unsupported-method），实时查看异常事件计数。面板与 #13 提供的故障 API 对接。
5. **场景管理页面（预览）** — 预留 YAML 场景包上传与执行入口，后端桩对接 #15 `scenario-engine`，前端仅展示场景列表与执行状态。

## 能力边界

### 新增能力

- `web-management-ui`：内嵌 Web 管理界面，包含节点概览、抓包面板、故障注入面板、场景管理预览。
- 前端资源打包机制：Vue3 + Vite + ElementPlus 产物通过 `embed.FS` 嵌入 Go 二进制，单文件分发。

### 修改的能力

- `internal/interface/http` — 可能新增少量前端路由所需的兜底端点或静态资源配置。
- `cmd/gb28181-simulator/main.go` — 注册 `/web/` 静态文件服务路由。
- `internal/platform/config` — 可能新增 `web.enable` 等开关配置。

## 非目标

- 实时流媒体播放器（HLS/RTMP/WebRTC）— 本次只做管理面，媒体流播放留给后续迭代或外部播放器。
- 多租户与 RBAC — 单机模拟器无认证需求。
- 国际化（i18n）— 仅做简体中文界面。
- 移动端适配 — 仅保证桌面浏览器（Chrome/Edge/Safari）可用。
- 节点配置编辑 — 节点 profile 由启动配置或 OpenSpec 驱动，不在本界面内动态修改。
- 真实抓包（libpcap）— 继续使用 #13 的合成 pcap。

## 影响范围

- `web/` — 新增前端源码目录（Vue3 + TypeScript + ElementPlus + Vite）。
- `internal/interface/http` — 注册 `/web/` 路由与可能的 API 代理。
- `cmd/gb28181-simulator/main.go` — 挂载前端静态资源。
- `internal/platform/config` — 可能新增 web 相关配置项。
- `openspec/changes/web-management-ui/specs/` — 按功能拆分子 spec：`capture-panel`、`fault-panel`、`node-overview`、`scenario-manager`。

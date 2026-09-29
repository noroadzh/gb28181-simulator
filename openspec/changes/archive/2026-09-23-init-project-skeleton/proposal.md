# Proposal

## Why

`gb28181-simulator` 是一个伪装成真实 GB/T 28181 网关的模拟器 —— 以 Node 为基本单位，每个节点独立扮演 `device` / `platform-small` / `platform-large` 三种身份中的一种，构成任意拓扑。在进入任何协议实现（路线图 Change 2~15）之前，必须先把**仓库结构、依赖、CI、日志、HTTP 骨架、Web 嵌入**等基础设施落地，否则后续每个 change 都会重复承担这些无差异成本。本 change 严格不引入任何协议实现，仅交付"启动后能在浏览器看到空壳"的工程基线，作为后续 14 个 change 的共同地基。

## What Changes

- 初始化 Go 1.22+ 模块（`github.com/.../gb28181-simulator`），按 `cmd/` + `internal/` + `web/` + `configs/` 分层布局
- 引入核心依赖：`labstack/echo/v4`（HTTP）、`gorilla/websocket`（WS）、`spf13/viper`（配置）、`modernc.org/sqlite`（持久化）、`log/slog`（日志）
- 实现**基础设施级别的 4 个内部包**（不涉及任何 GB28181 业务）：
  - `internal/logger`：基于 `log/slog` 的分级 Handler（trace/debug/info/warn/error），支持文件 + WebSocket 双输出与字段脱敏
  - `internal/config`：基于 viper 的 YAML 加载 + XDG / `%AppData%` 路径解析
  - `internal/storage`：基于 sqlite 的 schema 引导（仅建表，不写业务数据）
  - `internal/api`：Echo 注册路由 `/api/health`、`/api/version`、`/api/logs/stream`（WS）；WebSocket 日志流将作为后续 14 个 change 的通用通道
- 前端最小骨架：Vue3 + Element Plus + Vite 单页（仅一个 Dashboard 卡片显示版本/运行状态/日志 WebSocket 流）
- 前端构建产物经 `embed.FS` 嵌入 Go 二进制，**单文件分发**
- 仓库级 CI：`.github/workflows/ci.yml` —— golangci-lint、跨平台 build matrix（Linux amd64/arm64、macOS amd64/arm64、Windows amd64）、`go test ./...`；tag 触发 release workflow 产出二进制与 sha256
- 编写 README 简版（构建/运行/CI）
- 配置目录遵循 XDG（Linux/macOS）与 `%AppData%`（Windows）
- `go.mod` / `go.sum` 锁定；`Makefile` 封装常用命令（`make build` / `make test` / `make web` / `make run`）

## Capabilities

### New Capabilities

- `project-skeleton`: 仓库级基础设施（模块布局、CI、logger、HTTP+WS 骨架、配置加载、sqlite 引导、嵌入前端、跨平台构建）。这是后续所有 GB28181 协议 change 共享的工程基线。

### Modified Capabilities

（无）

## Impact

- **新增代码**：~15 个 Go 文件 + 1 个 Vue3 入口文件 + 1 份 CI 工作流 + README + Makefile，合计预计 < 1500 行
- **新增依赖**（全部为后续 change 也将复用的稳定库）：`github.com/labstack/echo/v4`、`github.com/gorilla/websocket`、`github.com/spf13/viper`、`modernc.org/sqlite`
- **后续 change 的影响**：Change 2 (`core-sip-stack`) ~ Change 15 (`scenario-engine`) 都将复用本 change 产出的 logger、config、storage、HTTP/WS 路由框架；不得绕开这些基础设施
- **不引入 CGO 依赖**：所有库均为纯 Go，保证 Linux/macOS/Windows 跨平台编译一致
- **不影响 `docs/`**：标准 PDF 保持原状，不被本 change 触及

## Non-Goals（明确不属于本 change）

- **不实现任何 GB28181 协议逻辑**：无 SIP 栈、无 SDP、无 MANSCDP+、无 PS 封装、无 RTP、媒体处理 —— 全部留待 Change 2+
- **不创建 Node 实例化框架**：节点模型留待 Change 4 (`node-abstraction`)
- **不实现 GB35114 / Digest 认证**：留待 Change 12 与 Change 2
- **不实现 X-GB-Ver 版本协商**：留待 Change 11 (`gb28181-2022-extensions`)
- **不实现媒体源**：留待 Change 8 (`media-sources`)
- **不实现动态目录 / 报警 / 录像 / 移动位置**：留待 Change 10 (`dynamic-sim-features`)
- **不实现 YAML 场景引擎**：留待 Change 15 (`scenario-engine`)
- **不引入级联路径处理**：留待 Change 9 (`cascade-and-multi-instance`)
- **不引入异常流与 pcap 导出**：留待 Change 13 (`exception-and-capture`)
- **不展开完整 Web UI**：本 change 仅交付"版本 + 状态 + 日志流"三件套；其余页面留待 Change 14

## 路线图定位

| 字段 | 值 |
|---|---|
| 路线图阶段编号 | **Change 1 / 15** |
| 前置依赖 | 无 |
| 解锁的下一 change | Change 2 (`core-sip-stack`)、Change 14 (`web-management-ui`) 可并行启动；其余 change 都依赖本 change 的基础设施 |
| 第一阶段终点 | **是**（本 change 完成 = 第一阶段"项目骨架 + Web"交付完毕，但尚不能收发任何 SIP 信令） |
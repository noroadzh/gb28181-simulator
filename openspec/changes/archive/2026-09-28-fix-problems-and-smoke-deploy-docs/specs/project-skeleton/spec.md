# Spec Delta

## ADDED Requirements

### Requirement: IDE Workspace Diagnostics Stay At Zero
当仓库被 IDE 打开时，Problems 面板对仓库内任何 `.go` 文件 MUST 报告零诊断；对仓库外的临时文件（`/tmp/*.go`、`~/cate/*.go` 等用户独立打开的文件） MUST 不被纳入仓库诊断列表。`.vscodeignore` 与 `.gitignore` MUST 屏蔽这些路径；`go.mod` 中所有 import 的第三方依赖 MUST 为直接依赖或由直接依赖显式传递；`go vet ./...`、`go build ./...`、`gopls check ./...` 在仓库根目录的输出 MUST 为空。

#### Scenario: Open repository fresh in CodeBuddy
- **WHEN** 用户在 IDE 中打开仓库根目录（不打开仓库外临时文件）
- **THEN** Problems 面板显示的诊断条目数为 0
- **AND** 终端中 `go vet ./...` 与 `go build ./...` 输出均为空

#### Scenario: Open repository with stray /tmp files
- **WHEN** 用户在 IDE 中除仓库外另外打开了 `/tmp/brace_scan.go` 等临时 Go 文件
- **THEN** 这些文件不进入仓库的诊断列表，Problems 面板依然为空

#### Scenario: go.mod direct dependencies only
- **WHEN** 运行 `go mod tidy`
- **THEN** `go.mod` 中 `require` 段所有非 `// indirect` 标注的模块都是仓库代码直接 import 的依赖
- **AND** `go list -m -mod=mod -f '{{if not .Indirect}}{{.Path}}{{end}}'` 输出覆盖所有仓库 `import` 语句

### Requirement: One-Command Smoke Baseline
仓库 MUST 提供一个 `make smoke` 目标（或等价的 `scripts/smoke.sh`），能在 Linux/macOS/Windows 三类开发机上完整执行：① Go 单元测试（`go test -race -count=1 ./...`）；② Go 静态检查（`go vet ./...`）；③ Go 单平台构建（`go build ./...`）；④ 跨平台编译（至少覆盖 linux/amd64、linux/arm64、darwin/amd64、windows/amd64 四种组合）；⑤ 前端构建（`cd web && npm ci && npm run build`，产物落 `web/dist/`）；⑥ 端到端冒烟（启动构建好的二进制，探测 `/healthz`、日志 WS、`/api/faults` 三个端点，返回 200 后退出）。每一步的退出码、耗时、产物路径 MUST 汇总到 `docs/smoke-results.json`。

#### Scenario: Run make smoke on Linux
- **WHEN** 在 Linux amd64 开发机上执行 `make smoke`
- **THEN** 全部 6 个子步骤依次成功（退出码 0）
- **AND** `docs/smoke-results.json` 存在，包含 6 个 step 对象，每个对象有 `name`、`status`、`duration_ms`、`artifacts` 字段
- **AND** 整个流程总耗时 ≤ 8 分钟（CI 中可放宽）

#### Scenario: Cross-compile produces all four targets
- **WHEN** `make smoke` 的跨平台编译步骤执行
- **THEN** `build/` 目录下出现 `gb28181-simulator-linux-amd64`、`gb28181-simulator-linux-arm64`、`gb28181-simulator-darwin-amd64`、`gb28181-simulator-windows-amd64.exe` 四个产物
- **AND** 每个产物的 `file` 命令报告其声明的目标平台

#### Scenario: e2e health probe succeeds
- **WHEN** e2e 步骤启动刚构建的二进制并等待其健康
- **THEN** 探测 `GET /healthz` 返回 200 且响应体包含 `{"status":"ok"}`
- **AND** 探测 `GET /api/faults` 返回 200
- **AND** WebSocket 端点 `/ws/logs` 在 3 秒内完成握手并收到至少一条日志消息

#### Scenario: smoke failure leaves diagnostic trail
- **WHEN** 任一子步骤退出码非零
- **THEN** `make smoke` 整体退出码为该子步骤的退出码
- **AND** `docs/smoke-results.json` 中对应 step 的 `status` 为 `"failed"`，`error` 字段包含该子步骤的最后 200 行输出

### Requirement: Smoke Test Documentation Published
仓库 MUST 在 `docs/smoke-test.md` 提供 Markdown 冒烟测试文档，覆盖：① 测试矩阵（19 个 capability 与冒烟用例的对应关系）；② 执行步骤（本地 `make smoke` 与 CI 触发）；③ 结果解读（`docs/smoke-results.json` schema 详解）；④ 失败排查（按诊断来源分类的修复指引）；⑤ 附录（历史报告归档位置）。

#### Scenario: First-time contributor reads smoke-test.md
- **WHEN** 新贡献者按 README 指引打开 `docs/smoke-test.md`
- **THEN** 文档首段说明 smoke 与 unit test 的边界（smoke 验证端到端可用，unit test 验证字节级正确）
- **AND** 文档的"测试矩阵"章节列出全部 19 个 capability 及其冒烟探针

#### Scenario: CI runs smoke and uploads artifact
- **WHEN** GitHub Actions 的 `smoke` job 完成
- **THEN** `docs/smoke-results.json` 被作为工件上传，可在 CI run 详情页下载
- **AND** 文档"CI 集成"章节给出该工件在 UI 中的下载路径

### Requirement: Dual-Form Deployment Documentation Published
仓库 MUST 提供两种部署形态的 Markdown 文档：`docs/deploy-linux.md`（Linux 单机 systemd unit 部署）与 `docs/deploy-docker-compose.md`（Docker Compose 多节点部署）。两文档 MUST 包含：① 前置依赖（OS 包、Go 版本、Docker/Compose 版本）；③ 安装步骤（含下载/构建、用户与目录、systemd unit 或 Compose 文件）；④ 配置文件位置与最小可用样例；⑤ 启动/停止/查看日志；⑥ 升级与回滚步骤；⑦ 常见故障排查。

#### Scenario: Operator deploys single node on Linux
- **WHEN** 运维按 `docs/deploy-linux.md` 在 CentOS 8 / Ubuntu 22.04 / Debian 12 任一 Linux 上部署
- **THEN** 按文档步骤执行后 `systemctl status gb28181-simulator` 显示 `active (running)`
- **AND** `curl http://localhost:8080/healthz` 返回 `{"status":"ok"}`

#### Scenario: Operator deploys multi-node topology via Compose
- **WHEN** 运维按 `docs/deploy-docker-compose.md` 在装有 Docker Engine 24+ 与 Compose v2 的主机上执行 `docker compose up -d`
- **THEN** Compose 文件定义的所有服务进入 `healthy` 状态
- **AND** `docker compose ps` 显示的端口映射与文档"网络规划"表格一致
- **AND** 文档中的 `docs/deploy-docker-compose.md#网络规划` 章节描述了节点之间 SIP 端口、媒体端口、HTTP 端口的可达性

#### Scenario: Operator rolls back documented upgrade
- **WHEN** 运维执行文档中描述的升级步骤（systemd 二进制替换或 Compose `pull` + `up`）
- **THEN** 出现不兼容升级时能按"回滚步骤"小节回退到上一版本（systemd 用 `systemctl revert` 或重新部署旧 unit；Compose 用 `docker compose down && docker compose up -d` 旧 tag）
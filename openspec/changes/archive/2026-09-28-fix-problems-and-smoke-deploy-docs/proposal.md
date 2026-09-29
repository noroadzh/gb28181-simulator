# Proposal

## Why

仓库当前在 IDE（VS Code / CodeBuddy）的 Problems 面板中出现 12 条诊断信息，其中 5 条源自用户在工作区之外打开的临时 Go 文件（`/tmp/brace_scan.go`、`/tmp/check_parse.go`、`~/cate/cascade_test.go`、`~/cate/model.go`），1 条为 `go.mod` 中将 `github.com/abema/go-mp4` 列为间接依赖的告警；另外 7 条涉及本仓库内文件的诊断虽经 `go vet ./...` 与 `go build ./...` 验证为零输出，但 IDE LSP 缓存仍在展示。同时项目此前未沉淀过统一的冒烟测试脚本与部署文档，前端 `web/dist/` 目录为空，README 也未指引如何在 Linux 单机或 Docker Compose 多节点场景下完成部署。本次变更借完成阶段 1（`init-project-skeleton`）+ 阶段 14（`web-management-ui`）后第一个"工程治理"窗口，将工作区诊断噪声收敛为零，补齐"一次命令即可跨平台冒烟"的工程基线，并交付可直接交付给运维的部署文档。

## What Changes

- **清理工作区诊断噪声**
  - 新增 `.gitignore` 与 `.vscodeignore` 规则，屏蔽 IDE 缓存的外部 `/tmp/*.go` 与 `~/cate/*.go` 文件
  - 通过 `go mod tidy` 将 `github.com/abema/go-mp4` 提升为直接依赖（在 mp4-and-info-extensions 归档中已实现，本 change 只需复核）
  - 对仓库内 IDE LSP 残留诊断（media、scenario、app 子包）执行 `gopls` 缓存清理与重新索引，确认 `gopls check` 输出为空
- **构建冒烟测试基线**
  - 新增 `scripts/smoke.sh`（bash，POSIX 可移植）：依次执行 `go test -race -count=1 ./...`、`go vet ./...`、`go build ./...`、跨 4 平台编译（linux/amd64、linux/arm64、darwin/amd64、windows/amd64）、前端 `npm ci` + `npm run build`、`go run ./cmd/...` 健康检查 + HTTP `/healthz` 探测，产物汇总到 `docs/smoke-results.json`
  - 暴露一个 `Makefile` 目标 `make smoke` 复用上述脚本
  - 端到端测试：在 `internal/test/e2e` 中新增一个基于 `httptest` 的轻量级冒烟（验证 `/healthz`、日志 WS、`/api/faults` 三个端点在新启动的进程上可达）
- **生成冒烟测试文档**
  - 输出 `docs/smoke-test.md`（Markdown）：覆盖范围（19 个 capability 矩阵）、执行步骤（本地 + CI）、结果解读、失败排查、附录（JSON 报告 schema）
- **生成部署文档（双形态）**
  - 输出 `docs/deploy-linux.md`：Linux 单机 systemd unit 部署（含前置依赖、用户/目录、二进制放置、systemd unit、systemctl 操作、回滚步骤）
  - 输出 `docs/deploy-docker-compose.md`：Docker Compose 多节点部署（含镜像构建、节点拓扑、`.env` 编排、持久化卷、网络规划、Promtail/Loki 可选对接、滚动升级）
- **CI 集成**
  - 在 `.github/workflows/ci.yml` 的矩阵任务后追加一个 `smoke` 任务，运行 `make smoke` 并上传 `docs/smoke-results.json` 作为工件

## Capabilities

### New Capabilities

（本 change 不新增独立 capability；冒烟测试与文档属于 `project-skeleton` 的工程基础设施扩展。）

### Modified Capabilities

- `project-skeleton`：扩展"仓库可维护性"面，新增 4 条 requirement：① IDE 工作区诊断保持为零噪声；② 仓库提供一键 `make smoke` 冒烟基线（Go 后端 + 前端构建 + 跨平台编译 + e2e）；③ 仓库交付 `docs/smoke-test.md` 冒烟测试文档；④ 仓库交付 Linux 单机部署文档与 Docker Compose 多节点部署文档。

## Impact

- 新增文件
  - `scripts/smoke.sh`
  - `Makefile`（若不存在则新增；若已存在则扩展 `smoke` 目标）
  - `.vscodeignore`（若不存在则新增；用于屏蔽 `/tmp/*.go` 扫描）
  - `internal/test/e2e/smoke_test.go`
  - `docs/smoke-test.md`
  - `docs/deploy-linux.md`
  - `docs/deploy-docker-compose.md`
  - `docs/smoke-results.json`（运行产物，gitignore）
  - `Dockerfile`（若不存在则新增；用于 Compose 场景）
  - `docker-compose.yml`（若不存在则新增；多节点拓扑示例）
- 修改文件
  - `.github/workflows/ci.yml`：追加 `smoke` job 并上传工件
  - `go.mod` / `go.sum`：`go mod tidy` 复核（已归档的 mp4-and-info-extensions 负责把 `go-mp4` 升级为直接依赖）
  - `web/dist/`：冒烟构建产物（gitignore）
  - `README.md`：在"开发与测试"小节引用 `make smoke` 与文档链接
- 受影响系统
  - CI（GitHub Actions）：增加 `smoke` job，约 5–8 分钟
  - 部署：新增 systemd unit 模板与 Compose 拓扑；不影响运行时二进制契约

## Non-goals

- 不修复 IDE 打开的 `/tmp/brace_scan.go`、`/tmp/check_parse.go` 等 **仓库外** 文件本身的编译错误（这些是用户在 IDE 中误打开的临时文件，与本项目无关；本 change 仅在 `.vscodeignore` / `.gitignore` 屏蔽它们对诊断面板的污染）
- 不引入新的 GB28181 协议能力，不修改 SIP/SDP/PS/MANSCDP 任一路径
- 不重写前端技术栈（仍为 Vue 3 + Element Plus + Vite）
- 不实现 K8s Helm Chart（仅 Docker Compose；K8s 留待后续 change）
- 不强制升级 Go 工具链或 Node 版本到新主版本（仅复核当前矩阵兼容）
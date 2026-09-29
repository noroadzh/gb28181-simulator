# Design

## Context

本 change 的动机在 proposal.md 中已阐述。核心约束：
- **纯 Go 无 CGO**：smoke 脚本中的跨平台编译使用 `CGO_ENABLED=0`，前端构建走 Node.js，均不引入新语言运行时依赖
- **单文件分发**：二进制包含嵌入的前端资源（`embed.FS`），smoke 不需要单独部署前端服务
- **无 K8s**：仅覆盖 systemd（单机）和 Docker Compose（多节点），K8s 留待后续 change

## Goals / Non-Goals

**Goals:**
- Problems 面板在仓库内对所有 `.go` 文件报告零诊断
- `make smoke` 一条命令完成 Go 后端（测试+lint+构建+跨平台编译）+ 前端构建 + e2e 冒烟
- 输出 `docs/smoke-results.json` 结构化报告，CI 上传为工件
- 交付 `docs/smoke-test.md`、`docs/deploy-linux.md`、`docs/deploy-docker-compose.md` 三份文档
- CI 集成 smoke job，不破坏现有 matrix build

**Non-Goals:**
- 不引入 K8s Helm Chart
- 不写 golden test 或 protocol-level e2e（这些属于各 change 的单元测试职责）
- 不改任何 GB28181 协议实现代码（sip/SDP/PS/MANSCDP 均不涉及）
- 不引入新语言运行时（Rust/WebAssembly 等）

## Decisions

### D1: Smoke 脚本放在 `scripts/smoke.sh`，由 `Makefile` 暴露

**选择**：Bash 脚本 + Makefile 目标。

**理由**：项目已有 `Makefile` 约定（context 中"CI: lint + unit test + build matrix"），Go 开发者对 `make` 普遍熟悉。smoke 不需要 Go 本身来跑（前端 npm build 依赖 Node），bash 脚本天然跨平台（Linux/macOS 原生，Windows 用 Git Bash 或 WSL 兼容）。

**替代**：纯 Go 脚本（`go run scripts/smoke.go`）——缺点：Node 构建步骤仍需 bash 调用，纯 Go 徒增复杂度。

### D2: e2e 冒烟基于 `httptest`，不启动真实多进程

**选择**：Go `net/http/httptest` + 内嵌 `embed.FS` 启动。

**理由**：smoke 的 e2e 只需验证 HTTP 端点（`/healthz`、WebSocket `/ws/logs`、`/api/faults`）可达，不验证 GB28181 协议交互。`httptest.Server` 启动快（毫秒级），不依赖端口，可并行于 CI。进程级别的真实二进制启动（`exec.Command`）留给各 change 的集成测试。

**替代**：实际 `exec.Command` 启动二进制——缺点：需分配端口、等待启动、清理进程，CI 中增加 30–60 秒。

### D3: `.vscodeignore` 使用 glob 屏蔽仓库外路径

**选择**：`.vscodeignore` 中写入 `/tmp/**`、`~/cate/**` 等路径 glob。

**理由**：`.vscodeignore` 控制 LSP 扫描范围，不影响 `.gitignore`。对仓库外临时文件只需屏蔽其进入诊断，不需修改工作区本身。`.gitignore` 则屏蔽 `build/`、`web/dist/`、`docs/smoke-results.json` 等构建产物。

**替代**：Workspace Trust / LSP 配置——缺点：需要用户手动配置，不可持续。

### D4: 跨平台编译产物输出到 `build/` 目录

**选择**：`build/` 目录集中管理。

**理由**：`build/` 与 `dist/`（前端产物）区分清晰。CI 上传工件时只打包 `build/` 和 `web/dist/`，不污染仓库根目录。

### D5: Smoke 结果 JSON schema

每 step 结构：
```json
{
  "name": "go-test",
  "status": "passed" | "failed" | "skipped",
  "duration_ms": 12345,
  "artifacts": ["build/gb28181-simulator-linux-amd64"],
  "error": "last 200 lines if failed"
}
```

顶层：
```json
{
  "version": "1.0",
  "timestamp": "2026-09-28T00:00:00Z",
  "total_duration_ms": 480000,
  "overall": "passed",
  "steps": [...]
}
```

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Windows CI runner 上 `make` 不可用（Git Bash/MSYS2 环境差异） | CI 用 `bash scripts/smoke.sh` 替代 `make smoke`；Makefile 仅作为开发机快捷入口 |
| `npm ci` 在网络受限 CI 环境超时 | CI job 配置 `actions/cache` 缓存 `web/node_modules`，超时设为 10 分钟 |
| `gopls` 诊断在某些 LSP 配置下仍有残留 | 在 `scripts/smoke.sh` 中加 `gopls check ./...` 并将失败计入 smoke 退出码；若 gopls 未安装则跳过并 warn |
| smoke 跨平台编译在 CI arm64 runner 上构建 darwin/amd64 失败 | CI matrix 仅在 amd64 runner 执行跨平台编译，arm64 runner 仅构建本机平台 |
| `web/dist/` 空导致前端嵌入二进制启动失败 | smoke 前端构建步骤强制 `npm ci && npm run build`，且 `embed.FS` 引用路径错误时 `go build` 失败 |

## Migration Plan

**实施顺序（对应 tasks.md 任务分组）：**

1. **IDE 诊断清理**（Group 1）：修改 `.vscodeignore` / `.gitignore`，运行 `go mod tidy`，执行 `gopls shutdown && gopls` 重启
2. **Smoke 脚本 + Makefile**（Group 2）：创建 `scripts/smoke.sh` 与 `Makefile` 目标，验证本地 `make smoke` 通过
3. **e2e 冒烟测试**（Group 3）：实现 `internal/test/e2e/smoke_test.go`，验证 `/healthz`/WS/`/api/faults`
4. **CI 集成**（Group 4）：在 `.github/workflows/ci.yml` 追加 `smoke` job，上传工件
5. **文档**（Group 5）：产出 `docs/smoke-test.md`、`docs/deploy-linux.md`、`docs/deploy-docker-compose.md`
6. **README 更新**（Group 6）：在"开发与测试"章节引用 `make smoke` 与文档路径

**回滚**：
- IDE 诊断清理：revert `.vscodeignore` / `.gitignore` diff
- Smoke 脚本：revert `scripts/smoke.sh` 和 `Makefile` diff
- CI 变更：revert `.github/workflows/ci.yml` diff
- 文档：revert `docs/` diff

所有变更在 commit 层面原子化，回滚时 `git checkout <commit>^ -- <file>` 即可。
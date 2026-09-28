# Tasks

## 1. IDE 诊断清理（问题面板 12 条）

- [x] 1.1 创建 `.vscodeignore` 屏蔽仓库外临时文件路径（`/tmp/**`、`~/cate/**`）并加入 `go.work` 排除项说明；验证：在 CodeBuddy 中重新加载窗口，Problems 面板对 `/tmp/brace_scan.go`、`/tmp/check_parse.go`、`~/cate/cascade_test.go`、`~/cate/model.go` 不再显示条目
- [x] 1.2 运行 `go mod tidy` 并确认 `go.mod` 中 `github.com/abema/go-mp4` 不再带 `// indirect` 标注；验证：`go list -m -f '{{if .Indirect}}indirect: {{.Path}}{{end}}' all` 输出中不包含 `go-mp4`，且 Problems 面板 `go.mod` 条目消失
- [x] 1.3 重启 gopls 重新索引仓库内 Go 文件；验证：`go vet ./...`、`go build ./...` 零输出，且 Problems 面板对 `internal/adapter/media`、`internal/adapter/scenario`、`internal/app` 下文件无残留告警

## 2. 冒烟脚本与 Makefile 目标

- [x] 2.1 创建 `scripts/smoke.sh`，依次执行 6 个子步骤并汇总 `docs/smoke-results.json`（① `go test -race -count=1 ./...` ② `go vet ./...` ③ `go build ./...` ④ 4 平台交叉编译到 `build/` ⑤ `web npm ci + npm run build` ⑥ e2e 冒烟），每步失败立即退出并记录 stderr 最后 200 行；验证：本地执行 `bash scripts/smoke.sh` 退出码 0，且 `docs/smoke-results.json` 含 6 个 step 对象
- [x] 2.2 在 `Makefile` 新增 `smoke` 目标（调用 `scripts/smoke.sh`），并加入 `.PHONY`；验证：`make smoke` 与 `bash scripts/smoke.sh` 产出一致
- [x] 2.3 更新 `.gitignore` 追加 `build/`、`docs/smoke-results.json`、`web/dist/`；验证：`git status` 不显示这些路径为未跟踪

## 3. e2e 冒烟测试

- [x] 3.1 创建 `internal/test/e2e/smoke_test.go`，用 `httptest` 启动完整 server 并探测 `/healthz`、`GET /v1/faults`（或 `/api/faults` 兜底）、`/ws/logs` 三端点；验证：`go test ./internal/test/e2e/...` 退出码 0
- [x] 3.2 在 `scripts/smoke.sh` 第 ⑥ 步中调用 `go test ./internal/test/e2e/...` 作为 e2e 冒烟入口；验证：`bash scripts/smoke.sh` 时 step `e2e` 的 `status` 为 `"passed"`

## 4. CI 集成

- [x] 4.1 在 `.github/workflows/ci.yml` 追加 `smoke` job（依赖 `lint`、`test`，运行 `bash scripts/smoke.sh`，上传 `docs/smoke-results.json` 与 `build/` 为工件）；验证：push 到 main 后 GitHub Actions 中出现 `smoke` job 且绿
- [x] 4.2 为 `web/node_modules` 配置 `actions/cache` 以加速 `npm ci`；验证：第二次 CI run 中 `npm ci` 耗时 < 60 秒

## 5. 冒烟测试文档

- [x] 5.1 创建 `docs/smoke-test.md`，含五章节：测试矩阵（19 个 capability 与冒烟探针对应表）、本地执行步骤（`make smoke`）、CI 触发步骤、结果解读（`docs/smoke-results.json` schema）、失败排查（按 6 步分类）；验证：文档存在且覆盖 19 个 capability
- [x] 5.2 在 `docs/smoke-test.md` 附录注明历史 smoke 报告归档路径（`docs/smoke-archive/`）及命名规则（`smoke-YYYYMMDD-HHmmss.json`）；验证：附录段落存在

## 6. 部署文档（Linux 单机 + Docker Compose）

- [x] 6.1 创建 `Dockerfile`（多阶段：node 构建前端 + go 构建二进制 → alpine 运行时）；验证：`docker build -t gb28181-simulator:dev .` 成功
- [x] 6.2 创建 `docker-compose.yml`，定义至少 2 节点拓扑（1 个 platform-large + 1 个 device 节点，同 network，暴露 SIP 5060/UDP、HTTP 8080/TCP）；验证：`docker compose config` 语法合法，`docker compose up -d` 后 `docker compose ps` 显示两服务 healthy
- [x] 6.3 创建 `docs/deploy-linux.md`：前置依赖（Ubuntu 22.04 / Debian 12 / CentOS 8 任一）、二进制下载与放置（`/opt/gb28181-simulator/bin/`）、专用用户与目录、`gb28181-simulator.service` systemd unit、`systemctl start/stop/status/journalctl`、升级与回滚（`systemctl revert` 或旧 unit 重部署）、常见故障（端口占用 / 权限 / 依赖缺失）；验证：按文档在一台全新 Ubuntu 22.04 上部署后 `curl localhost:8080/healthz` 返回 `{"status":"ok"}`
- [x] 6.4 创建 `docs/deploy-docker-compose.md`：前置依赖（Docker Engine 24+ / Compose v2）、镜像构建或拉取、`.env` 编排（节点身份、端口、持久化卷）、网络规划（SIP UDP/TCP、媒体 RTP 端口段、HTTP 端口）、持久化卷挂载、启动停止升级回滚、常见故障（端口冲突 / 卷权限 / 网络隔离）；验证：按文档在装有 Docker 24+ 的主机上 `docker compose up -d` 后两节点 healthy
- [x] 6.5 两份文档均含"最小可用配置样例"段落（YAML 或环境变量片段），与 `internal/platformconfig` 当前 schema 一致；验证：样例片段能通过启动时的 config 校验

## 7. README 与最终回归

- [x] 7.1 更新 `README.md`：新增"开发与测试"章节，引用 `make smoke`、`docs/smoke-test.md`、`docs/deploy-linux.md`、`docs/deploy-docker-compose.md`；验证：README 中链接可点击跳转到对应文件
- [x] 7.2 全量回归：`make smoke` 本地全绿，`docs/smoke-results.json` 显示 6 步全部 `passed`，总耗时 ≤ 8 分钟；验证：将 JSON 附到 PR 描述
- [x] 7.3 在 CodeBuddy IDE 中重新打开仓库并加载窗口；验证：Problems 面板对仓库内所有文件显示 0 诊断

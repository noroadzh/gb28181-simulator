---
name: change1-test-report
overview: 编写 Change 1（init-project-skeleton）的端到端测试报告：重跑全部验收命令后生成 reports/change1-test.md，包含 11 个任务的逐条验收矩阵与证据。
todos:
  - id: verify-dist-fix
    content: 验证 internal/webui/dist.go 修复已生效并强制 rebuild 二进制使 embed 内容生效
    status: completed
  - id: rerun-go-tests
    content: 重跑 go test -race ./... 收集 logger/config/storage/api 全部输出
    status: completed
  - id: e2e-curl-ws
    content: 启动服务并执行 curl health/version/首页/SPA/静态资源 + WS 脱敏验证
    status: completed
  - id: cross-compile
    content: 执行 make release-matrix 跨平台编译并启动各平台产物 smoke test
    status: completed
  - id: write-report
    content: 撰写 reports/change1-test.md 完整 11 任务验收矩阵并嵌入证据
    status: completed
  - id: tick-tasks
    content: 更新 openspec/changes/init-project-skeleton/tasks.md checkbox 与 evidence 链接
    status: completed
---

## 用户需求

为 Change 1（`init-project-skeleton`）生成归档前的完整端到端测试报告，作为后续 `openspec-archive` 的验收证据。

## 报告产出

- 文件位置：`/Users/noroadzh/code/jfys/go/gb28181-simulator/reports/change1-test.md`（仓库内结构化 Markdown）
- 覆盖范围：完整 11 个任务、35 条验收点，逐条对照 `openspec/changes/init-project-skeleton/tasks.md`
- 每条验收点包含：复跑命令、fresh 输出、通过/失败判定、产物/日志路径指针

## 验收维度

1. 仓库 bootstrap（go.mod、目录骨架、.gitignore）
2. 依赖与编译（echo/websocket/viper/sqlite + CGO_ENABLED=0）
3. internal/logger（5 级别过滤、password 脱敏、Hub fan-out）
4. internal/config（Defaults、viper 加载、XDG/AppData 回退）
5. internal/storage（Bootstrap 幂等、WAL/foreign_keys、t.TempDir）
6. internal/api（health/version JSON、WS 多订阅者、graceful shutdown）
7. Web 前端（Vue3+ElementPlus 构建、embed FS、SPA fallback、静态资源 MIME）
8. CLI 入口（-version 三行、SIGINT/SIGTERM 优雅关闭）
9. CI workflows（ci.yml + release.yml YAML 结构 + `make release-matrix` 本地可执行）
10. 文档/Makefile（README 完整段落、目标齐全）
11. 端到端：curl health/version/静态资源、WS 脱敏 `***REDACTED***`、跨平台编译可启动

## 副产物

- 同步更新 `openspec/changes/init-project-skeleton/tasks.md` 的 checkbox（全部勾选 + 报告链接）。

## 验证策略

所有本地可执行命令**重跑一遍**，fresh 输出落到 `/tmp/gb28181-e2e/` 后嵌入报告代码块。

### 关键命令清单

- 1.1~1.3：`go mod tidy`、`tree -L 2 -I 'node_modules|web/dist'`、`git status --ignored`
- 2.1~2.3：`go list -m all`、`CGO_ENABLED=0 go build ./...`、`go vet ./...`（如无 golangci-lint）
- 3.1~3.3：`go test -race -v ./internal/logger/...`
- 4.1~4.3：`go test -race -v ./internal/config/...`
- 5.1~5.3：`go test -race -v ./internal/storage/...`
- 6.1~6.3：`go test -race -v ./internal/api/...`
- 7.1~7.3：`npm --prefix web install`、`npm --prefix web run build`、`ls internal/webui/embed/dist/`
- 8.1~8.2：`go build -o bin/gb28181-simulator ./cmd/gb28181-simulator`、`./bin/gb28181-simulator -version`
- 9.1~9.3：解析 `.github/workflows/{ci,release}.yml` 结构 + `make -n release-matrix`
- 10.1~10.2：`cat README.md`、`make -n fmt lint test web build`
- 11.1：`curl /api/health`、`curl /api/version`、`curl /`、`curl /some/spa/path`、`curl /assets/index-*.{css,js}`
- 11.2：临时 Go 脚本订阅 `ws://127.0.0.1:18181/api/logs/stream` + 触发含 `password` 字段的日志，验证 `***REDACTED***`
- 11.3：`make release-matrix`（darwin/linux/windows × amd64/arm64），本机可启动的平台执行 smoke test

### 报告结构

```
# Change 1 Test Report — init-project-skeleton

- Meta: 日期/分支/commit
- Summary: 35 条 PASS/FAIL/SKIPPED 计数
- Acceptance Matrix: 表格形式列出 35 行
- Per-section raw output: 代码块原样嵌入
- Limitations: GitHub Actions 触发需远程、Linux/Windows 二进制 smoke test 受 host OS 限制
```

### 已知限制（报告中需明确标注）

- GitHub Actions 本身无法本地触发 → "Workflow YAML validated locally; full PR/tag run pending remote"
- Linux/Windows 二进制在 macOS 上无法原生执行 → "binary built; smoke test deferred to CI"

# Tasks

> 本 change 是 Change 1（路线图 15 步中的第一步），目标是把"仓库结构 + 依赖 + CI + HTTP/WS 骨架 + Web 嵌入"一次性落地，**不写任何 GB28181 协议代码**。
> 任务粒度 ≤ 2 小时；每条任务都含验收标准；本 change 无 SIP/SDP/PS 字节格式工作，故无 golden test 要求。

## 1. Repo bootstrap

- [x] 1.1 在仓库根创建 `go.mod`（module 路径占位为 `github.com/your-org/gb28181-simulator`，确认 `go 1.21` toolchain 在 `go.mod` 与 CI 一致) 并验证 `go mod tidy` 无错误
- [x] 1.2 创建目录骨架 `cmd/gb28181-simulator/`、`internal/{logger,config,storage,api}/`、`web/{src,dist}/`、`docs/.keep`、`configs/.keep` 并验证 `tree -L 2` 列出全部
- [x] 1.3 添加 `.gitignore`（覆盖 `dist/`、`web/dist/`、`*.log`、`*.db`、`.idea/`、`node_modules/`）并验证 `git status` 不再列出被忽略文件

## 2. Dependencies

- [x] 2.1 通过 `go get github.com/labstack/echo/v4 github.com/gorilla/websocket github.com/spf13/viper modernc.org/sqlite` 添加依赖并验证 `go.sum` 已生成
- [x] 2.2 运行 `CGO_ENABLED=0 go build ./...` 并验证跨平台无 CGO 编译警告
- [x] 2.3 添加 `.golangci.yml`（启用 `govet`、`staticcheck`、`gofmt`、`goimports`）并验证 `golangci-lint run ./...` 通过

## 3. Internal package: logger

- [x] 3.1 在 `internal/logger` 中实现 `Level`（`trace/debug/info/warn/error` 与 slog `Level` 映射）、`Options{Level, File, AddSource, RedactKeys}` 并验证 `go test ./internal/logger/... -run TestOptions -v` 通过
- [x] 3.2 实现 `MultiHandler`（同时输出到文件异步 writer 与 Hub 订阅通道，含字段名黑名单脱敏）并验证单元测试覆盖：trace/debug/info/warn/error 过滤、`password` 字段脱敏、Hub fan-out 至少 3 个订阅者
- [x] 3.3 暴露 `Init(opts Options) error` 与全局 `Hub().Subscribe() / Unsubscribe()`，并验证同一日志条目同时落到文件与所有 WS 订阅者（用 `t.TempDir()` + 一个伪订阅通道）

## 4. Internal package: config

- [x] 4.1 定义 `Config{HTTP{Host, Port}, Log{Level, File, RedactKeys}, Storage{Path}}` 并验证 `go test ./internal/config/... -run TestConfig_Defaults -v` 通过
- [x] 4.2 实现基于 viper 的 `Load(path string) (*Config, error)`，含 `GB28181_SIMULATOR_` 前缀、`.` → `_` 替换、XDG/AppData 路径解析，并验证：YAML 加载、环境变量覆盖端口、XDG 默认回退 三条单测通过
- [x] 4.3 暴露 `DefaultConfigPath() string` 并验证在 `$XDG_CONFIG_HOME` 设置与未设置时分别返回预期路径

## 5. Internal package: storage

- [x] 5.1 在 `internal/storage` 实现 `Bootstrap(path string) (*sql.DB, error)`，启用 `journal_mode=WAL` 与 `foreign_keys=ON`，执行 `schema.sql` 中若干 `CREATE TABLE IF NOT EXISTS`（业务表留空，仅占位注释）并验证幂等：连续调用两次不报错、文件存在
- [x] 5.2 暴露 `DefaultDBPath(cfg Config) string` 并验证 XDG/AppData 回退与 `storage.path` 显式配置两条单测通过
- [x] 5.3 编写 `storage_test.go` 覆盖：(a) 在 `t.TempDir()` 中调用 Bootstrap 文件被创建；(b) 第二次调用不抛错

## 6. Internal package: api

- [x] 6.1 实现 `NewServer(cfg config.Config, hub *http.Hub, version string) *echo.Echo`，注册 `GET /api/health` 与 `GET /api/version` 并验证：`/api/health` 返回 `{"status":"ok"}`、`/api/version` JSON 含 4 字段
- [x] 6.2 实现 `WSHandler(hub)` 将 gorilla/websocket 升级为订阅者，使用 buffered channel + 非阻塞 select 跳过慢订阅者；并验证 100 条日志在 5 个并发订阅者时全部收到至少 1 条（允许慢订阅者丢消息，但全局 hub 不阻塞）
- [x] 6.3 在 `internal/api/server.go` 暴露 `Run(addr string) error` 与 `Shutdown(ctx context.Context) error` 并验证：`httptest.NewServer` 启动后可被优雅关闭

## 7. Web frontend

- [x] 7.1 在 `web/` 初始化 Vue3 + Vite 工程（`package.json` 含 `vue@^3.4`、`element-plus@^2.7`、`vite@^5`），并验证 `npm install` 成功
- [x] 7.2 实现单页 Dashboard：`App.vue` 显示版本（`/api/version` 拉取）、状态（`/api/health` 拉取 + 心跳）、实时日志面板（WS `/api/logs/stream`）；并验证 `npm run build` 产出 `web/dist/index.html` + assets
- [x] 7.3 在 `internal/webui/dist.go` 中通过 `//go:embed all:dist` 暴露 `FS fs.FS`，在 `/` 与 fallback 路径下返回 `index.html`（`text/html; charset=utf-8`），其他路径从 FS 读取（`Content-Type` 推断）；并验证 `httptest` 中 `GET /` 返回 200 与内嵌 HTML

## 8. CLI entrypoint

- [x] 8.1 在 `cmd/gb28181-simulator/main.go` 中串联：解析 `-config` flag（默认 `configs/config.yaml`）、`config.Load`、`logger.Init`、`storage.Bootstrap`、`api.NewServer().Run(addr)`、`SIGINT/SIGTERM` 优雅关闭（`echo.Shutdown` + DB 关闭）；并验证 `--help` 输出与本地默认配置可成功 listen `127.0.0.1:18080`
- [x] 8.2 在 main 中暴露 `-version` 标志（默认 `0.1.0-dev`，ldflags 注入 `commit`、`builtAt`）并验证 `gb28181-simulator -version` 输出 3 行（version / commit / builtAt）

## 9. CI workflows

- [x] 9.1 添加 `.github/workflows/ci.yml`：jobs = `lint`（golangci-lint v1.57）、`test`（`go test ./...`，ubuntu-latest）、`build`（matrix: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64，导出 artifact）；并验证在 PR 上每个 job 都运行
- [x] 9.2 添加 `.github/workflows/release.yml`：触发条件 `tags: ['v*']`，复用上一步 build 的产物，加入 `.sha256` 并通过 `softprops/action-gh-release@v2` 发布；并验证在 `v0.0.0-test` tag 上产出 5 个 sha256 文件
- [x] 9.3 在 CI 中显式设 `CGO_ENABLED=0` 并验证日志中无 `cgo` 警告

## 10. Documentation & Makefile

- [x] 10.1 编写 `README.md`（构建、运行、配置位置、CI 状态徽章占位、开发提示）并验证 `README.md` 渲染无明显笔误
- [x] 10.2 添加 `Makefile`：目标 `build`（`CGO_ENABLED=0 go build -o bin/gb28181-simulator ./cmd/gb28181-simulator`）、`test`（`go test ./...`）、`web`（`npm --prefix web install && npm --prefix web run build`）、`run`（先 web 后 build 再 `./bin/gb28181-simulator -config configs/config.yaml`）、`clean`、`fmt`、`lint`；并验证 `make web && make build` 成功

## 11. End-to-end verification

- [x] 11.1 启动 `bin/gb28181-simulator -config configs/config.yaml`，`curl http://127.0.0.1:18080/api/health` 返回 `{"status":"ok"}`，`/api/version` 返回 4 字段 JSON；浏览器访问 `/` 渲染 Dashboard 且实时显示启动阶段日志
- [x] 11.2 用 `wscat` 或浏览器 console 连接 `ws://127.0.0.1:18080/api/logs/stream`，触发一条 info 日志（如 kill -USR1 或 reload），并验证消息秒级到达、敏感字段（如配置里的 password 字段被自动写入测试用例）显示为 `***REDACTED***`
- [x] 11.3 跨平台本地验证（仅开发者机可用平台）：`make build` 后分别在本机 OS 与 GOOS=linux GOARCH=arm64 交叉构建，产物均可启动并响应 `/api/health`

---

## Evidence

- Full report: [`../../reports/change1-test.md`](../../reports/change1-test.md)
  (commands re-run on 2026-09-23, fresh console output embedded; `21/21`
  Go tests pass, 5-platform matrix builds succeed, live `curl` + WS
  redaction verified against `127.0.0.1:18181`).
- Defects fixed during verification:
  - `internal/webui/dist.go` — `SpaHandler` embed path bug.
  - `cmd/gb28181-simulator/signals_{unix,windows}.go` — Windows build
    tags for the SIGUSR1 ping handler.
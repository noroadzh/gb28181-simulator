# Design

## Context

本 change 是 15 步路线图的第一步（见 `proposal.md` 中的"Why"），目标是把后续 14 个 GB28181 协议 change 共享的工程基础设施（仓库布局、依赖、CI、日志、HTTP/WS 骨架、前端嵌入、配置与持久化）一次性落地。当前 `openspec/` 已经存在并写入完整 `context`，但仓库根目录没有任何 Go 代码、没有 CI、没有前端构建产物。约束：

- Go 1.22+ 纯 Go，无 CGO
- 跨平台目标：Linux amd64/arm64、macOS amd64/arm64、Windows amd64
- 前端经 `embed.FS` 嵌入，单文件分发
- 所有依赖均为后续 change 也将复用的稳定库
- 本 change 不实现任何 GB28181 协议逻辑

## Goals / Non-Goals

**Goals:**

- 启动一个进程即可监听 HTTP + WS、加载 YAML 配置、连接 SQLite、推送分级日志到前端 Dashboard
- 所有平台 `CGO_ENABLED=0` 成功 `go build`
- CI 在 push 与 PR 时全矩阵构建并跑 lint + 单测
- 前端构建产物可被嵌入并随二进制分发
- 4 个内部包（`logger`、`config`、`storage`、`api`）的公共接口稳定，便于 Change 2+ 直接复用

**Non-Goals（设计层面）：**

- 不引入任何 SIP、SDP、MANSCDP+、PS、RTP、媒体类型
- 不引入 Node 模型（留给 Change 4）
- 不引入 GB35114 / Digest / TLS（留给 Change 2、12）
- 不引入日志订阅过滤 API（仅暴露最小日志流；过滤留给 Change 14）
- 不引入 sqlite 业务表（仅 schema 引导；业务表留给 Change 5+）

## Decisions

### D1. Echo + gorilla/websocket 作为唯一 HTTP/WS 框架

- **决定**：`internal/api` 使用 `github.com/labstack/echo/v4` 注册 HTTP 路由；`/api/logs/stream` 使用 `github.com/gorilla/websocket` 升级 WS。
- **理由**：Echo 中间件生态完整（Recover / RequestID / CORS），可直接注册 WebSocket handler；gorilla/websocket 是事实标准且与 Echo 配合良好。
- **替代考虑**：
  - `gin` —— 生态等价，但 Echo 对标准 `http.Handler` 兼容更直接，便于将来外接 SDK。
  - Fiber —— 性能更高但 API 风格与 `net/http` 差距大，不利于与 `embed.FS` 中间件混用。
  - 标准库 + 自行接线 —— 不需要，省去框架收益不明显。

### D2. `log/slog` + 自定义 MultiHandler 实现日志双输出

- **决定**：实现一个 `internal/logger.MultiHandler`（实现 `slog.Handler` 接口），把日志 fan-out 到两个目标：(a) 文件异步 writer（缓冲 1s 刷盘）；(b) 全局 hub，hub 上挂载 N 个订阅者，每个 WS 连接为一个订阅者。
- **理由**：`log/slog` 是 Go 1.21+ 标准库，避免再引入 `logrus` / `zap`。MultiHandler 模式可在不动业务代码的情况下插入第三个输出（如 syslog，将来可选）。
- **替代考虑**：
  - `uber-go/zap` —— 性能更高，但与本 change 的 GB28181 wire-bytes 场景无关；后续 change 可在性能成为瓶颈时再评估。
  - 双写两套 logger（每路一份 slog.Logger）—— 实现复杂且字段脱敏难以统一。

### D3. 字段脱敏在 MultiHandler 层集中实现

- **决定**：所有结构化字段在 MultiHandler `Handle` 时按"属性名黑名单 + 类型断言为字符串/[]byte 时替换"统一脱敏；黑名单：`password`、`secret`、`private_key`、`authorization`（预留）。
- **理由**：后续 SIP Digest、GB35114 SM2、VKEK 都会涉及密钥字段；越早把脱敏放在唯一出口，越能避免后续 change 漏掉。
- **替代考虑**：
  - 在调用 `logger.Info` 前脱敏 —— 不可强制，业务层容易漏。

### D4. Viper + GB28181_SIMULATOR_ 环境变量前缀

- **决定**：`internal/config` 用 `viper.New()` + `viper.SetEnvPrefix("GB28181_SIMULATOR")` + `viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))`；viper 默认值/反序列化目标为顶层结构体 `Config{HTTP, Log, Storage}`。
- **理由**：环境变量前缀 + 点号转下划线覆盖了"GB28181_SIMULATOR_HTTP_ADDR"场景，且让后续 change 直接给 Config 加字段就能自动暴露。
- **替代考虑**：
  - 纯 `os.Getenv` —— 缺少 YAML 与默认值绑定。
  - `kelseyhightower/envconfig` —— 标签驱动但与 YAML 解耦；不适合混合场景。

### D5. SQLite 引导：仅"建表 SQL 列表 + 单实例 Exec"，业务表空集

- **决定**：`internal/storage.Bootstrap(path)` 负责打开 DB、设置 PRAGMA journal_mode=WAL / foreign_keys=ON、执行 `schema.sql` 中的若干 `CREATE TABLE IF NOT EXISTS`，本 change 不创建任何业务表；预留一行注释说明"GB28181 业务表在 Change 5+ 增量加入"。
- **理由**：把"schema 引导"与"业务表"分层，让后续 change 通过追加 SQL 文件而非修改 `storage.go` 完成演进。
- **替代考虑**：
  - 用 `ent` / `gorm` ORM —— 后续业务表会显著增长，但本 change 引入 ORM 过早，且与"零业务表"的设计目标不符。
  - 把 schema 直接 hardcode 在 storage.go 里 —— 后续变更会反复改这个文件，不利于 review。

### D6. 前端：Vue3 + Element Plus + Vite，单文件 + embed.FS

- **决定**：`web/` 目录按 Vite 标准结构（`web/src/main.ts`、`web/src/App.vue`、`web/index.html`）；`vite build` 产出到 `web/dist/`；Go 端通过 `//go:embed all:dist` 嵌入到一个 `internal/webui/dist.FS`。路由 `/` 直接返回 `index.html` 字节流（带 fallback 到 `index.html` 以支持前端 hash 路由）。
- **理由**：单页应用 + hash 路由，避免任何 SSR 复杂度。`embed.FS` 是 Go 1.16+ 标准方案，无新依赖。
- **替代考虑**：
  - SSR / Next.js 风格 —— 与"单二进制分发"目标矛盾。
  - 不用 Element Plus —— 表单再升级时减少样式工作量。

### D7. CI 选 `golangci-lint` + GitHub Actions 原生 matrix

- **决定**：`.github/workflows/ci.yml` 三个 job：lint（golangci-lint）、test（`go test ./...`，仅 Linux）、build（5 平台 matrix）。`.github/workflows/release.yml` 仅在 `v*` tag 上触发，使用 `softprops/action-gh-release` 发布产物与 sha256。
- **理由**：lint 与 test 分离可避免 cache 重建；build matrix 与 release 分离可避免 push 上就发布。
- **替代考虑**：
  - 全部塞进一个 job —— 在大矩阵下吞吐差。

### D8. 配置与日志目录走 `os.UserConfigDir` + XDG 风格回退

- **决定**：`internal/config` 暴露 `DefaultStateDir()` / `DefaultConfigDir()`，按 `runtime.GOOS == "windows"` 走 `%AppData%\gb28181-simulator`，否则先看 `XDG_STATE_HOME` / `XDG_CONFIG_HOME`，未设置则 `$HOME/.local/state` 与 `$HOME/.config`。
- **理由**：与 spec 中"无 XDG 环境变量时回退默认目录"的场景一致；本 change 不强制创建目录，由 storage bootstrap 与 logger 初始化时按需创建。
- **替代考虑**：
  - 硬编码当前工作目录 —— 与"安装到不同机器路径"的预期不符。
  - 完全交给 viper 自带路径 —— viper 不感知 XDG，需要自实现一层。

## Risks / Trade-offs

- **[Risk]** gorilla/websocket 与 Echo 中间件共用 Recover 时 WS 异常 panic 会被吃掉 → 在 MultiHandler 与 WS read loop 中各自加 `defer` 保护，避免 Recover 吞错。
- **[Risk]** `embed.FS` 在 Windows 上大小写不敏感但路径严格区分 → 路由 fallback 统一使用 `filepath.ToSlash`。
- **[Risk]** 多 WS 连接同时订阅日志时，订阅者慢消费会阻塞全局 hub → 用 channel 缓冲（256）+ 慢订阅者被自动跳过（`select default`），避免单点拖累整个进程。
- **[Risk]** slog 的 trace 级别在 Go 1.21+ 尚未官方化（仅作为别名 `LevelDebug = -4` 保留）→ 本 change 不暴露自定义低于 Debug 的级别，trace 通过 "level=trace + 字段名" 的方式预留。
- **[Trade-off]** 本 change 不引入数据库迁移工具（如 `golang-migrate`），仅使用幂等 SQL → 失去版本化迁移能力，Change 5+ 引入业务表时若需回滚需手写 SQL 兜底；建议 Change 5 决定是否引入迁移工具。

## Migration Plan

- 不需要数据库迁移（首次安装即创建空库）。
- 不需要向后兼容（旧版本不存在）。
- 升级路径：本 change 完成后，未来任何 change 仅追加代码与 SQL，未来 archive 由 OpenSpec 自身完成。

## Open Questions

- （无）所有可推迟的设计决策都已落入"留给 Change X"的范畴，不影响本 change 的实施。
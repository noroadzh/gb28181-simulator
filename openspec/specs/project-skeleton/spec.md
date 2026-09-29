# project-skeleton 规范

## Purpose

`project-skeleton` 是 `gb28181-simulator` 的工程基线，为后续 14 个 GB28181 协议 change 提供模块布局、日志、配置、HTTP/WS 骨架、前端嵌入与跨平台 CI 等公共基础设施。本能力不定义任何协议行为，只定义"启动一个进程、暴露 HTTP 健康与版本接口、向前端实时推送日志"这一最小可运行边界。

## Requirements

### Requirement: 单二进制分发

系统 MUST 为每个目标平台产出一个可执行单文件，其中包含编译好的前端资源，运行时无需任何外部文件依赖即可提供前端服务。

#### 场景：启动后单二进制包含前端资源
- **WHEN** 用户在 Linux amd64 上运行 `gb28181-simulator`（不附带 `web/dist/` 目录）
- **THEN** 进程成功启动，访问 `http://127.0.0.1:<port>/` 返回内嵌的 Dashboard 页面（HTTP 200），且同目录下不存在任何必需的外部静态文件

#### 场景：跨平台二进制可执行性
- **WHEN** 用户在 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64` 中任一平台执行对应构建产物
- **THEN** 进程启动完成（未退出/未 panic），成功监听配置端口并响应 HTTP 请求

### Requirement: 分层模块布局

仓库 MUST 采用 `cmd/`、`internal/`、`web/` 三级顶层布局，后续 GB28181 协议包统一放在 `internal/` 下，前端资源放在 `web/` 下。

#### 场景：包布局可被后续 change 复用
- **WHEN** 开发者首次浏览仓库
- **THEN** 存在 `cmd/gb28181-simulator/main.go` 作为唯一入口，`internal/` 下至少包含 `logger`、`config`、`storage`、`api` 四个非空包，`web/` 下包含 Vue3 前端源码

#### 场景：内部包不可被外部导入误用
- **WHEN** 仓库外的 Go 模块尝试 `import "<module-path>/internal/logger"`
- **THEN** Go 工具链按 `internal/` 规则拒绝该导入

### Requirement: 分级结构化日志与流式输出

系统 MUST 使用分级结构化日志，包含五个严重等级（trace、debug、info、warn、error），同时写入文件 sink 与 WebSocket sink，并对配置的敏感字段做脱敏处理。

#### 场景：五个等级可配置且可过滤
- **WHEN** 配置中设置日志级别为 `warn`
- **THEN** 文件与 WebSocket 输出均不含 trace/debug/info 记录，但保留 warn 与 error 记录

#### 场景：文件与 WebSocket 双输出一致
- **WHEN** 系统产生一条 info 级日志
- **THEN** 该记录同时出现在日志文件与 `/api/logs/stream` WebSocket 消息中，且字段（时间戳、等级、消息、结构化属性）一致

#### 场景：敏感字段脱敏
- **WHEN** 任意 log 记录中包含名为 `password`、`secret` 或 `private_key` 的属性字段
- **THEN** 两个输出通道中该字段的值均为 `***REDACTED***`

#### 场景：无 XDG 环境变量时回退默认目录
- **WHEN** 在未设置 `XDG_STATE_HOME` 的 Linux/macOS 环境启动
- **THEN** 日志文件写入 `$HOME/.local/state/gb28181-simulator/logs/`；在 Windows 下写入 `%AppData%\gb28181-simulator\logs\`

### Requirement: 配置契约

系统 MUST 读取 YAML 配置文件，支持以 `GB28181_SIMULATOR_` 为前缀的环境变量覆盖，并通过 HTTP 端点暴露版本与构建元数据。

#### 场景：配置文件与端口绑定
- **WHEN** 启动时提供 `configs/config.yaml`，其中 `http.addr = "127.0.0.1:18080"`
- **THEN** HTTP 服务监听 `127.0.0.1:18080`，其他地址不可连接

#### 场景：环境变量覆盖端口
- **WHEN** 同时存在配置文件 `http.addr = "127.0.0.1:18080"` 与环境变量 `GB28181_SIMULATOR_HTTP_ADDR=127.0.0.1:18081`
- **THEN** HTTP 服务监听 `127.0.0.1:18081`，环境变量优先

#### 场景：版本元数据可查询
- **WHEN** 调用 `GET /api/version`
- **THEN** 返回 HTTP 200，JSON 包含 `version`（如 `0.1.0-dev`）、`commit`、`go_version`、`platform` 四个字段

### Requirement: Web 健康检查与日志流接口

系统 MUST 暴露一个 HTTP 健康检查端点和一个 WebSocket 日志流端点；内嵌的 Dashboard 页面 MUST 连接到日志流并实时渲染日志记录。

#### 场景：健康检查反映进程存活
- **WHEN** 进程运行中并调用 `GET /api/health`
- **THEN** 返回 HTTP 200，JSON 为 `{"status":"ok"}`

#### 场景：Dashboard 实时接收日志
- **WHEN** 浏览器打开 `/`，同时系统产生一条 info 级日志
- **THEN** 页面通过 `/api/logs/stream` 收到该条日志并在不刷新页面的情况下展示出来，展示内容包含等级与消息文本

### Requirement: SQLite schema 初始化

系统 MUST 在首次启动时创建本地 SQLite 数据库文件并执行幂等的 schema 初始化，本 change 中不持久化任何 GB28181 领域数据。

#### 场景：首次启动创建数据库
- **WHEN** 在干净的安装目录首次启动，且配置 `storage.path` 指向一个不存在的文件
- **THEN** 程序自动创建父目录与该 SQLite 文件，之后再次启动不再重建，也不报错（幂等）

#### 场景：数据库文件遵循 XDG / AppData 路径
- **WHEN** 未显式配置 `storage.path` 且在 Linux/macOS 启动
- **THEN** 数据库位于 `$XDG_STATE_HOME/gb28181-simulator/`（未设置时回退 `$HOME/.local/state/gb28181-simulator/`）；在 Windows 下位于 `%AppData%\gb28181-simulator\`

### Requirement: 跨平台 CI

仓库 MUST 提供 GitHub Actions workflow，对每个受支持的目标执行 lint、单元测试和构建，并提供一个由 tag 触发的额外发布 workflow。

#### 场景：Push 触发 CI 全矩阵
- **WHEN** 有任意 push 到 `main` 分支或打开 Pull Request
- **THEN** CI 依次执行 `golangci-lint`、`go test ./...`，并对 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64` 逐目标执行 `go build`

#### 场景：Tag 触发 release 产物
- **WHEN** 推送形如 `v0.1.0` 的 tag
- **THEN** 产出对应 5 个平台的压缩二进制，并为每个产物生成 `.sha256` 校验文件

#### 场景：CI 不引入 CGO 依赖
- **WHEN** CI 在任一目标平台执行 `CGO_ENABLED=0 go build ./...`
- **THEN** 构建成功（提示当前依赖集不含 CGO）

### Requirement: IDE 工作区诊断保持零警告

当仓库被 IDE 打开时，Problems 面板对仓库内任何 `.go` 文件 MUST 报告零诊断；对仓库外的临时文件（`/tmp/*.go`、`~/cate/*.go` 等用户独立打开的文件） MUST 不被纳入仓库诊断列表。`.vscodeignore` 与 `.gitignore` MUST 屏蔽这些路径；`go.mod` 中所有 import 的第三方依赖 MUST 为直接依赖或由直接依赖显式传递；`go vet ./...`、`go build ./...`、`gopls check ./...` 在仓库根目录的输出 MUST 为空。

#### 场景：在 CodeBuddy 中干净打开仓库
- **WHEN** 用户在 IDE 中打开仓库根目录（不打开仓库外临时文件）
- **THEN** Problems 面板显示的诊断条目数为 0
- **AND** 终端中 `go vet ./...` 与 `go build ./...` 输出均为空

#### 场景：打开仓库时存在 /tmp 临时文件
- **WHEN** 用户在 IDE 中除仓库外另外打开了 `/tmp/brace_scan.go` 等临时 Go 文件
- **THEN** 这些文件不进入仓库的诊断列表，Problems 面板依然为空

#### 场景：go.mod 仅含直接依赖
- **WHEN** 运行 `go mod tidy`
- **THEN** `go.mod` 中 `require` 段所有非 `// indirect` 标注的模块都是仓库代码直接 import 的依赖
- **AND** `go list -m -mod=mod -f '{{if not .Indirect}}{{.Path}}{{end}}'` 输出覆盖所有仓库 `import` 语句

### Requirement: 一键冒烟基线

仓库 MUST 提供一个 `make smoke` 目标（或等价的 `scripts/smoke.sh`），能在 Linux/macOS/Windows 三类开发机上完整执行：① Go 单元测试（`go test -race -count=1 ./...`）；② Go 静态检查（`go vet ./...`）；③ Go 单平台构建（`go build ./...`）；④ 跨平台编译（至少覆盖 linux/amd64、linux/arm64、darwin/amd64、windows/amd64 四种组合）；⑤ 前端构建（`cd web && npm ci && npm run build`，产物落 `web/dist/`）；⑥ 端到端冒烟（启动构建好的二进制，探测 `/healthz`、日志 WS、`/api/faults` 三个端点，返回 200 后退出）。每一步的退出码、耗时、产物路径 MUST 汇总到 `docs/smoke-results.json`。

#### 场景：在 Linux 上执行 make smoke
- **WHEN** 在 Linux amd64 开发机上执行 `make smoke`
- **THEN** 全部 6 个子步骤依次成功（退出码 0）
- **AND** `docs/smoke-results.json` 存在，包含 6 个 step 对象，每个对象有 `name`、`status`、`duration_ms`、`artifacts` 字段
- **AND** 整个流程总耗时 ≤ 8 分钟（CI 中可放宽）

#### 场景：跨平台编译产出四个目标产物
- **WHEN** `make smoke` 的跨平台编译步骤执行
- **THEN** `build/` 目录下出现 `gb28181-simulator-linux-amd64`、`gb28181-simulator-linux-arm64`、`gb28181-simulator-darwin-amd64`、`gb28181-simulator-windows-amd64.exe` 四个产物
- **AND** 每个产物的 `file` 命令报告其声明的目标平台

#### 场景：e2e 健康探针成功
- **WHEN** e2e 步骤启动刚构建的二进制并等待其健康
- **THEN** 探测 `GET /healthz` 返回 200 且响应体包含 `{"status":"ok"}`
- **AND** 探测 `GET /api/faults` 返回 200
- **AND** WebSocket 端点 `/ws/logs` 在 3 秒内完成握手并收到至少一条日志消息

#### 场景：smoke 失败留下诊断轨迹
- **WHEN** 任一子步骤退出码非零
- **THEN** `make smoke` 整体退出码为该子步骤的退出码
- **AND** `docs/smoke-results.json` 中对应 step 的 `status` 为 `"failed"`，`error` 字段包含该子步骤的最后 200 行输出

### Requirement: 冒烟测试文档已发布

仓库 MUST 在 `docs/smoke-test.md` 提供 Markdown 冒烟测试文档，覆盖：① 测试矩阵（19 个 capability 与冒烟用例的对应关系）；② 执行步骤（本地 `make smoke` 与 CI 触发）；③ 结果解读（`docs/smoke-results.json` schema 详解）；④ 失败排查（按诊断来源分类的修复指引）；⑤ 附录（历史报告归档位置）。

#### 场景：新贡献者阅读 smoke-test.md
- **WHEN** 新贡献者按 README 指引打开 `docs/smoke-test.md`
- **THEN** 文档首段说明 smoke 与 unit test 的边界（smoke 验证端到端可用，unit test 验证字节级正确）
- **AND** 文档的"测试矩阵"章节列出全部 19 个 capability 及其冒烟探针

#### 场景：CI 运行 smoke 并上传制品
- **WHEN** GitHub Actions 的 `smoke` job 完成
- **THEN** `docs/smoke-results.json` 被作为工件上传，可在 CI run 详情页下载
- **AND** 文档"CI 集成"章节给出该工件在 UI 中的下载路径

### Requirement: 双形态部署文档已发布

仓库 MUST 提供两种部署形态的 Markdown 文档：`docs/deploy-linux.md`（Linux 单机 systemd unit 部署）与 `docs/deploy-docker-compose.md`（Docker Compose 多节点部署）。两文档 MUST 包含：① 前置依赖（OS 包、Go 版本、Docker/Compose 版本）；② 安装步骤（含本地源码构建、用户与目录、systemd unit 或 Compose 文件），**不依赖外部 GitHub Release**；③ 配置文件位置与最小可用样例（含 `allow_no_auth: true` 的无密码模式示例）与密码管理最佳实践；④ 启动/停止/查看日志；⑤ 升级与回滚步骤；⑥ 常见故障排查（含"密码为空导致注册失败"）；⑦ 安装脚本（`scripts/install-linux.sh`）从本地 `configs/config.example.yaml` 复制。

#### 场景：运维在 Linux 上从源码部署单节点
- **WHEN** 运维按 `docs/deploy-linux.md` 在 CentOS 8 / Ubuntu 22.04 / Debian 12 任一 Linux 上执行源码构建与部署
- **THEN** 按文档步骤执行后 `systemctl status gb28181-simulator` 显示 `active (running)`
- **AND** `curl http://localhost:8080/healthz` 返回 `{"status":"ok"}`
- **AND** 文档**不包含**指向 `github.com/your-org/gb28181-simulator/releases` 的下载链接

#### 场景：运维通过 Compose 从源码部署多节点拓扑
- **WHEN** 运维按 `docs/deploy-docker-compose.md` 在装有 Docker Engine 24+ 与 Compose v2 的主机上执行 `docker compose build && docker compose up -d`
- **THEN** 所有服务进入 `healthy` 状态
- **AND** 镜像构建来自本地 Dockerfile（`docker build -t gb28181-simulator:local .`），不依赖 GHCR pull

#### 场景：运维部署无密码 device 节点
- **WHEN** 运维在测试/内网环境部署 device 节点
- **THEN** `docs/deploy-linux.md` 提供 `allow_no_auth: true` 配置样例
- **AND** `docs/deploy-docker-compose.md` 提供 `allow_no_auth: true` 的 device Compose service

#### 场景：运维按文档回滚升级
- **WHEN** 运维执行文档中描述的升级步骤（systemd 二进制替换或 Compose `up --build`）
- **THEN** 出现不兼容升级时能按"回滚步骤"小节回退到上一版本

## 验收钩子（供 Change 14 复用的接口契约）

以下验收观察点为后续 `web-management-ui` 预留，本 change 只保证接口存在且不被破坏：

- `GET /api/health` 返回 `{"status":"ok"}`
- `GET /api/version` 返回 `version` / `commit` / `go_version` / `platform`
- `WS /api/logs/stream` 持续推送各级别日志记录
- `GET /` 返回内嵌 Dashboard 页面，可与日志流互通
- `storage.path` 与 `http.addr` 均可通过配置文件与环境变量控制

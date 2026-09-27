# project-skeleton 规范

## Purpose

`project-skeleton` 是 `gb28181-simulator` 的工程基线，为后续 14 个 GB28181 协议 change 提供模块布局、日志、配置、HTTP/WS 骨架、前端嵌入与跨平台 CI 等公共基础设施。本能力不定义任何协议行为，只定义"启动一个进程、暴露 HTTP 健康与版本接口、向前端实时推送日志"这一最小可运行边界。

## Requirements

### 需求：单二进制分发

系统 MUST 为每个目标平台产出一个可执行单文件，其中包含编译好的前端资源，运行时无需任何外部文件依赖即可提供前端服务。

#### 场景：启动后单二进制包含前端资源
- **WHEN** 用户在 Linux amd64 上运行 `gb28181-simulator`（不附带 `web/dist/` 目录）
- **THEN** 进程成功启动，访问 `http://127.0.0.1:<port>/` 返回内嵌的 Dashboard 页面（HTTP 200），且同目录下不存在任何必需的外部静态文件

#### 场景：跨平台二进制可执行性
- **WHEN** 用户在 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64` 中任一平台执行对应构建产物
- **THEN** 进程启动完成（未退出/未 panic），成功监听配置端口并响应 HTTP 请求

### 需求：分层模块布局

仓库 MUST 采用 `cmd/`、`internal/`、`web/` 三级顶层布局，后续 GB28181 协议包统一放在 `internal/` 下，前端资源放在 `web/` 下。

#### 场景：包布局可被后续 change 复用
- **WHEN** 开发者首次浏览仓库
- **THEN** 存在 `cmd/gb28181-simulator/main.go` 作为唯一入口，`internal/` 下至少包含 `logger`、`config`、`storage`、`api` 四个非空包，`web/` 下包含 Vue3 前端源码

#### 场景：内部包不可被外部导入误用
- **WHEN** 仓库外的 Go 模块尝试 `import "<module-path>/internal/logger"`
- **THEN** Go 工具链按 `internal/` 规则拒绝该导入

### 需求：分级结构化日志与流式输出

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

### 需求：配置契约

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

### 需求：Web 健康检查与日志流接口

系统 MUST 暴露一个 HTTP 健康检查端点和一个 WebSocket 日志流端点；内嵌的 Dashboard 页面 MUST 连接到日志流并实时渲染日志记录。

#### 场景：健康检查反映进程存活
- **WHEN** 进程运行中并调用 `GET /api/health`
- **THEN** 返回 HTTP 200，JSON 为 `{"status":"ok"}`

#### 场景：Dashboard 实时接收日志
- **WHEN** 浏览器打开 `/`，同时系统产生一条 info 级日志
- **THEN** 页面通过 `/api/logs/stream` 收到该条日志并在不刷新页面的情况下展示出来，展示内容包含等级与消息文本

### 需求：SQLite schema 初始化

系统 MUST 在首次启动时创建本地 SQLite 数据库文件并执行幂等的 schema 初始化，本 change 中不持久化任何 GB28181 领域数据。

#### 场景：首次启动创建数据库
- **WHEN** 在干净的安装目录首次启动，且配置 `storage.path` 指向一个不存在的文件
- **THEN** 程序自动创建父目录与该 SQLite 文件，之后再次启动不再重建，也不报错（幂等）

#### 场景：数据库文件遵循 XDG / AppData 路径
- **WHEN** 未显式配置 `storage.path` 且在 Linux/macOS 启动
- **THEN** 数据库位于 `$XDG_STATE_HOME/gb28181-simulator/`（未设置时回退 `$HOME/.local/state/gb28181-simulator/`）；在 Windows 下位于 `%AppData%\gb28181-simulator\`

### 需求：跨平台 CI

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

## 验收钩子（供 Change 14 复用的接口契约）

以下验收观察点为后续 `web-management-ui` 预留，本 change 只保证接口存在且不被破坏：

- `GET /api/health` 返回 `{"status":"ok"}`
- `GET /api/version` 返回 `version` / `commit` / `go_version` / `platform`
- `WS /api/logs/stream` 持续推送各级别日志记录
- `GET /` 返回内嵌 Dashboard 页面，可与日志流互通
- `storage.path` 与 `http.addr` 均可通过配置文件与环境变量控制
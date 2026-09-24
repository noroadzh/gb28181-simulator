# Spec

## Purpose

`project-skeleton` 是 `gb28181-simulator` 的工程基线，为后续 14 个 GB28181 协议 change 提供模块布局、日志、配置、HTTP/WS 骨架、前端嵌入与跨平台 CI 等公共基础设施。本能力不定义任何协议行为，只定义"启动一个进程、暴露 HTTP 健康与版本接口、向前端实时推送日志"这一最小可运行边界。

## Requirements

### Requirement: Single-binary distribution

The system SHALL produce a single executable per target platform that contains the compiled web assets and serves them without any external file dependency at runtime.

#### Scenario: 启动后单二进制包含前端资源
- **WHEN** 用户在 Linux amd64 上运行 `gb28181-simulator`（不附带 `web/dist/` 目录）
- **THEN** 进程成功启动，访问 `http://127.0.0.1:<port>/` 返回内嵌的 Dashboard 页面（HTTP 200），且同目录下不存在任何必需的外部静态文件

#### Scenario: 跨平台二进制可执行性
- **WHEN** 用户在 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64` 中任一平台执行对应构建产物
- **THEN** 进程启动完成（未退出/未 panic），成功监听配置端口并响应 HTTP 请求

### Requirement: Layered module layout

The repository SHALL be organized into a `cmd/`, `internal/`, and `web/` top-level layout, where future GB28181 protocol packages live under `internal/` and the web assets under `web/`.

#### Scenario: 包布局可被后续 change 复用
- **WHEN** 开发者首次浏览仓库
- **THEN** 存在 `cmd/gb28181-simulator/main.go` 作为唯一入口，`internal/` 下至少包含 `logger`、`config`、`storage`、`api` 四个非空包，`web/` 下包含 Vue3 前端源码

#### Scenario: 内部包不可被外部导入误用
- **WHEN** 仓库外的 Go 模块尝试 `import "<module-path>/internal/logger"`
- **THEN** Go 工具链按 `internal/` 规则拒绝该导入

### Requirement: Leveled structured logging with streaming

The system SHALL use leveled structured logging with five severities (trace, debug, info, warn, error), write to both a file sink and a WebSocket sink, and redact configured sensitive fields.

#### Scenario: 五个等级可配置且可过滤
- **WHEN** 配置中设置日志级别为 `warn`
- **THEN** 文件与 WebSocket 输出均不含 trace/debug/info 记录，但保留 warn 与 error 记录

#### Scenario: 文件与 WebSocket 双输出一致
- **WHEN** 系统产生一条 info 级日志
- **THEN** 该记录同时出现在日志文件与 `/api/logs/stream` WebSocket 消息中，且字段（时间戳、等级、消息、结构化属性）一致

#### Scenario: 敏感字段脱敏
- **WHEN** 任意 log 记录中包含名为 `password`、`secret` 或 `private_key` 的属性字段
- **THEN** 两个输出通道中该字段的值均为 `***REDACTED***`

#### Scenario: 无 XDG 环境变量时回退默认目录
- **WHEN** 在未设置 `XDG_STATE_HOME` 的 Linux/macOS 环境启动
- **THEN** 日志文件写入 `$HOME/.local/state/gb28181-simulator/logs/`；在 Windows 下写入 `%AppData%\gb28181-simulator\logs\`

### Requirement: Configuration contract

The system SHALL read a YAML configuration file, support environment variable overrides from a `GB28181_SIMULATOR_` prefix, and expose version/build metadata via an HTTP endpoint.

#### Scenario: 配置文件与端口绑定
- **WHEN** 启动时提供 `configs/config.yaml`，其中 `http.addr = "127.0.0.1:18080"`
- **THEN** HTTP 服务监听 `127.0.0.1:18080`，其他地址不可连接

#### Scenario: 环境变量覆盖端口
- **WHEN** 同时存在配置文件 `http.addr = "127.0.0.1:18080"` 与环境变量 `GB28181_SIMULATOR_HTTP_ADDR=127.0.0.1:18081`
- **THEN** HTTP 服务监听 `127.0.0.1:18081`，环境变量优先

#### Scenario: 版本元数据可查询
- **WHEN** 调用 `GET /api/version`
- **THEN** 返回 HTTP 200，JSON 包含 `version`（如 `0.1.0-dev`）、`commit`、`go_version`、`platform` 四个字段

### Requirement: Web health and log stream interface

The system SHALL expose an HTTP health endpoint and a WebSocket log stream endpoint; the embedded Dashboard page SHALL connect to the log stream and render records in real time.

#### Scenario: 健康检查反映进程存活
- **WHEN** 进程运行中并调用 `GET /api/health`
- **THEN** 返回 HTTP 200，JSON 为 `{"status":"ok"}`

#### Scenario: Dashboard 实时接收日志
- **WHEN** 浏览器打开 `/`，同时系统产生一条 info 级日志
- **THEN** 页面通过 `/api/logs/stream` 收到该条日志并在不刷新页面的情况下展示出来，展示内容包含等级与消息文本

### Requirement: SQLite schema bootstrap

The system SHALL create a local SQLite database file on first start and apply an idempotent schema bootstrap without persisting any GB28181 domain data in this change.

#### Scenario: 首次启动创建数据库
- **WHEN** 在干净的安装目录首次启动，且配置 `storage.path` 指向一个不存在的文件
- **THEN** 程序自动创建父目录与该 SQLite 文件，之后再次启动不再重建，也不报错（幂等）

#### Scenario: 数据库文件遵循 XDG / AppData 路径
- **WHEN** 未显式配置 `storage.path` 且在 Linux/macOS 启动
- **THEN** 数据库位于 `$XDG_STATE_HOME/gb28181-simulator/`（未设置时回退 `$HOME/.local/state/gb28181-simulator/`）；在 Windows 下位于 `%AppData%\gb28181-simulator\`

### Requirement: Cross-platform CI

The repository SHALL provide GitHub Actions workflows that lint, unit test, and build every supported target, and an additional release workflow triggered by tags.

#### Scenario: Push 触发 CI 全矩阵
- **WHEN** 有任意 push 到 `main` 分支或打开 Pull Request
- **THEN** CI 依次执行 `golangci-lint`、`go test ./...`，并对 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64` 逐目标执行 `go build`

#### Scenario: Tag 触发 release 产物
- **WHEN** 推送形如 `v0.1.0` 的 tag
- **THEN** 产出对应 5 个平台的压缩二进制，并为每个产物生成 `.sha256` 校验文件

#### Scenario: CI 不引入 CGO 依赖
- **WHEN** CI 在任一目标平台执行 `CGO_ENABLED=0 go build ./...`
- **THEN** 构建成功（提示当前依赖集不含 CGO）

## Acceptance Hooks（供 Change 14 复用的接口契约）

以下验收观察点为后续 `web-management-ui` 预留，本 change 只保证接口存在且不被破坏：

- `GET /api/health` 返回 `{"status":"ok"}`
- `GET /api/version` 返回 `version` / `commit` / `go_version` / `platform`
- `WS /api/logs/stream` 持续推送各级别日志记录
- `GET /` 返回内嵌 Dashboard 页面，可与日志流互通
- `storage.path` 与 `http.addr` 均可通过配置文件与环境变量控制
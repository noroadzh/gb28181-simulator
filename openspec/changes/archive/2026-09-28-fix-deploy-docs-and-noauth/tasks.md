# Tasks

## 1. 代码层 — config 模型加 allow_no_auth

- [x] 1.1 `internal/platform/config/config.go` 中 `NodeRegistrationConfig` 增加 `AllowNoAuth bool` 字段（mapstructure `allow_no_auth`）。验证：`go build ./...` 通过。
- [x] 1.2 `NodePlatformConfig` 增加 `AllowNoAuth bool` 字段。验证：`go build ./...` 通过。
- [x] 1.3 `RegistrationParams` 增加 `AllowNoAuth bool` 字段并传入 `NewRegistration`。验证：`go vet ./...` 零输出。
- [x] 1.4 `validateNodePlatform` 调整：当 `p.AllowNoAuth == true` 时，跳过 password 必填检查但仍校验 username 非空。验证：新增单元测试 `TestLoad_PlatformNoAuthEmptyPasswordOK`。

## 2. 代码层 — Registration 模型

- [x] 2.1 `internal/domain/model/registration.go` 中 `NewRegistration` 当 `p.Password == "" && p.AllowNoAuth == true` 时跳过 password 必填错误。验证：新增 `TestRegistration_NoAuthEmptyPasswordOK`。
- [x] 2.2 现有 `TestRegistration_*` 全部通过（无回归）。

## 3. 代码层 — Authorizer

- [x] 3.1 `internal/adapter/auth/authorizer.go` 中 `Check` 方法：若 cred 为空 response 且 allow_no_auth=true，返回 `Authorized`。验证：新增 `TestAuthorizer_NoAuthEmptyResponse`。
- [x] 3.2 现有 `TestAuthorizer_*` 全部通过。

## 4. 配置示例 — config.example.yaml

- [x] 4.1 `configs/config.example.yaml` 增加"无密码模式"注释示例。验证：grep 包含 `allow_no_auth` 字样。

## 5. 部署文档 — deploy-linux.md

- [x] 5.1 移除"从 CI 工件下载"小节，替换为"从源码本地构建"。验证：grep 不含 `actions.githubusercontent.com`。
- [x] 5.2 增加 `allow_no_auth: true` 配置样例。验证：grep 包含 `allow_no_auth`。
- [x] 5.3 故障排查增加"密码为空导致注册失败"条目。验证：grep 包含 `password 为空`。

## 6. 部署文档 — deploy-docker-compose.md

- [x] 6.1 移除 GHCR 拉取章节，改为 `docker build -t gb28181-simulator:local .`。验证：grep 不含 `ghcr.io`。
- [x] 6.2 device service 配置示例增加 `allow_no_auth: true`。验证：grep 包含 `allow_no_auth`。
- [x] 6.3 platform service 配置示例增加 `platform.allow_no_auth: true`。验证：grep 包含 `allow_no_auth`。

## 7. 安装脚本 — scripts/install-linux.sh

- [x] 7.1 创建 `scripts/install-linux.sh`：从本地 `configs/config.example.yaml` 复制到 `/etc/gb28181-simulator/config.yaml`，而不是远程 URL。验证：bash -n 通过且不含 `raw.githubusercontent.com`。
- [x] 7.2 脚本支持 `--allow-no-auth` 参数，生成无密码模式的默认 config。验证：带参数运行后生成的 config 包含 `allow_no_auth: true`。

## 8. 最终验证

- [x] 8.1 `make smoke` 全量通过。验证：`docs/smoke-results.json` 中所有 step `status: "passed"`。
- [x] 8.2 `go test -race ./...` 通过。验证：退出码 0。
- [x] 8.3 `openspec validate fix-deploy-docs-and-noauth --strict` 通过。验证：零错误。
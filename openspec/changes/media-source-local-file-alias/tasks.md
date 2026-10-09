# Tasks: media-source-local-file-alias

## 1. 代码修复

- [x] 1.1 在 `internal/domain/model/media.go` 的 `MediaConfig.Normalize()` 中将 `Kind == "local_file"` 替换为 `SourceKindFile`
- [x] 1.2 更新 `Normalize()` 注释，说明 local_file 别名行为

## 2. 测试

- [x] 2.1 在 `internal/domain/model/media_test.go` 中新增 `TestMediaConfigNormalizeLocalFileAlias` 覆盖归一化逻辑
- [x] 2.2 运行 `go test ./internal/domain/model/` 确认无回归

## 3. 部署

- [x] 3.1 重新构建 linux/amd64 二进制（`bin/gb28181-simulator-linux-amd64`）
- [x] 3.2 上传至服务器 `/slow2/gb28181-simulator/binary/`
- [x] 3.3 `docker compose build` + `docker compose up -d` 部署 gbsim-platform / gbsim-device
- [x] 3.4 `curl /v1/flv/...` 验证返回 200 + `Content-Type: video/x-flv`

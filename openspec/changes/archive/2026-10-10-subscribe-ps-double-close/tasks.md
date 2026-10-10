# Tasks

## 1. 修复 SubscribePS double-close bug

- [ ] 1.1 修改 `internal/app/media_service.go` 中 `SubscribePS` 方法，将 reader goroutine 的 `defer close(psChRaw)` 改为 `defer once.Do(func() { close(psChRaw) })`，与 cleanup 共享同一个 `sync.Once`；并在代码中加注释说明"channel 关闭由 once 串行化，src 仍由 cleanup 关闭"；通过 `go build ./...` 编译通过验证
- [ ] 1.2 验证现有调用方（`internal/app/streaming/adapter.go:50` 与 `streaming/gateway.go:113`）的 cleanup 调用语义不变；通过 `go vet ./...` 与 `go build ./...` 全量编译通过验证

## 2. 编写并发单元测试

- [ ] 2.1 新增 `internal/app/media_service_test.go`，实现 `TestSubscribePSCloseByEOF`（reader 自然 EOF 触发关闭，cleanup 调用无 panic）；通过 `go test -race ./internal/app -run TestSubscribePSCloseByEOF -v` 验证
- [ ] 2.2 在 `media_service_test.go` 增加 `TestSubscribePSCloseByCancel`（ctx 取消同时 cleanup 调用，验证 -race 模式下无 panic）；通过 `go test -race ./internal/app -run TestSubscribePSCloseByCancel -v -count=100` 稳定通过验证
- [ ] 2.3 在 `media_service_test.go` 增加 `TestSubscribePSCloseByCleanup`（caller 主动调用 cleanup，channel 正确关闭，src.Close 被调用一次）；通过 `go test -race ./internal/app -run TestSubscribePSCloseByCleanup -v` 验证

## 3. 全量测试与构建

- [ ] 3.1 运行 `go test -race ./...` 确认无回归；通过所有现有测试通过 + 新增 3 个测试通过验证
- [ ] 3.2 交叉编译 linux/amd64 二进制 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=subscribe-ps-double-close" -o bin/gb28181-simulator-linux-amd64 ./cmd/gb28181-simulator`；通过 `ls -la bin/gb28181-simulator-linux-amd64` 与 `sha256sum` 输出与历史不一致验证

## 4. 部署与验证

- [ ] 4.1 scp 二进制到 `root@10.96.1.125:/slow2/gb28181-simulator/binary/`，在服务器上 `cd /slow2/gb28181-simulator && docker compose build 2>&1 | tail -5 && docker compose up -d`；通过 `docker ps --filter name=gbsim` 显示 `Up` 验证
- [ ] 4.2 服务器上 `curl -s -o /tmp/flv.bin -w "SIZE:%{size_download}" --max-time 3 http://127.0.0.1:18081/v1/flv/34020000001310000001/34020000001310000002`；通过 SIZE > 1024（不止 12 字节头）验证流媒体管道稳定
- [ ] 4.3 `docker logs gbsim-device --tail 50` 不应再出现 `panic: close of closed channel`；通过 grep 无 panic 输出验证

## 5. Git 提交与 openspec 归档

- [ ] 5.1 `git add internal/app/media_service.go internal/app/media_service_test.go openspec/changes/subscribe-ps-double-close/` 后提交；通过 `git log -1 --format='%H %s'` 显示新 commit 验证
- [ ] 5.2 `openspec archive subscribe-ps-double-close --yes`；通过 `ls openspec/changes/archive/2026-10-09-subscribe-ps-double-close/` 验证归档存在 + `grep -n "PS 管道并发安全" openspec/specs/media-sources/spec.md` 验证 ADDED requirement 已合并到 main spec

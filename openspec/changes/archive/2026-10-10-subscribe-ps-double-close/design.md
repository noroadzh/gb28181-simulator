# Design

## Context

`MediaService.SubscribePS`（`internal/app/media_service.go:292-356`）是 HTTP-FLV 流媒体管道的核心入口。它启动一个 reader goroutine 读取 ES 帧并 PS 封包后写入 unbuffered channel `psChRaw`，同时返回一个 `cleanup` 函数给 caller 释放资源。

当前实现存在两个并发的 channel 关闭路径：
- `cleanup` 函数（line 324-329）使用 `sync.Once` 保护 `close(psChRaw)`
- reader goroutine（line 331-353）的 `defer close(psChRaw)`（line 332）绕过 `sync.Once`

当 ctx 取消时两条路径同时执行 → `panic: close of closed channel` → 进程重启 → FLV 流断裂。

## Goals / Non-Goals

**Goals:**
- 将 channel 关闭路径唯一化（共享同一个 `sync.Once`）
- 保持 `MediaService` 公开 API 签名不变
- 编写可复现的并发单元测试

**Non-Goals:**
- 不引入新的并发原语（保留 `sync.Once`）
- 不修改 `streaming/adapter.go` / `streaming/gateway.go` 调用方
- 不修改 `MediaConfig` 序列化或 HTTP API 行为

## Decisions

### Decision 1: 共享 `sync.Once` 关闭 channel

将 reader goroutine 的 `defer close(psChRaw)` 改为 `defer once.Do(func() { close(psChRaw) })`，与 cleanup 共享同一个 `var once sync.Once`。

- **为什么选 Once 而不是 Mutex**：Once 是惰性、单次的关闭语义，天然适合"channel 关闭"这种一次性动作；Mutex 需手动 lock/unlock 容易写错。
- **替代方案**：在 channel 关闭前用 `sync.Mutex` 加锁后判断并关闭——可工作但更繁琐，且会引入死锁风险（cleanup 中关闭 src 时若 src 的 Close 内部反向调用同一 mutex 会死锁）。
- **替代方案**：引入 `atomic.Bool` 标志位——可行但 Go 1.25 社区惯例是用 `sync.Once`。

### Decision 2: 不在 reader goroutine 关闭 src

`src.Close()` 仍由 cleanup 函数（once 保护下）调用，reader goroutine 只负责关闭 channel。原因：
- cleanup 是 caller 显式调用的资源释放入口，符合 RAII 语义
- reader 内部已经因 ctx cancel / EOF 退出，无需重复释放 src
- 避免两个 close 路径都需要协调"谁负责 src，谁负责 channel"的复杂度

### Decision 3: 测试用 stub factory + synthetic reader

不依赖真实 mp4 文件或 ffprobe。新增 `internal/app/media_service_test.go`：
- 用 `model.MediaConfig{Kind: "synthetic"}` 配合一个返回 100 帧后报 EOF 的 stub reader
- 三个测试 case 覆盖：自然 EOF / ctx cancel / cleanup 主动调用
- 启用 `go test -race` 验证无 data race

## Risks / Trade-offs

- [Risk] 现有 `defer src.Close()` 在错误分支（line 316）已存在，读者可能误以为 reader 也应关闭 src → Mitigation: 注释明确写出"reader 退出走 once 关闭 channel，src 由 cleanup 关闭"
- [Risk] `sync.Once` 的 doSlow 在高并发首次竞争时会自旋，理论上引入微秒级开销 → Mitigation: SubscribePS 调用频率低（每通道每播放会话一次），开销可忽略

## Migration Plan

无需数据迁移。部署步骤：
1. 重新构建 linux/amd64 二进制
2. scp 到 10.96.1.125 服务器
3. `docker compose build && up -d`（gbsim-device + gbsim-platform 容器滚动重启）
4. 验证 `curl /v1/flv/.../...` 返回 > 1KB 持续流
5. 浏览器访问 http://10.96.1.125:18081 验证视频播放

回滚策略：保留旧版二进制 `85b447f` 与镜像 `gb28181-simulator:85b447f`，`docker compose down && docker compose up` 即可回滚。

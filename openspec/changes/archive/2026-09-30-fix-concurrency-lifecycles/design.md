# Design

## Context

三处缺陷已在代码中核实（详见 proposal 的 What Changes）：

1. `internal/app/acceptor.go:78` 的 `var inviteTimers sync.Map` 是进程级全局；`:879-889` 的 `watchInviteExpiry` goroutine 体内只有 `<-timer.C` 一条语句，**没有 `ctx.Done()` 分支**。`cancelInviteExpiry` 只调 `t.Stop()`——已 Stop 的 timer 不再向 `timer.C` 投递，该 goroutine 永久阻塞。
2. `internal/app/node_split_transport.go:207-213` 的 `Close()` 顺序为 `cancel() → <-done → close(uas) → close(uac)`。`dispatch` 的三处 send（`:138`、`:162` 闭包内、`:186`）都带 `default` 分支，因此正常情况下不会阻塞；但 `transactionHandler.fn` 由**外部 goroutine** 通过 `RegisterHandler` 注入并在 `dispatch` 之外被调用，Close 之后该 handler 仍持有已关闭的 channel 引用。
3. `internal/adapter/media/hls_source.go:109-114` 的 `Close()` 只置 `closed` 标志。`stream` 的循环（`:74-105`）仅在**每个 segment 开头**检查该标志（`:81-86`）与 `ctx.Done()`（`:75-79`）。若此刻正在 `h.client.Do(req)`（`:92`，最长 10s 超时）或 `io.ReadAll(resp.Body)`（`:96`）中阻塞，`w.Write` 永不执行到，`stream` 不退出，`p.reader.Read(ctx)` 永不返回 EOF/错误，`internal/app/media_service.go:238` 的 `<-errCh` 永久挂起。

约束：不得引入 CGO 依赖；spec 与任务标识符用英文；跨平台（Linux/macOS/Windows）需验证；每次提交必须带测试。

## Goals / Non-Goals

**Goals:**
- 三处缺口在**节点/会话关闭后的 100ms 内**可观测地消失（无残留 goroutine、无 panic、无挂起）。
- 修复不改变任何对外可观察的协议行为：SIP 报文字节、SDP、媒体 RTP 输出、HTTP 响应全部保持不变。
- 每处修复配一个可在 `-race` 下稳定通过的回归测试。

**Non-Goals:**
- 不修复扫描中列出的其他并发点（`file_source.go` / `rtsp_source.go` 的双关、`dialog.go` 的 `Range` 回调重入、`capture/ring.go` 慢消费者条目、`siptransport/transport.go:211` 的同型close-after-send）。它们是独立缺陷，应各自开change；本 change 只做已核实的 A1/A2/A3。
- 不做性能优化（热路径 buffer 复用、pprof 暴露、benchmark 补齐）。
- 不补 `internal/adapter/media` 的 round-trip 测试（候选 B1），那是独立的测试补齐change。

## Decisions

### D1: 看门狗用 stop channel 而非 ctx 做显式唤醒

**选择**：每个 pending INVITE 存 `{timer *time.Timer, done chan struct{}}`；`cancelInviteExpiry` 先 `close(done)` 再 `timer.Stop()`；goroutine 体为
```go
select {
case <-timer.C:   // 自然到期
case <-done:      // 被取消
case <-a.ctx.Done(): // 节点停止
}
```

**备选与否决理由**：
- *只加 `a.ctx.Done()` 分支*：能解决节点停止时的泄漏，但**解决不了 ACK 到达时的泄漏**——ACK 路径只调 `cancelInviteExpiry`，不会取消节点 ctx。仍会每次 cancel 泄漏一个 goroutine。
- *用 `time.AfterFunc`*：`AfterFunc` 的回调在独立 goroutine 执行且可通过返回的 `Timer` 的 `Stop` 阻止，但已经触发后无法中断，且需要额外的回调体 goroutine 计数，语义不如显式 channel 直观。
- *`context.WithCancel` per dialog*：每次分配一个 context + cancel，语义正确但对 30s 定时器这种简单场景偏重；显式 `done chan struct{}` 更轻。

**注册表作用域**：`inviteTimers` 从包级 `sync.Map` 降为 `Acceptor` 字段 `map[string]*inviteWatch`，由 acceptor 已有的互斥保护（或新增一把专用小锁）。`Acceptor` 本身是单实例服务（每个进程一个），因此即使降为字段也满足"节点级隔离"——同一进程内两个 `Acceptor`（测试中常见）不会互相污染。

### D2: splitTransport 用 atomic closed 标志 + sync.Once 双保险

**选择**：`splitTransport` 增加 `closed atomic.Bool` 与 `closeOnce sync.Once`；`Close()` 改为：
```go
s.closeOnce.Do(func() {
    s.closed.Store(true)   // 1. 先标志
    s.cancel()
    <-s.done// 2. 等reader 退出（不再有新的 dispatch 进入）
    close(s.uas); close(s.uac)   // 3. 最后关 channel
})
```
三处 send 统一改为经由一个 helper `trySend(ch chan arrival, a arrival)`，内部 `select { case ch <- a: default: }` **且**入口先 `if s.closed.Load() { return }`。

**备选与否决理由**：
- *只调换 Close 顺序为"先 close chan 再等 done"*：会让仍在运行的 `dispatch` 直接 panic，反而更糟。
- *给 channel 加 recover*：掩盖问题而非修复，且 per-send 的 defer 成本落在热路径上。
- *改为由唯一 reader 负责 close channel*：语义最干净，但 `dispatch` 是被 `run` 调用的同步函数，reader 退出即不再 dispatch，本就能保证"close 在所有 dispatch 之后"——**这一点当前实现已经满足**。真正缺的是"外部注册的 handler 在 Close 之后仍被调用"这一条路径的防御，因此保留 reader-close 模式 + closed 标志兜底。

**为何仍需 closed 标志**：外部通过 `RegisterHandler` 注入的 `fn` 由调用方 goroutine 直接触发，完全绕过 `dispatch`；Close 返回后该 `fn` 若被调用，仅靠 "reader 已停" 无法阻止它向已关闭 channel send。closed 标志提供幂等的短路点。

**TOCTOU 修复**：`dispatch:151-170` 先 `Lock` 读 `already` 再 `Unlock`，随后**再次加锁写入** `handlers[callID] = h`。两次加锁之间 `UnregisterHandler` 可删条目，导致 handler 复活。把检查+写入合并为**一次持锁**的临界区。

### D3: HLSSource 用 CloseWithError 打断 pipe，而非依赖 ctx

**选择**：`HLSSource` 增加字段 `cancel context.CancelFunc` 与 `pw *io.PipeWriter`（均由 `Open` 写入，`Close` 读取，均受 `h.mu` 保护）。`Close()`：
```go
h.mu.Lock()
if h.closed { h.mu.Unlock(); return nil }  // 幂等
h.closed = true
cancel, pw := h.cancel, h.pw
h.mu.Unlock()
if cancel != nil { cancel() }        // 打断在途 HTTP 请求
if pw != nil { pw.CloseWithError(ErrSourceClosed) }  // 打断阻塞的 Read
return nil
```

**备选与否决理由**：
- *只加 `pw.Close()`*：`stream` 仍卡在 `client.Do` 上（最长 10s），只是让 reader 提前返回——治标不治本，goroutine 仍在。
- *只加 `cancel()`*：`stream` 会从 `client.Do` 返回错误并走到下一个 segment 的 `ctx.Done()` 检查后 return，但 `w.Close()` 是 `defer` 的会执行——看似够用；**但**若 `stream` 已 return 而 reader 还在等，`pr` 不会收到 EOF 通知（pipe 的读端只在 writer 真正 Close 时才收到，且 `defer w.Close()` 确实会执行）。因此 cancel 实际上也能让 reader 收到 EOF。**保留 `pw.CloseWithError` 是为了让 reader 收到明确的错误语义而非 `io.EOF`**，因为调用方（`media_service.go:254`）把 `io.EOF` 当作"正常结束"返回 nil，而关闭一个正在推流的会话**不应**被当作正常结束。
- *把 `stream` 的 segment 循环改为 `for ctx.Err() == nil`*：等价于现有检查，无额外收益。

**`ErrSourceClosed` 语义**：新增导出哨兵错误 `media.ErrSourceClosed`，调用方可 `errors.Is` 判断"是主动关闭"而非"源故障"。这与 `internal/domain/model/errors.go` 既有哨兵风格一致。

### D4: 测试策略——用可观测的等待而非 sleep

三处修复的测试都遵循"不依赖固定 sleep 时长"的写法：
- **A1**: 起 N 个 INVITE 对话，记录 `runtime.NumGoroutine()` 基线，全部 cancel 后用 `waitFor(t, cond, 2s)` 轮询直到 goroutine 数回落，而非 `time.Sleep(100ms)`。
- **A2**: 直接构造"Close 已返回 → 再手动触发已注册 handler" 的确定性场景，不依赖竞态窗口自然发生；另加一个并发 Close+dispatch 的 `-race` 测试。
- **A3**: `httptest.Server` 的 handler 阻塞在 `<-r.Context().Done()`（客户端 ctx 被cancel时自动触发），Close 后断言 reader 在 200ms 内返回错误。

## Risks / Trade-offs

- **[D1] `inviteTimers` 从包级改为字段可能遗漏调用点** → 编译期即暴露（`acceptor.go` 内5 处引用集中，apply 阶段用 `go build ./...` 验证）。
- **[D2] `closed` 标志引入额外原子读** → 每条消息 1 次 atomic load，纳秒级，相对 SIP 解析开销可忽略。
- **[D2] 合并 dispatch 两次加锁为一次会略微改变 handler 注册时机** → 原代码本就是"先查后写"的非原子逻辑，合并为原子操作只会更正确，不改变正常路径行为。
- **[D3] `pw.CloseWithError` 会让正在 `Read` 的调用方拿到非 EOF 错误** → 这是**期望行为**（主动关闭 ≠ 正常结束）；`media_service.go` 已把 `io.EOF` 单独处理，其他错误原样返回，无需改动调用方。
- **[D3] `HLSSource` 现有测试若假设"Close 后仍可读完剩余分片"会失败** → apply 阶段先跑现有 HLS 测试确认；若有冲突，改为在测试中显式断言新的关闭语义。
- **[测试] `runtime.NumGoroutine()` 断言在 `-race` 下可能因测试框架自身 goroutine 抖动而 flaky** → 使用"基线 + 容差"而非精确相等，并优先用"cancel 后 watcher 数量归零"的内部可观测量（暴露为测试钩子或直接检查 map 长度）而非全局 goroutine 计数。

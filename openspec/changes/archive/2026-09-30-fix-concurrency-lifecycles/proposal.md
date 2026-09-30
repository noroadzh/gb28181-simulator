# Proposal: fix-concurrency-lifecycles

## Why

媒体热循环与 SIP 路由热路径上的三处生命周期缺口在节点/媒体会话关停时可被触发，造成 goroutine 泄漏、`send on closed channel` panic 或 reader 永不返回。当前 spec 隐含承诺"启停互不影响""Close 关闭"，但未明文约束。本 change 把这三处缺陷显式写入既有 capability 的 requirement，并以单测与回归测试覆盖。

## What Changes

- **`internal/app/acceptor.go`** 的 INVITE 过期看门狗（`watchInviteExpiry`）：将进程级 `sync.Map inviteTimers` 降为 Acceptor 实例字段；过期 goroutine 增加 `ctx.Done()` 分支；cancel 路径在 `Stop`/`Close` 时主动 wakeup goroutine 而非仅 `Stop()`。
- **`internal/app/node_split_transport.go`** 的 `splitTransport`：`Close` 顺序调整（先 close queues 再 wait reader），并为两路 queue 加 closed 原子标志，send 路径检查后放弃；恢复被 close 后触发的 transaction handler 的 nil/closed 防御。
- **`internal/adapter/media/hls_source.go`** 的 `HLSSource`：`Open` 返回的 `io.PipeReader`/`io.PipeWriter` 必须支持 `Close` 立即打断读端，`Close()` 同时关闭 pipe writer（携带错误）并 cancel 内部 ctx，使 `stream` goroutine 与 `PacketizeOutbound` 都能在毫秒级返回。
- 配套单测：
  - `acceptor_test.go` 新增 `TestAcceptor_INVITEExpiry_NoLeak`：起 200 个 INVITE → 全部 cancel → 等 100ms → 用 `runtime.NumGoroutine` 与 `runtime.NumDead` 间接比对；节点 Stop 后进程不泄漏 INVITE 过期 goroutine。
  - `node_split_transport_test.go` 新增 `TestSplitTransport_CloseRace`：在 reader 未退出窗口里从外部触发一个注册 handler，验证不 panic（`-race` 必须干净）。
  - `hls_source_test.go` 新增 `TestHLSSource_CloseUnblocksReader`：用 `httptest.Server` 返回 5s 才到位的慢响应，`ctx` 立刻 cancel，`Close()` 后 `Read` 在 200ms 内返回错误，且 `Open` 返回的 reader 已 `Close` 不再可读。

## Capabilities

### Modified Capabilities

- `media-sources`: 媒体源 `Close()` MUST 在 100ms 内使 `Open()` 返回的 reader `Read()` 返回非 nil 错误（即便 ctx 未取消）；源内部的 stream goroutine MUST 在 `Close()` 后退出。
- `core-sip-stack`: `splitTransport.Close()` MUST 与 `dispatch`/外部 handler 调用方并发安全；Close 后任何对内部 queue 的 send MUST NOT panic；queue 的关闭顺序 MUST 是 "先 set closed 标志 → 再 close chan → 再等 reader"。
- `node-abstraction`: 节点生命周期 MUST 回收所有由该节点发起的后台 goroutine（含 INVITE 过期看门狗），节点 Stop 后 `runtime.NumGoroutine()` 相对启动期不应增长；状态/计时器不能挂在进程级全局容器中跨节点共享。

## Non-goals

本 change 不引入任何新的 GB/T 28181 协议能力。以下均显式排除：

- **不新增/修改任何 SIP 方法、头字段、MANSCDP 指令或 SDP 属性**。本 change 只修并发与生命周期，不触碰报文字节。
- **不实现 2022 增量能力**（PTZ 精准位置、看守位、巡航轨迹、软件升级、图像抓拍、H.265/AAC/G.722.1）——这些属于 `gb28181-2022` capability 的范围。
- **不实现级联与多级路径发现**（X-RoutePath/X-PreferredPath、设备 ID 改写）——属于 `cascade-routing` capability。
- **不实现场景引擎、YAML 场景报告、pcap 导出**——分别属于 `scenario-engine` 与 `capture-and-pcap`。
- **不新增媒体源类型**（不新增 WebRTC、SRT、GB28181 附录 D 之外的传输）。本 change 只修既有 HLS 源的关闭语义。
- **不修扫描中列出的其他并发点**：`file_source.go`/`rtsp_source.go` 的 FD 双关、`dialog.go` 的 `Range` 回调重入、`capture/ring.go` 慢消费者条目、`siptransport/transport.go:211` 的同型 close-after-send。它们是独立缺陷，各自开 change。
- **不补 `internal/adapter/media` 的 round-trip 测试**（PS/RTP 编解码）、**不补 SM2 错误注入测试**——那是独立的测试补齐 work，与本 change 的并发修复正交。
- **不做性能优化**：热路径 buffer 复用、pprof 端点暴露、benchmark 补齐均不在范围内。

## Impact

- 改动文件：`internal/app/acceptor.go`、`internal/app/node_split_transport.go`、`internal/adapter/media/hls_source.go`，以及对应 `_test.go` 文件。
- 新增字段：Acceptor 增加 `inviteTimers map[string]*time.Timer` 与 `inviteWatchers map[string]chan struct{}`（用于 wake-up）。
- `HLS` 与 split 行为对外部保持兼容；仅在 Close 路径上缩短资源回收延迟并消除 panic。
- 路线图：本 change 落在第一阶段之后，是对既有 capability 的不变量强化，不引入新的对外 capability，因此不打乱 roadmap 顺序。
# Spec Delta: media-sources

## MODIFIED Requirements

### Requirement: MediaService 提供出站管道入口

系统 MUST 提供将媒体源与 PS packetizer 和 RTP packetizer 组合为出站管道的功能，
并暴露 `Open/Close/Read` 语义，使调用方能够从实时媒体源拉取 RTP 包。媒体源故障
MUST 通过节点生命周期上报，且不中断信令。

`MediaService` 是出站管道的入口：把源、PS 封装、RTP 分包串起来，对外暴露
开/关/读；源失败只报错到节点生命周期，不中断信令。

媒体源关闭语义（本 change 新增的不变量）：任何媒体源的 `Close()` MUST 使此前
`Open()` 返回的 reader 的阻塞中的 `Read()` 在 100ms 内返回非 nil 错误，无论调用方
传入的 context 是否已取消；源内部的下流 goroutine（如 HLS 分片下载循环）MUST 在
`Close()` 后退出。重复调用 `Close()` MUST 幂等且不 panic。

#### Scenario: Open 把配置好的源接入管道

- **WHEN** 以源名称和 transport 配置调用 `Open`
- **THEN** 创建 `MediaService` 管道：源 → PS → RTP
- **AND** `Read` 产出 sequence number 递增且以源 PTS 作为 RTP timestamp 的 RTP 包

#### Scenario: 失败的源不会杀死节点

- **WHEN** 底层媒体源无法打开或在读取时报错
- **THEN** `Read` 返回该错误，节点被上报存在媒体故障，而信令生命周期不受影响

#### Scenario: Close 恰好释放管道一次

- **WHEN** 对已打开的 `MediaService` 调用 `Close`
- **THEN** 源被关闭，管道被拆除，后续读取返回 closed 错误

#### Scenario: Close 打断慢分片的阻塞 Read（本 change 新增）

- **WHEN** HLS 源正在等待对端产出下一个分片（可能耗时数秒），调用方在未取消 context 的情况下调用 `Close()`
- **THEN** `Open()` 返回的 reader 的阻塞 `Read` 在 100ms 内返回非 nil 错误，内部下载 goroutine 随之退出

#### Scenario: Close 幂等（本 change 新增）

- **WHEN** 对同一媒体源实例连续调用两次 `Close()`
- **THEN** 第二次调用无错误无 panic 返回，且第一次调用启动的 goroutine 不残留

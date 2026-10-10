# Spec Delta

## ADDED Requirements

### Requirement: PS 管道并发安全

`MediaService.SubscribePS` MUST 在所有并发场景下（reader 自然 EOF、ctx 取消、caller 主动调用 cleanup 函数）保证返回的 PS 帧 channel 只被关闭一次。任何路径下都不应触发 `panic: close of closed channel`。

#### Scenario: reader 自然 EOF 触发关闭
- **WHEN** 媒体源读到 EOF，reader goroutine 正常退出
- **THEN** 返回的 channel 被关闭一次，caller 后续调用 `cleanup()` 不会触发 panic

#### Scenario: ctx 取消触发两条关闭路径
- **WHEN** ctx 被取消，reader goroutine 因 `<-ctx.Done()` 退出，同时 caller 调用 `cleanup()` 关闭 src
- **THEN** channel 仅被关闭一次（无 panic），caller 可正常从 channel 的 range 循环退出

#### Scenario: caller 主动调用 cleanup
- **WHEN** caller 在 channel 消费完成前调用 `cleanup()` 函数
- **THEN** src 被关闭，channel 在 reader 自然 EOF 或 ctx 取消时被关闭一次（无 panic）

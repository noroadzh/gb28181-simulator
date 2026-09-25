# Tasks

## 1. 扩展 domain model

- [x] `internal/domain/model/notify.go`：新增 `CmdTypeSubscribe`、`CmdTypeMediaStatus`
  、`CmdTypePlaybackControl` 常量和判断方法
- [x] `internal/domain/model/dialog.go`：新增 `DialogState`、`DialogStatus`（calling /
  proceeding / confirmed / terminated）、`MediaSession` 值对象
- [x] `internal/domain/model/subscribe.go`：新增 `Subscription`、`SubscriptionState`
- [x] `internal/domain/model/media_status.go`：新增 `MediaStatus`、`VideoParam`、
  `AudioParam`、`RecordStatus` 值对象

## 2. 新增 domain port 接口

- [x] `internal/domain/port/playback.go`：新增 `PlaybackPort` 接口（StartPlayback /
  StopPlayback / QueryRecord）
- [x] `internal/domain/port/subscribe.go`：新增 `SubscribePort` 接口（Subscribe /
  Unsubscribe / Notify）
- [x] `internal/domain/port/media_status.go`：新增 `MediaStatusPort` 接口
  （UpdateMediaStatus / ForwardMediaStatus）
- [x] 在 `internal/adapter/` 新增占位 adapter，携带 `var _ port.Xxx = (*Adapter)(nil)` 断言

## 3. 扩展 MANSCDP codec

- [x] `internal/adapter/manscdp/codec.go`：`DecodeNotify` 对未知命令类型返回可解析
  的 Notify，不报错
- [x] `internal/adapter/manscdp/subscribe.go`：新增 `MarshalSubscribe` / `MarshalNotify`
  方法
- [x] `internal/adapter/manscdp/media_status.go`：新增 `MediaStatusNotify` 编解码
- [x] `internal/adapter/manscdp/playback.go`：新增 PlaybackControl 编解码

## 4. 升级 splitTransport 为事务分拣

- [x] `internal/app/node_split_transport.go`：新增 `TransactionMatcher` 接口与
  `transactionSplitTransport` 实现
- [x] 维护 `Call-ID → TransactionHandler` 映射
- [x] 请求 → serving 半；响应 → Call-ID 匹配的事务处理器
- [x] 携带现有方向分拣作为 fallback

## 5. 实现 Dialog / Session 管理

- [x] `internal/app/dialog.go`：`DialogManager`，维护 `sync.Map[Call-ID]*DialogState]`
- [x] 新建 Dialog 时生成随机 tag（`-randomTag()` 复用现有逻辑）
- [x] 状态机：calling → proceeding → confirmed → terminated
- [x] 超时清理（30s 无响应触发 terminated）

## 6. 扩展 Acceptor serving loop

- [x] `handleInvite`：解析 SDP → 创建 Dialog → 启动入站媒体管道 → 应答 200 OK + SDP
- [x] `handleSubscribe`：记录订阅关系 → 返回 200 OK + 可选 NOTIFY
- [x] `handleOptions`：返回 200 OK，记录 OPTIONS 时间戳
- [x] `handleAck` / `handleBye`：事务完成与清理
- [x] 向平台-small 注册一个 `handleInvite` 的分发逻辑

## 7. 组装入站媒体管道

- [x] `internal/app/media_service.go`：新增 `OpenInboundPipeline` 方法
- [x] 绑定 addr（来自 SDP）创建 UDP listener
- [x] RTPDeizer → PSDepacketizer → ESWriteCloser 管道组装
- [x] Dialog 生命周期绑定：confirmed 时启动，terminated 时 Close + 释放端口

## 8. 扩展 Keeper（OPTIONS 保活）

- [x] `internal/app/keeper.go`：新增 `optionsEnabled` / `optionsInterval` 字段
- [x] 启动 OPTIONS 探测：定期发送 `OPTIONS` 请求，处理 200/408 响应
- [x] 连续 N 次 408 触发节点 fault

## 9. platform-small 集成

- [x] `internal/app/node_service.go`：Start 时组装 DialogManager 与入站管道
- [x] Stop 时清理所有 Dialog，释放所有端口
- [x] 配置解析：新增 `platform.options.enabled` / `platform.options.interval` 字段支持

## 10. 测试

- [x] 单元测试：`dialog_test.go` 状态机 / 超时清理
- [x] 单元测试：`acceptor_message_test.go` INVITE 200 OK / 400 Bad Request / 超时
- [x] 单元测试：`acceptor_message_test.go` SUBSCRIBE 订阅 / 过期 / NOTIFY 推送
- [x] 单元测试：`acceptor_message_test.go` OPTIONS 200 / 408 / 连续失败 fault
- [x] 单元测试：`acceptor_message_test.go` MediaStatus 解析与转发
- [x] MANSCDP golden test：新增 `testdata/manscdp-subscribe-*.xml`、
  `testdata/manscdp-mediastatus-*.xml`
- [x] 集成测试：`node_service_platform_small_test.go` 完整 INVITE 点播 e2e

## 11. 审查与归档

- [x] 使用 [subagent:code-reviewer] 对照 proposal/design/tasks/spec 做最终审查
- [x] 修复所有阻塞项
- [x] `go build ./...` 与 `go test ./... -count=1` 绿色
- [x] `openspec validate platform-small-supplementary --strict` 绿色
- [x] `openspec archive platform-small-supplementary` 归档

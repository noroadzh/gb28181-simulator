# Tasks

## 1. OpenSpec artifacts

- [x] 1.1 `proposal.md`：定义非目标、变更范围、影响
- [x] 1.2 `design.md`：splitTransport 升级、Dialog 管理、MANSCDP 扩展、媒体管道组装、port 接口、Keeper 扩展
- [x] 1.3 `tasks.md`：本文件
- [x] 1.4 `specs/platform-small-node/spec.md`：delta spec（ADDED 需求）
- [x] 1.5 `openspec validate` 通过

## 2. Domain model 扩展

- [x] 2.1 `internal/domain/model/notify.go`：新增 `CmdTypeSubscribe`、`CmdTypeMediaStatus`
- [x] 2.2 `internal/domain/model/dialog.go`（新增）：DialogState、DialogDirection、Session 值对象
- [x] 2.3 `internal/domain/model/media.go`：PlaybackControl、SubscribeInfo、MediaStatusReport 值对象

## 3. Domain port 接口

- [x] 3.1 `internal/domain/port/media.go`：新增 `PlaybackPort`、`SubscribePort`、`MediaStatusPort`
- [x] 3.2 `internal/domain/port/media_test.go`：compile-time 断言（adapter 实现）

## 4. MANSCDP 扩展

- [x] 4.1 `internal/adapter/manscdp/subscribe.go`（新增）：Subscribe 编解码
- [x] 4.2 `internal/adapter/manscdp/mediastatus.go`（新增）：MediaStatus 编解码
- [x] 4.3 `internal/adapter/manscdp/playback.go`（新增）：PlaybackControl 编解码
- [x] 4.4 `internal/adapter/manscdp/codec.go`：`DecodeNotify` 支持新命令类型
- [x] 4.5 单元测试覆盖新增编解码

## 5. SIP 方法处理器

- [ ] 5.1 `internal/app/acceptor.go`：`ServingHandler` 新增 `handleInvite/handleSubscribe/handleOptions/handleAck/handleBye`
- [ ] 5.2 `internal/app/node_split_transport.go`：`handleMessage` 按 method 分发到新处理器
- [ ] 5.3 `internal/app/node_service.go`：实现各处理器逻辑（Dialog 创建、SDP 应答、200 OK）

## 6. Transaction splitter 升级

- [ ] 6.1 `internal/app/node_split_transport.go`：升级为 `transactionSplitTransport`，支持 Call-ID 级别事务路由
- [ ] 6.2 serving 半和 registering 半各自注册事务处理器
- [ ] 6.3 单元测试验证事务分拣正确性

## 7. Dialog/Session 管理

- [x] 7.1 `internal/app/dialog.go`：DialogManager 实现（New/Create/Get/Delete/Terminate）
- [ ] 7.2 Dialog 状态机：calling → confirmed → terminated
- [ ] 7.3 Dialog 超时清理：Timer 管理
- [ ] 7.4 单元测试覆盖 Dialog 生命周期

## 8. 入站媒体管道

- [ ] 8.1 `internal/app/media_service.go`：新增 `InboundPipeline` 方法
- [ ] 8.2 组装 RTPDeizer → PSDepacketizer → ESWriteCloser
- [ ] 8.3 绑定 Dialog 生命周期（INVITE 200 OK 启动，BYE/超时关闭）
- [ ] 8.4 e2e 测试：入站媒体流经管道

## 9. Keeper OPTIONS 扩展

- [ ] 9.1 `internal/app/keeper.go`：新增 OPTIONS 保活探测
- [ ] 9.2 区分 200/408/5xx 响应
- [ ] 9.3 单元测试覆盖 OPTIONS 响应处理

## 10. platform-small 集成

- [ ] 10.1 `internal/app/node_service.go`：NodeService 组装 Dialog 管理与入站管道
- [ ] 10.2 `internal/app/acceptor.go`：ServingHandler 集成 Dialog 与媒体管道
- [ ] 10.3 `go build ./...` 通过

## 11. 测试

- [ ] 11.1 SIP 方法 golden test：INVITE/SUBSCRIBE/OPTIONS/ACK/BYE
- [ ] 11.2 MANSCDP 编解码 test
- [ ] 11.3 Dialog 状态机 test
- [ ] 11.4 入站媒体 e2e test
- [ ] 11.5 `go test ./... -race -count=1` 全绿

## 12. 收尾

- [ ] 12.1 `openspec validate` 通过
- [ ] 12.2 同步主 spec
- [ ] 12.3 归档
- [ ] 12.4 提交 commit

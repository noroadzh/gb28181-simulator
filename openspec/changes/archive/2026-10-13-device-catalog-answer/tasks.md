# Tasks

## 1. Acceptor.ServeDevice 与 platform.isDevice 标记

- [x] 1.1 在 `internal/app/acceptor.go` 的 `platform` struct 中新增 `isDevice bool` 字段
  - 验收：`go build ./internal/app/...` 通过
- [x] 1.2 新增 `Acceptor.ServeDevice(id model.NodeID, tr port.SIPTransport, realm string) error` 方法：
  - 复用 `Serve()` 的 readLoop 骨架，**不**启动 catalogPoller（device 不发 Query），**不**调用 `devices.Clear()`（device 无下游）
  - 创建 `platform{isDevice: true, policy: model.NewExpiresPolicy(3600, 3600, 3600)}` 并加入 `a.serving` map
  - realm 校验：空字符串返回 error
  - 验收：`go build ./internal/app/...` 通过
- [x] 1.3 在 `acceptor.run` 的 method switch 入口处加 device 节点 gate：device 节点只允许 `MESSAGE` 方法；其他方法返回 405 Method Not Allowed（Allow 头含 MESSAGE）；`ACK` 例外——静默丢弃不响应（RFC 3261 §13.2.2.4：UAS 不答 ACK）
  - 验收：device 节点对 OPTIONS 响应 405，Allow 头含 MESSAGE

## 2. NodeService 集成

- [x] 2.1 在 `internal/app/node_service.go` 的 device 节点注册成功路径调用 `s.acceptor.ServeDevice(id, tr, realm)`
  - 验收：device 节点 `register()` 返回 nil 后 `ServeDevice` 必被调用
- [x] 2.2 在 device 节点反注册（Unregister）路径调用 `s.acceptor.Stop(id)`，复用既有 stop 逻辑
  - 验收：device 节点 Unregister 后 ServeDevice 启动的 goroutine 必退出

## 3. 单元测试

- [x] 3.1 新增 `TestAcceptor_DeviceAnswersCatalogQuery`（`internal/app/acceptor_device_test.go`，用 fake transport 覆盖应答体）：device 侧 ServeDevice + Registry，注入 catalog query MESSAGE，断言 device 响应 200 OK + 完整 catalog body（含 MediaSource 扩展）
  - 验收：`go test ./internal/app/ -run TestAcceptor_DeviceAnswersCatalogQuery` 通过
- [x] 3.2 新增 `TestAcceptor_DeviceReturns405ForNonMessage`：device 节点收到 OPTIONS / INVITE / BYE / SUBSCRIBE / INFO，断言响应 405 + Allow: MESSAGE；ACK 静默丢弃
  - 验收：`go test ./internal/app/ -run TestAcceptor_DeviceReturns405ForNonMessage` 通过
- [x] 3.3 新增 `TestAcceptor_DeviceCatalogResponseWithoutMediaSource`：device profile 不配 MediaSource，断言 catalog Item 无 Extension 段
  - 验收：`go test ./internal/app/ -run TestAcceptor_DeviceCatalogResponseWithoutMediaSource` 通过
- [x] 3.4 新增 `TestAcceptor_DeviceCatalogResponseWithNodeMediaFallback`：device profile 仅节点级 mediaConfig，断言每个 channel Item 携带节点级 MediaSource 扩展
  - 验收：`go test ./internal/app/ -run TestAcceptor_DeviceCatalogResponseWithNodeMediaFallback` 通过
- [x] 3.5 新增 `TestAcceptor_DeviceAndPlatformShareAcceptor`：同一 `Acceptor` 实例同时 `ServeDevice` 与 `Serve` 一个 platform-large 节点，断言 `Serving() == 2` 且两个节点的 MESSAGE 各自被自己的 read loop 应答（覆盖 spec 中「device 与 platform-large 共用 Acceptor」scenario）
  - 验收：`go test ./internal/app/ -run TestAcceptor_DeviceAndPlatformShareAcceptor` 通过

## 4. 端到端回归

- [x] 4.1 全量回归：`go test ./...` 通过
  - 验收：所有包测试无失败
- [x] 4.2 `openspec validate --strict` 通过
  - 验收：CLI exit 0，无 warning

## 5. 部署验证

- [x] 5.1 在测试环境（10.96.1.125）重建 device + platform-large 双容器（`/slow2/gb28181-simulator/device` 与 `/slow2/gb28181-simulator/platform`）
  - 验收：device 与 platform 各自启动成功（`docker compose ps` 两者 healthy）
- [x] 5.2 等待 catalogPoller 一轮周期（30s）后 `curl http://10.96.1.125:18080/v1/nodes/34020000002000000001/channels/34020000130000000001/media`
  - 验收：响应 `has_media: true, media_kind: "file"`——实测 `GET /v1/nodes/34020000002000000001/channels` 返回 `[{"id":"34020000130000000001",...,"has_media":true,"media_kind":"file"}]`
- [x] 5.3 检查 device 端日志出现 catalog 应答记录
  - 验收：命中。因 `answering a catalog query` 为 Debug 级别，需将 device `log.level` 临时调为 `debug`；等价的功能证据是 platform 侧 `catalog response received; importing media sources`（channels:2）以及 5.2 的 `has_media: true`

## 6. 文档与提交

- [x] 6.1 提交 `bd1865d`，commit message: `feat(app): device 节点应答 SIP catalog query，复用 Acceptor 解决 platform 媒体源回填链路`
  - 验收：`git log -1` 显示该提交，含单元测试

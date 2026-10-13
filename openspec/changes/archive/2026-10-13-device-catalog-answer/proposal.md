# Proposal: Device Node Answers Catalog Query via Reused Acceptor

## Why

device 节点（YAML 配置为 `kind: device`）注册成功后，platform-large 通过 `CatalogPoller` 周期性发送 `MESSAGE/Query` (CmdType=Catalog) 查询下级通道列表。但 device 侧只有 `Registrar.transaction` 在收 SIP 消息——它**只消费自己 Call-ID 的 Response**（REGISTER 认证），所有其他 SIP Request 直接被丢弃。因此 catalog query 永远到不了 `answerCatalog`，platform 侧的 channel `has_media: false` 永远无法被回填。

对应路线图阶段：阶段 5（device-node）缺陷修复，阶段 9（cascade-and-multi-instance）的前置依赖。

## What Changes

- **device 节点**：注册成功后额外启动一个 SIP 读循环，复用 `Acceptor` 的 `handleMessage` → `answerCatalog` 处理来自 platform 的 catalog query，返回携带 `<Extension><MediaSource>` 的完整目录响应
- **非 catalog 的 MESSAGE**：保持 `handleMessage` 既有逻辑（心跳响应、DeviceInfo 等）
- **平台发来的其他方法**：device 返回 405 Method Not Allowed（而非静默丢弃）
- **降级兼容**：无 media source 配置时 catalog Item 无 Extension 段，与真实设备行为一致

## Capabilities

### New Capabilities

- `device-catalog-answer`（路径：`device-catalog-answer`）：定义 device 节点作为 UAS 接收并响应 catalog query 的行为契约。包括读循环复用 `Acceptor` 的规则、device 节点只应答 MESSAGE（其他方法 405）的规则、catalog 响应携带 Channel 列表（含自身 channels）与 MediaSource Extension 的规则。

### Modified Capabilities

- （无 spec 级行为变更。`cascade-media-source-propagation` 的 `applyMediaSourcesFromCatalog` 已在 catalogPoller 路径中实现，本 change 只需修复 device 侧的 query 接收）

## Non-goals

- 不改变 device 注册流程（REGISTER 认证逻辑不变）
- 不改变 platform 侧的 CatalogPoller 发送逻辑
- 不改变 catalog XML 扩展段格式（已由 `cascade-media-source-propagation` 规定）
- 不改变 `answerCatalog` 的聚合逻辑（device 无下游，`a.devices.List()` 为空，`a.registry.Get(p.id).Profile().Channels()` 即 device 自身 channels）
- 不实现 device 主动向 platform 推送 catalog NOTIFY 的机制
- 不实现 device 对 INVITE/BYE/SUBSCRIBE 等平台发来请求的处理（仅返回 405）

## Impact

- **SIP 层**：`internal/app/acceptor.go`——新增 `ServeDevice()` 方法，复用 `Acceptor.run` 的 readLoop，只应答 MESSAGE 方法
- **NodeService 层**：`internal/app/node_service.go`——device 节点注册成功后调用 `s.acceptor.ServeDevice(id, tr)`
- **回归风险**：低。device 节点不改变任何已有行为（REGISTER 认证路径不变）；新增 goroutine 只收 MESSAGE，405 应答不影响任何已有流程
- **测试**：新增 `TestAcceptor_DeviceAnswersCatalogQuery` 集成测试（双端真实 socket）

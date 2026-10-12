# Design: cascade-media-source-propagation

## Context

参考 `proposal.md` 的动机与 `specs/cascade-media-source-propagation/spec.md` 的契约。本节说明实现层面的关键技术决策。

**当前架构事实**：
- `acceptor.handleCatalog` (acceptor.go:~2150) 在 platform 节点被 catalog 拉取时聚合：在线 device 表 + `a.downstreamChannels(...)`（device 自身若是 simulator 节点，展开其 profile channels）+ platform 自身 channels
- `channelCatalogItems` (acceptor.go:~2252) 把 `model.Channel` 渲染为 `CatalogItem`，**目前不携带媒体源信息**
- `manscdp.MarshalCatalog` (catalog.go) 把 `CatalogItem` 序列化为 XML，**目前 `catalogItem` struct 不含扩展段字段**
- platform 收到下级 catalog 响应通过 `manscdp.DecodeNotify`，目前作为 `Notify` 解析（catalog 是 response 体，**目前 simulator 之间的 catalog 拉取路径是单向上行**：device 响应 platform 的查询；**反向**——platform 主动拉取 device 目录——通过 platform-small 主动 SUBSCRIBE/QUERY 实现，不在 acceptor 主路径）

**待定问题**：当前 device→platform 之间是否存在「platform 主动拉取 device catalog」的实现？需要先核实。本设计**假定**通过以下机制之一实现（任务阶段会确认）：
- 选项 A：复用现有 platform-small 主动 `SUBSCRIBE catalog` 机制（catalog 订阅）
- 选项 B：新增 platform 启动时 + 设备注册后主动 `MESSAGE/Query catalog` 拉取
- 选项 C：依赖 device 端 `NOTIFY catalog` 上行（国标标准选项）

如果当前不存在 A/B/C，则 tasks.md 阶段需要补一个「catalog 拉取触发机制」任务。

## Goals / Non-Goals

**Goals:**
- 在 catalog XML Item 末尾追加 `<Extension>/<MediaSource>` 扩展段，携带 device 端 `mediaConfigByChannel` 的有效媒体源
- platform 端 catalog 响应解析时识别该段并回填到本地 `NodeProfile.mediaConfigByChannel`
- 保持对标准国标设备的零影响（无扩展段时不报错、不回填、行为不变）
- 单元测试覆盖扩展段编解码 + fallback 矩阵 + golden test 字节级断言

**Non-Goals:**
- 不实现 device 主动 NOTIFY 触发 platform 回填
- 不实现增量 catalog 差异同步（每次都全量）
- 不实现 catalog 拉取触发机制本身（如果缺失，作为独立 task 添加）
- 不修改 `channel-http-api` 的 HTTP 端点契约
- 不修改 MANSCDP 标准字段名/顺序/类型
- 不支持一个通道的多个媒体源（仅单值覆盖式）

## Decisions

### 决策 1：扩展段命名为 `<Extension>` 包裹 `<MediaSource>`

**理由**：
- GB/T 28181-2016 §A.2.4.1 catalog XML 允许 Item 内含厂商自定义扩展
- 嵌套结构（`Extension` 外壳 + `MediaSource` 元素）便于未来扩展其他段（如 PTZ 能力、报警能力），不局限于媒体源
- 元素属性而非子元素：与 `device-platform` 节点列表里现有的 attribute-only 风格一致

**备选**：
- A. 直接在 Item 下放 `<MediaSource>`（无外壳）——更紧凑但难以扩展其他段
- B. 用 `<Info>` 段（标准国标 Info XML 风格）——更"标准"但可能被误解析

**选定**：A 但用 `<Extension>` 包裹——既简洁又可扩展。

### 决策 2：XML 编解码扩展方式

**理由**：
- Go `encoding/xml` 默认严格匹配已知字段，**未声明的字段会被丢弃**——这正是我们想要的副作用：标准 MANSCDP 解析器忽略扩展段
- 但我们自己解析时 MUST 能拿到扩展段，所以**需要新增一个 decoder**（不修改既有 `DecodeNotify` 路径）

**实现策略**：
- `manscdp/catalog.go` 新增 `Extension struct { MediaSource *MediaSource }` + `MediaSource struct { Kind, Path string; Loop bool; ... }`，在 `catalogItem` 中加 `Extension *Extension `xml:"Extension"`` 字段
- `MarshalCatalog` 在构造 `catalogItem` 时按 `mediaConfigByChannel` 决定是否填充 `Extension`
- 平台侧 catalog 解码**复用既有 `MANSCDPCodec` 接口**，但需要新方法 `DecodeCatalogResponse` 解析标准 catalog response（含扩展段）；既有 `DecodeNotify` 不动

**备选**：
- A. 在 `DecodeNotify` 路径里 hack 一个 type 路由——侵入大、影响 notify 解析
- B. 新增独立的 `DecodeCatalogResponse` 方法——更干净，spec 边界清晰

**选定**：B。

### 决策 3：MediaSource 属性顺序固定为 kind/path/loop/fps/clock/mtu/ssrc

**理由**：
- 与 spec 中的 Scenario 描述一致
- Go xml 包的 struct field 顺序 = 输出顺序，**便于 golden test 字节级断言**
- 省略零值字段（`xml:",omitempty"`）以保持向后兼容（旧版本读到零值不会看到无意义空字段）

**备选**：
- A. 全量输出（即使零值）——XML 体积大、向后兼容差
- B. 统一字符串格式序列化——不友好，难以解析

**选定**：A 的 omitempty 变体。

### 决策 4：平台端回填时机

**理由**：
- 启动后首次 catalog 拉取：可立即看到 device 端媒体源
- 设备注册/心跳刷新：catalog 拉取周期触发（标准 GB28181 30s 周期）
- 不引入额外的事件总线或 web socket 推送

**实现位置**：
- 在 `acceptor` catalog 响应解析路径上，**新加一个 helper `applyMediaSourcesFromCatalog`**：解析完 catalog 后，对每个 Item 调用该 helper 写回 `NodeProfile.mediaConfigByChannel`
- 写入动作通过 `NodeService.SetChannelMediaConfig` 完成（已有 API）

**备选**：
- A. 在 `NodeService.Create` 期间异步拉取——侵入 lifecycle
- B. 在 HTTP API 层 `ListChannels` 触发拉取——延迟首次显示

**选定**：catalog 解码路径内同步回填，简单一致。

### 决策 5：错误处理策略

**理由**：
- 扩展段解析失败 = 设备不规范，platform 不应拒绝整个 catalog 响应
- 符合 GB/T 28181 原则：接收方宽容、发送方严格

**策略**：
- `DecodeCatalogResponse` 中捕获扩展段解析错误，warn 日志后继续
- catalog 主结构（CmdType/SN/DeviceID/Item list）解析失败 → 整体错误（标准行为）

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| 平台 catalog 拉取周期长（30s），device 媒体源变更到 UI 可见有延迟 | spec 接受「最终一致」语义；不在本 change 引入主动刷新 |
| 扩展段未来被 GB/T 28181 后续版本定义为标准字段导致冲突 | 元素名用 `<Extension>` 外壳隔离；属性命名空间 `xmlns:sim="..."` 留作后续防御（当前不加） |
| platform 本地已有 mediaConfigByChannel 被 device 覆盖可能造成「用户配置的媒体源丢失」 | spec 明确「最近一次 catalog 响应为准」语义；后续 change 引入「平台覆盖保护」（手动标记免覆盖） |
| 设备 catalog 拉取如果不存在则 tasks 阶段需要补 | tasks.md 阶段先验证 catalog 拉取路径存在性，缺失则作为 task 1 优先添加 |
| 真实国标设备的 catalog Item 不含 channel 段，platform 把 device 本身当 channel 看待 | spec 「Scenario: 真实国标设备（无扩展段）兼容」明确此行为；channel-http-api 已有 fallback 兜底 |

## Migration Plan

无需数据迁移（不修改存储 schema）。部署步骤：
1. 合并代码并通过单元测试 + golden test
2. 在测试环境（10.96.1.125）重建 device + platform-large 双容器
3. 验证：device yaml 配置 file 媒体源 → platform 通道详情页可见 `has_media: true`
4. 验证：device 移除 media_source → platform 仍可见（catalog 周期 30s 内保持，超期后清除）
5. 验证：真实国标设备（无扩展段）行为不变

回滚：git revert 即可，扩展段为新增字段，移除后回到 change 之前行为。

## Open Questions

- **Q1**：当前 platform-large 是否已实现对 device 的 catalog 拉取（主动或被动）？如果未实现，需要先在 tasks.md 添加「catalog 拉取触发」任务。
  - **答复（task 1.1 核实，2026-10-12）**：**未实现**，本 change 必须新增这条路径。
    - `internal/app/acceptor.go:801` 的 `case notify.IsCatalogQuery():` 是**接收**分支——本节点作为 UAS **回答**别人对本节点的查询。`IsCatalogQuery` 全仓库仅此一处调用点，**没有任何代码**用它构造 platform 主动发出的 catalog 查询。
    - `internal/app/acceptor.go:2199-2203` 的 `answerCatalog` 聚合的是**本节点**的在线设备表（`a.devices.List`）+ 本节点 registry 里的 profile channels，不向下级发起任何请求。
    - `internal/adapter/subscribe/stub.go:26` 的 `Subscribe` 直接返回 `ErrSubscribeUnsupported`，是未实现的占位；`a.subscribe` 虽已装配（`acceptor.go:107,286`），但 Acceptor 内部**从未调用**。
    - `internal/adapter/manscdp/catalog.go:36-43` 的 `catalogResponse` 仅用于**序列化输出**，无对应的反序列化入口。
    - 结论：catalog 目前是**单向被动**流动（只答不问），且**没有任何代码解析 catalog 响应**。
  - **影响**：design 决策 4「回填时机」中的「catalog 拉取周期」需要一个实现载体。本 change 新增：platform 侧 `CatalogPoller`（周期主动 `MESSAGE + CmdType=Catalog` 查询下级）+ `DecodeCatalogResponse`（解析响应）。见 tasks.md 第 6 组。
- **Q2**：catalog 拉取周期是否可配置？30s 默认值若太慢，是否需要为媒体源变更引入更短的轮询？当前决定不引入，仍按 30s。
- **Q3**：扩展段是否需要命名空间（`xmlns`）？当前不加，等 GB/T 28181 后续版本有定义时再加。

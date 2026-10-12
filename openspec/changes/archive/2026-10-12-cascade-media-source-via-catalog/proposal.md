# Proposal: Cascade Media Source Propagation via SIP Catalog XML Extension

## Why

当 device 节点在 YAML 中配置了媒体源后，作为其上级平台的 platform-large 节点在 Web UI 的通道详情页仍显示「媒体源未配置」。根因是两容器完全独立——platform 的节点配置中没有该设备通道的媒体源，而标准 SIP catalog 只同步通道 ID/名称/状态，不携带媒体源元数据。需要在 simulator 扩展层补足这个信息传播缺口。

对应路线图阶段：阶段 8（media-sources 媒体源统一抽象）& 阶段 9（cascade-and-multi-instance 级联路径）——本 change 解决「级联拓扑下媒体源如何跨节点可见」的问题。

## What Changes

- **Device 节点**：在响应平台 catalog 查询时，在 XML Item 中嵌入 `<MediaSource>` 自定义扩展段，包含 kind / path / loop / fps / clock 等媒体源元数据（仅 simulator 间互通，不影响标准解析器）
- **Platform 节点**：收到 device catalog 响应后，解析扩展段并将媒体源回填到平台本地通道级 `MediaConfig`（按 channel_id 匹配），使其在 HTTP API 和 Web UI 可见
- **降级兼容**：不发送扩展段的设备（真实设备）或解析失败时，平台保持原有行为（`has_media=false`），不报错
- **Catalog 触发**：platform 通过 `MESSAGE/Query` (MessageID: Catalog) 主动拉取 device 通道列表（标准 GB/T 28181-2016 A.2.4.1），收到后立即解析并回填

## Capabilities

### New Capabilities

- `cascade-media-source-propagation`（路径：`cascade-media-source-propagation`）：定义级联拓扑下媒体源配置如何通过 SIP catalog XML 扩展在节点间传播的行为契约。包括 device 端在 catalog Item 中嵌入扩展字段的规则、platform 端解析并回填通道级配置的规则、以及与标准设备的降级兼容规则。

### Modified Capabilities

- （无 spec 级行为变更。HTTP API 层 `channel-http-api` 的 `has_media` 回退逻辑已完整实现；本 change 解决的是 catalog → channel store 的上游数据填充问题，不改变 API 契约。）

## Non-goals

- 不实现通过 SIP INVITE / SDP 协商获取媒体源的机制
- 不实现 device 配置变更时主动发送 SIP NOTIFY 通知 platform 的机制
- 不实现 platform 通过 HTTP 反向查询 device 节点媒体配置的能力
- 不改变已实现的 `channel-http-api` 的 `has_media` fallback 逻辑（channel → node）
- 不改变 GB/T 28181 标准 SIP 消息格式（仅在 XML body 中扩展自定义段）
- 不支持真实设备（不支持扩展段的设备）的媒体源自动获取

## Impact

- **SIP 层**：`internal/sip/catalog.go`——device catalog 响应构造时注入自定义 XML 段；platform catalog 接收时解析并写回 store
- **Store 层**：`internal/store/channel.go`——已支持 `MediaConfig` 持久化，无需新接口
- **HTTP API 层**：无需改动，`has_media` fallback 逻辑（channel → node）已由 `fix-channel-media-fallback` change 实现
- **Web UI**：自动受益——通道详情页已支持显示 `has_media` / `media_kind`，无需额外改动
- **回归风险**：低。device catalog 增加扩展段对标准平台透明；platform 对无扩展段的 catalog 行为不变

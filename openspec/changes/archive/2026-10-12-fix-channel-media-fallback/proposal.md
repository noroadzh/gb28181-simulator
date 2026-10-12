# Proposal

## Why

platform-large 节点在 Web UI 通道详情页看到的 `has_media: false`，是一个 HTTP API 层的回归 bug：用户明明已在设备节点的 YAML 配置中为通道绑定了媒体源（本地文件 / RTSP / HLS），但进入通道页面后 UI 始终提示"媒体源未配置"。根因不在 catalog 同步（catalog XML 不携带媒体源），而在 `handleChannelList` 和 `handleChannelDetail` 只查询通道级 `MediaConfig`，当通道未单独配置时没有回退到节点级配置。`handleMediaFile` 早已实现了正确的 fallback，但列表和详情两个端点遗漏了。

## What Changes

- `handleChannelList`：对每个通道，查询通道级 `MediaConfig`；若未配置，则回退查询节点级 `MediaConfig`，将回退结果填入 `has_media` / `media_kind` 响应字段
- `handleChannelDetail`：同上，补充节点级 fallback 逻辑
- 单元测试：为上述两个场景补充"节点级媒体源 fallback"的单元测试
- spec 同步：`channel-http-api` 主 spec 中 `has_media` 的语义明确为"通道级优先 → 节点级回退"

## Capabilities

### New Capabilities

（无新能力引入）

### Modified Capabilities

- `channel-http-api`：更新 `has_media` / `media_kind` 响应字段的语义定义，明确回退行为为 MUST

## Impact

- 范围：仅 `internal/interface/http/channels.go` 的两处 handler 函数
- 对外契约：`GET /v1/nodes/:id/channels` 与 `GET /v1/nodes/:id/channels/:ch` 的响应中 `has_media` 语义扩展
- 无破坏性变更：不改变已有 channel 数据的结构，不影响 `handleMediaFile`（已有 fallback，不受影响）
- 无新依赖引入

## Non-goals

- 不修改 catalog 同步逻辑（catalog XML 不携带媒体源，非本 change 范畴）
- 不修改 `NodeService.GetChannelMedia` 的公共接口（仍只返回通道级配置）
- 不修改节点级媒体源配置加载逻辑（YAML → NodeProfile 的链路无需改动）

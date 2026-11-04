# Design

## Context

平台拉流会话登记在 `NodeService.live`（键为 `(nodeID, channelID)`）；HTTP 已有 POST live / live/stop 与 recordings 列表/FLV。Web 通道页目前假定本地 `has_media`，平台聚合通道常无本地源，直接点播放会误报「未配置媒体源」。

## Decisions

1. **按 `node.kind` 分流 UI**  
   `kind === 'device'`：保持现有 PTZ/对讲/本地播放。  
   `kind` 以 `platform` 开头：拉流条 + live FLV；隐藏 PTZ/对讲；录像页用 recordings API。

2. **`GET …/live`**  
   有活动会话 → `200` 同 start 响应体；无会话 → `404`。不创建会话。`session_id` 在 start 时写为 `live-<channelID>`（与既有 httptest 假实现一致），便于展示。

3. **播放语义（平台）**  
   「开始拉流」→ POST live；成功后可「开始播放」用 `flvUrl(..., {source:'live'})`。未拉流时播放按钮提示先拉流（不静默回落 local，避免误以为下级画面）。

4. **录像（平台）**  
   列表字段映射到现有表格；时长由 `end_time - start_time` 计算；播放仅挂 recording FLV，不 teardown playback INVITE。

5. **入口**  
   节点卡片「通道」按钮已对全 kind 可见，无需改 NodesView。

## Risks

- 刷新前未调 GET live → 会话仍在但 UI 显示未拉流；用 GET 缓解。
- 平台通道 `has_media=false` 时旧空态遮罩挡住播放器 → 平台分支去掉该遮罩。

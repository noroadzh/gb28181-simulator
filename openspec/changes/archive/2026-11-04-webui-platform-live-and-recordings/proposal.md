# Proposal

## Why

`#41`/`#42` 已交付平台侧实时拉流与录像库 HTTP API，但 Web UI 仍以设备通道（本地媒体源 / RecordInfo / PTZ / 对讲）为主；运维无法在平台节点上点选拉流、预览 live FLV、浏览平台录像库。路线图 `#47+` 将「Web UI 平台拉流/录像库入口」列为首个候选。

路线图编号：#47 `webui-platform-live-and-recordings`。

## What Changes

- 平台节点通道详情：拉流启动/停止（UDP/TCP）、会话信息展示、FLV 预览（`source=live`）；隐藏设备专属 PTZ/对讲面板。
- 平台节点录像页：走 `GET …/recordings` 与 `…/recordings/:id/flv`，不发设备侧 `POST /playback`。
- `api.js` 封装 live / recordings / flv `source` 查询参数。
- 可选只读 `GET …/live`：刷新后恢复会话感知（无会话 404）。
- 更新 `docs/web-ui-guide.md`、`HelpView`、路线图 #47。

## Capabilities

### New Capabilities

- `web-ui-platform-ops`：平台节点在 Web UI 上的拉流与录像库操作面。

### Modified Capabilities

- `channel-http-api`：增加 `GET /v1/nodes/:id/channels/:ch/live` 只读查询。
- `docs-roadmap-sync`：登记 #47 归档行；说明节更新 #48+ 候选。

## Impact

- 前端：`web/src/api.js`、`ChannelDetailView.vue`、`RecordView.vue`、`HelpView.vue`。
- 后端（小）：`GetLive` + HTTP GET；`StartLive` 填充 `session_id`（若为空）。
- 文档：`docs/web-ui-guide.md`、`docs/roadmap-15-steps.md`。

## Non-goals

- 级联中继会话列表（#48）、校时/媒体保活（#49）、SIP TLS/SRTP（#50）。
- 平台 UI 上的 PTZ/对讲面板、快照解码器、改动设备侧回放路径。

# Proposal

## Why

通道详情页点击"开始播放"后播放器黑屏，浏览器控制台报 `Uncaught TypeError: Class extends value undefined is not a constructor or null`；手动用 FLV 地址访问也无法播放。根因为两个独立故障叠加：flv.js Web Worker 在 Vite 5 bundler 下模块加载失败（阻止请求发出）+ FLV 拉流 URL 与后端路由不匹配（即使发出也会 404）。

## What Changes

- 修改 `web/src/api.js`：`flvUrl()` 方法由硬编码绝对 URL 改为相对路径，匹配后端实际路由 `/v1/flv/:nodeID/:channelID`。
- 修改 `web/src/views/ChannelDetailView.vue`：关闭 Web Worker（`enableWorker: false`），消除 flv.js 1.6.2 + Vite 5 组合下的模块继承链断裂。
- 修改 `web/src/views/RecordView.vue`：同样关闭 Web Worker，保持与通道直播播放器一致的行为。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

无。修复范围为前端播放器实现细节，不涉及后端路由或 spec 约定的 API 行为变更。

## Impact

- **前端**：`web/` 目录下 3 个文件的 4 处改动，改动量极小，无依赖引入。
- **后端**：无需改动。
- **路线图**：属于 Change 14（web-management-ui）的缺陷修复，不改变路线图编号。

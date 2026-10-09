# Tasks

## 1. 修复 FLV 拉流 URL

- [x] 1.1 修改 `web/src/api.js` 的 `flvUrl` 方法，将硬编码绝对 URL 改为相对路径 `/v1/flv/${encodeURIComponent(nodeId)}/${encodeURIComponent(ch)}`，验收：浏览器开发者工具 Network 面板中 FLV 请求发往正确后端端口与路径，返回 200 而非 404。

## 2. 关闭 Web Worker（直播播放器）

- [x] 2.1 修改 `web/src/views/ChannelDetailView.vue` 的 `startFlv` 函数，将 `flvjs.createPlayer` 第二个参数中的 `enableWorker: true` 改为 `enableWorker: false`，验收：控制台无 TypeError，播放器能收到 FLV 流。

## 3. 关闭 Web Worker（录像播放器）

- [x] 3.1 修改 `web/src/views/RecordView.vue` 的 `playRecord` 函数，将 `flvjs.createPlayer` 第二个参数中的 `enableWorker: true` 改为 `enableWorker: false`，验收：录像回放播放正常，控制台无 TypeError。

# Tasks

## 1. 后端：GET live

- [x] 1.1 `NodeService.GetLive`：有会话返回 `LiveSessionInfo`（补齐空 `session_id` 为 `live-<channel>`）；无会话返回 false。`StartLive` 成功时写入同一 `session_id`。验收：`go test ./internal/app -run Live -count=1` 相关用例通过
- [x] 1.2 `ChannelView` + `GET …/live` + httptest（200 / 404）；假实现同步。验收：`go test ./internal/interface/http -run Live -count=1`

## 2. 前端 API 与视图

- [x] 2.1 `api.js`：`startLive` / `stopLive` / `getLive` / `listRecordings` / `recordingFlvUrl`；`flvUrl` 支持 `{source}`。验收：方法拼出正确路径
- [x] 2.2 `ChannelDetailView`：加载节点 kind；平台拉流条 + live 播放；隐藏 PTZ/对讲；挂载时 GET live。验收：手工或构建无报错
- [x] 2.3 `RecordView`：平台走 recordings + recording FLV；设备保持原路径。验收：构建通过

## 3. 文档与校验

- [x] 3.1 更新 `docs/web-ui-guide.md`、`HelpView.vue`；路线图登记 #47（可在归档提交一并改）。验收：文档描述与 UI 一致
- [x] 3.2 `make test`（至少 http + app live 相关）与前端 `npm run build`（若可行）通过

## Workflow follow-up

- 归档时同步主 spec（`channel-http-api`、新建 `web-ui-platform-ops`、`docs-roadmap-sync`）并将说明节候选前移至 #48+

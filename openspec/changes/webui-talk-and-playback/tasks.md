# Tasks

## 1. 前端接线

- [x] 1.1 `web/src/utils/ulaw.js`：μ-law 编解码（G.711，8kHz）；验收：函数往返一致（注释内给出对拍样例），`pnpm build` 通过
- [x] 1.2 `web/src/api.js`：新增 `talkAudioUrl`、`startPlayback`、`stopPlayback`、`playbackFlvUrl`；验收：`pnpm build` 通过，URL 与后端路由逐字一致
- [ ] 1.3 `ChannelDetailView.vue`：开始/停止对讲的音频管线（getUserMedia → μ-law → WS；下行解码播放）；权限被拒显示错误且不进入对讲中；验收：`pnpm build` 通过 + 手工清单
- [ ] 1.4 `RecordView.vue`：点击条目 → `play`（带 sn/start/end）→ 用 `playback/flv?sn=` 播放；失败显示错误；离开/停止发 `teardown`；验收：`pnpm build` 通过 + 手工清单

## 2. 文档与收尾

- [ ] 2.1 `docs/web-ui-guide.md`：补 §5.3 对讲音频与 §5.4 回放播放的操作步骤、前提与失败码；验收：人工审阅
- [ ] 2.2 收尾：`cd web && pnpm build`、`make build`、`make test`、`openspec validate webui-talk-and-playback --strict --type change` 全部通过；验收：退出码 0

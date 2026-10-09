# Tasks

## Implementation

- [x] 1. 修改默认路由重定向 — `web/src/router/index.js` 将根路径从 `/nodes` 改为 `/dashboard`
- [x] 2. 修复账号列表字段名 — `web/src/views/AccountsView.vue` 将表格 `prop="Username"` / `prop="CreatedAt"` 改为 `prop="username"` / `prop="created_at"`
- [x] 3. 后端放开账号空密码校验 — `internal/interface/http/accounts.go` 移除 `handleAccountAdd` / `handleAccountSetPassword` 中密码非空校验；新增 `TestAddAccount_EmptyPassword` 测试用例
- [x] 4. 修复通道播放中断竞态 — `web/src/views/ChannelDetailView.vue` 改用 `METADATA_PARSED` 事件后再触发 `play()`，并吞掉 `AbortError`
- [x] 5. 优化通道上传入口 — `web/src/views/ChannelListView.vue` 将"媒体源"下拉拆分为独立的"媒体源" + "上传"两个按钮
- [x] 6. Device 节点账号页增加提示 — `web/src/views/AccountsView.vue` 当选中节点为 device 类型时显示明确的 el-alert 警告

## Verification

- [x] 7. 前端构建 — `cd web && npm run build` 成功，产物嵌入 Go 二进制
- [x] 8. 后端构建与测试 — `go build ./...` 与 `go test ./...` 全部通过
- [x] 9. openspec 严格校验 — `openspec validate --changes "ui-feedback-fixes" --strict` 无错误

## Deployment

- [x] 10. 构建 release 并部署 10.96.1.125 — 执行 `./release.sh` 与 `rsync`，重启容器后健康检查返回 ok
- [x] 11. git 提交与推送 — 提交代码并 `git push` 到 origin/main

## OpenSpec Archival

- [x] 12. 归档 change — `openspec archive "ui-feedback-fixes" --yes`，delta spec 同步到 `openspec/specs/`

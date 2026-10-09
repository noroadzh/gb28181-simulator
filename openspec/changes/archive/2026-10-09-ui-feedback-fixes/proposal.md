# Proposal

## Why

用户在实际使用中反馈了 Web 管理界面的 6 类 UX 问题：默认页应展示仪表盘、账号列表字段名错误导致显示 "undefined"、平台账号不支持空密码、通道播放时 flv.js play Promise 被意外中断、上传文件入口路径过深、设备节点进入账号页缺少提示。这些均为实现细节缺陷，不影响协议行为，但严重影响日常使用体验。

## What Changes

- **默认页改仪表盘**：`router/index.js` 根路径重定向从 `/nodes` 改为 `/dashboard`，打开页面即展示仪表盘
- **账号列表字段名修复**：`AccountsView.vue` 表格列 `prop="Username"` 改为 `prop="username"`（小写），消除 "undefined" 显示
- **支持空密码账号**：后端 `accounts.go` 放开密码非空硬校验，仅保留用户名非空与 20 位校验；前端表单密码字段改为非必填
- **修复播放中断竞态**：`ChannelDetailView.vue` 改用 flv.js `METADATA_PARSED` 事件后再触发 `play()`，避免 load→play 期间的 Promise 被 `pause()` 中断
- **优化上传入口**：通道卡片的"媒体源"按钮从嵌套下拉改为独立直达按钮，减少操作步骤
- **Device 节点账号页提示**：非 platform 节点进入账号管理页时，给出明确的"当前节点类型不支持账号管理"提示，而非空表格

## Capabilities

### New Capabilities

（无新能力引入。）

### Modified Capabilities

- `platform-account-management`：放开密码非空约束——`POST /v1/platforms/:id/accounts` 请求体中 `password` 字段允许为空字符串；`PUT .../password` 同理。username 非空与 20 位校验保持不变。
- `channel-web-ui`：修改以下已验收需求的行为细节：
  - 路由默认重定向至 `/dashboard`
  - AccountsView 账号列表字段名（消除 undefined）
  - 账号管理页对非 platform 节点给出提示
  - ChannelListView 上传按钮入口优化

## Impact

- **代码改动**：4 个前端文件（router、AccountsView、ChannelDetailView、ChannelListView）+ 1 个后端文件（accounts.go）
- **API 兼容性**：`POST /v1/platforms/:id/accounts` 放宽 password 约束为可选，属于向后兼容扩展
- **其他**：不影响 SIP 协议、媒体转发、数据库 schema

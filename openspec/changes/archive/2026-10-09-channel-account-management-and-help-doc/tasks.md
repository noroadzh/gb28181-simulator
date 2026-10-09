# Tasks

## 1. 存储层与模型

- [ ] 1.1 schema.sql 追加 platform_accounts / channels / channel_media / node_media 四张表（IF NOT EXISTS），验证：storage 单测通过、老库启动无损
- [ ] 1.2 `NodeProfile.WithChannelAdded` / `WithChannelRemoved` 不可变方法 + 单测（重复 id 报错、未知 id 报错、原 profile 不变），验证：`go test ./internal/domain/model/`
- [ ] 1.3 新建 `adapter/accountsql` sqlite 账号存储（实现 CredentialStore + AccountAdmin：Lookup/List/Add/Remove/SetPassword，互斥保护）+ 单测（seed 幂等、CRUD、并发、密码不落日志），验证：`go test ./internal/adapter/accountsql/`
- [ ] 1.4 新建 `app/account_service.go`（seed 逻辑：YAML → INSERT OR IGNORE；节点 kind 校验）+ 单测，验证：`go test ./internal/app/ -run Account`

## 2. 通道持久化服务

- [ ] 2.1 NodeService 新增 AddChannel / RemoveChannel：改内存 profile + 落 channels 表；同时将 SetChannelMedia/ClearChannelMedia/节点级媒体配置落 channel_media/node_media 表，验证：`go test ./internal/app/ -run Channel`
- [ ] 2.2 节点启动恢复：从 sqlite 读取动态通道与媒体配置，按"YAML 基线 + 库内叠加、同 id 库内优先"合并进 profile + 单测（含重启恢复场景），验证：`go test ./internal/app/ -run Persist`

## 3. HTTP 层

- [ ] 3.1 `interface/http/accounts.go`：GET/POST /v1/platforms/:id/accounts、DELETE .../accounts/:username、PUT .../accounts/:username/password（409/404/仅 platform-large 语义）+ handler 单测（fake AccountAdminView），验证：`go test ./internal/interface/http/ -run Account`
- [ ] 3.2 `interface/http/upload.go`：POST /v1/nodes/:id/media/upload（multipart 流式写盘、扩展名白名单、大小上限、Base 清洗、返回 path）+ 单测（合法上传/非法扩展名/超限/穿越名），验证：`go test ./internal/interface/http/ -run Upload`
- [ ] 3.3 ChannelView 追加 AddChannel/RemoveChannel，`channels.go` 新增 POST /v1/nodes/:id/channels（201/409/404）与 DELETE /v1/nodes/:id/channels/:ch（204/404）+ handler 单测，验证：`go test ./internal/interface/http/ -run Channel`
- [ ] 3.4 `server.go` NewServer 追加第 8 参 accounts（nil 时账号端点 501），批量更新 main.go 与全部测试调用点，验证：`go build ./... && go test ./internal/interface/http/`

## 4. 装配与集成

- [ ] 4.1 main.go 装配：platform 进程构造 accountsql store（seed YAML 账号）传入 NewServer；device 进程构造通道持久化 adapter；验证：`go build ./...` 通过
- [ ] 4.2 siptest e2e：新增"运行时新增通道后 Catalog 即时可见"用例（真 socket 双端），验证：`go test ./internal/adapter/siptest/ -run DynamicChannel`

## 5. Web 前端

- [ ] 5.1 api.js 扩展：listAccounts/addAccount/deleteAccount/setAccountPassword/addChannel/removeChannel/uploadMedia（FormData），验证：`cd web && npm run build` 通过
- [ ] 5.2 AccountsView.vue：节点选择器 + 账号表格（username/created_at/操作）+ 新增对话框（20 位编码校验、密码确认）+ 删除确认 + 改密码对话框，验证：`npm run build` 通过、手工走查增删改
- [ ] 5.3 ChannelListView.vue：新增通道按钮与对话框 + 通道卡片媒体源操作改造（手动输入 / 上传文件两种方式，上传成功自动回填路径），验证：`npm run build` 通过、手工走查
- [ ] 5.4 NodesView.vue platform 节点加"账号"入口按钮 + 侧边栏新增"账号管理""帮助"菜单项 + router 注册 accounts/help 路由，验证：`npm run build` 通过
- [ ] 5.5 HelpView.vue：锚点式帮助页（架构图 SVG + el-steps 对接四步流程 + 双 Web UI 分工 + 常见操作指南 + 已知限制说明），验证：`npm run build` 通过、页面渲染走查

## 6. 全量验证与文档

- [ ] 6.1 后端全量：`go build ./... && go vet ./... && go test ./...` 全绿
- [ ] 6.2 前端构建：`cd web && npm run build` 成功且产物 embed 更新
- [ ] 6.3 README 更新：账号管理、通道增删、文件上传、帮助页章节（中文），验证：内容完整
- [ ] 6.4 openspec validate --strict 通过

## 7. 交付

- [ ] 7.1 归档：`openspec archive channel-account-management-and-help-doc --yes`，验证：delta 同步到 openspec/specs/
- [ ] 7.2 构建 linux-amd64 release（.env 版本号同步 commit），rsync 到 root@10.96.1.125:/slow2/gb28181-simulator/，down.sh + deploy.sh，验证：platform 18080 / device 18081 健康检查通过
- [ ] 7.3 git 提交（main 分支）并尝试 push（失败则告知用户）

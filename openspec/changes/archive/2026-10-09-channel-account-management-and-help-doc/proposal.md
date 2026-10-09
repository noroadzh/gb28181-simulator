# Proposal

## Why

当前模拟器的运营管理能力存在三处明显缺口：① platform-large 的下级注册账号只能写在 `config.yaml` 里，重启之外无法增删，页面上完全没有管理入口，第三方设备对接时无法动态开通账号；② device 节点的通道列表只能来自静态配置，无法在运行时新增/删除子通道，也无法绑定 RTSP 地址或上传的 MP4 文件，导致"业务系统拉取设备列表"场景只能演示固定通道；③ 系统的定位与对接流程（REGISTER → CATALOG → INVITE → 拉流）只存在于口头解释中，新用户面对两个 Web UI（18080 平台 / 18081 设备）不知道从哪里入手。

本变更是路线图 #14 web-management-ui 的运营管理增强延续。

## What Changes

- **账号管理（sqlite 持久化）**：新增 `platform_accounts` SQLite 表；启动时把 YAML `platform.accounts` 幂等 seed 入库；新增 `port.AccountAdmin` 端口（List/Add/Remove/SetPassword）与 sqlite 适配器（同时实现 `port.CredentialStore`，acceptor 语义不变）；新增 HTTP API（列表/新增/删除/改密码）与 Web 账号管理页；运行时改动即时生效、重启不丢
- **动态通道管理**：`NodeProfile` 新增 `WithChannelAdded` / `WithChannelRemoved` 不可变方法；`NodeService` 新增 AddChannel / RemoveChannel；通道与通道媒体配置落 SQLite，重启恢复；Catalog 查询即时反映通道变化
- **媒体文件上传**：新增 `POST /v1/nodes/:id/media/upload`（multipart），白名单扩展名（mp4/ts/mkv/flv/h264/h265）、默认 2GB 上限、防目录穿越、流式写盘；返回容器内绝对路径，可直接填入节点级或通道级媒体源
- **节点级/通道级媒体源持久化**：`node_media` / `channel_media` 表落库，重启恢复（此前仅存于内存 profile）
- **Web UI**：新增 AccountsView（账号管理页）、HelpView（帮助文档页）；ChannelListView 增加"新增通道"对话框与上传绑定媒体源入口；NodesView 为 platform 节点增加"账号"入口按钮；侧边栏新增"账号管理"与"帮助"菜单
- **帮助文档**：Web UI 内置帮助页，讲清系统定位（平台 + 设备双角色）、业务系统对接全流程（REGISTER → CATALOG 拉设备列表 → INVITE 预览 → RTP/RTSP 拉流）、两个 Web UI 的使用分工、常见操作指南

## Capabilities

### New Capabilities

- `platform-account-management`：platform-large 节点下级注册账号的 SQLite 持久化、启动 seed、HTTP 管理 API（列表/新增/删除/改密码）、即时生效语义与密码保密约束
- `media-file-upload`：媒体文件 multipart 上传端点——扩展名白名单、大小上限、路径清洗、流式写盘、返回可绑定的容器内路径
- `runtime-state-persistence`：运行时状态（动态通道、通道媒体配置、节点级媒体源）落 SQLite 并在节点启动时恢复；YAML 静态配置与库内运行时状态的合并规则
- `help-documentation`：Web UI 内置帮助文档页，覆盖系统架构、业务系统对接流程（REGISTER/CATALOG/INVITE/拉流）、双 Web UI 分工与常见操作指南

### Modified Capabilities

- `multi-channel-devices`：新增"单个通道的运行时增删"需求（`WithChannelAdded` / `WithChannelRemoved`，不可变拷贝路径、重复/未知 id 报错）
- `channel-http-api`：新增 `POST /v1/nodes/:id/channels` 与 `DELETE /v1/nodes/:id/channels/:ch` 端点需求；账号管理端点归 `platform-account-management`
- `channel-web-ui`：新增"新增通道对话框""上传绑定媒体源入口""platform 节点账号入口按钮"需求

## Non-goals

- 不实现 Web UI 自身的登录鉴权体系（沿用 `auth-optional` 现状，可选开关不改动）
- 不实现账号的级联下发/上级平台账号同步（仅管理本平台下级账号）
- 不实现文件上传后的转码、抽帧、元信息提取（上传即落盘，媒体解析仍由现有 media-source 管道处理）
- 不实现通道/账号的多用户权限隔离、审计日志（单管理员模式）
- 不改动 SIP Digest 认证算法与 401/403 语义（`platform-large-node` 现有需求保持不变）
- 不实现 YAML → SQLite 的反向导出（配置文件仍是静态通道的声明来源）

## Impact

- **代码**：`internal/domain/port`（AccountAdmin）、`internal/domain/model`（WithChannelAdded/Removed）、`internal/storage/schema.sql`（新表）、`internal/adapter/accountsql`（新适配器）、`internal/app`（AccountService、NodeService 扩展）、`internal/interface/http`（accounts.go、upload.go、channels.go、server.go 签名变更）、`cmd/gb28181-simulator/main.go`（装配）、`web/src`（新页面 + 路由 + api.js）
- **API**：新增 6 个 HTTP 端点（账号 4 + 通道 2 + 上传 1，共 7）；`NewServer` 签名追加 accounts 依赖（**BREAKING**（内部接口）：所有测试调用点需同步更新）
- **存储**：SQLite 新增 4 张表（platform_accounts / channels / channel_media / node_media），schema 幂等追加，不破坏现有表
- **部署**：release/ 下 platform 与 device 容器的数据卷（`./platform/data`、`./device/data`）将持久化新表与 uploads 目录；无配置格式变更（YAML 账号仍兼容，作为 seed 来源）
- **文档**：README 增加账号管理/上传/帮助页说明

# Design

## Context

- 账号：`port.CredentialStore`（Lookup only，不可枚举）+ `adapter/credstore`（内存实现），账号仅从 YAML `platform.accounts` 在 main.go 装配时 `Add` 进 store；acceptor 直接消费该端口做 Digest 鉴权。重启即丢，无管理面。
- 通道：`NodeProfile.channels` 不可变切片，`WithChannels` 全量替换、`WithChannelStatus` 单通道改状态；运行时改动仅存在于内存 profile，重启丢失。`NodeService` 暴露 ListChannels/Channel/SetChannelMedia 等给 `ChannelView`。
- 存储：`internal/storage.Bootstrap`（modernc.org/sqlite，WAL，单连接，schema.sql 幂等）已就绪；platform 与 device 进程各自持有独立 db（容器内 `/var/lib/gb28181-simulator/`）。
- HTTP：echo；`NewServer(cfg, hub, ver, nodes, scenarios, channels, streaming)` 7 参；所有 handler 经窄接口（NodeView/ChannelView/ScenarioRunner）而非 *app.NodeService。
- 上传：项目无任何 multipart 处理；MediaConfig 的 local_file path 指容器内路径。
- 前端：Vue 3 + Element Plus，`web/src/views/*` + `api.js`，构建产物 embed 进二进制。

## Goals / Non-Goals

Goals：账号可管理且持久化；通道可运行时增删且持久化；媒体源（节点级/通道级）持久化；文件可上传绑定；帮助页讲清业务对接。

Non-Goals：见 proposal Non-goals（不做 UI 登录体系、转码、审计、级联账号、YAML 反向导出等）。

## Decisions

### D1 账号存储：sqlite 适配器 + 双端口实现，CredentialStore 接口不动

新增 `port.AccountAdmin`（List/Add/Remove/SetPassword），`adapter/accountsql.Store` 同时实现 `port.CredentialStore` 与 `port.AccountAdmin`。acceptor 侧零改动（仍拿 CredentialStore），管理面走 AccountAdmin。

- 备选 A：直接扩展 CredentialStore 增加 List/Add/Remove —— 被否：违反"不可枚举"的端口设计意图，acceptor 拿到多余能力。
- 备选 B：credstore 内存 store + 独立 sqlite 持久化钩子 —— 被否：两份数据源需双向同步，语义复杂；直接以 sqlite 为 source of truth 最简单。

Lookup 直接查 sqlite（单连接 + WAL，模拟器规模无性能压力），无需内存缓存层。

### D2 YAML 账号 seed 策略：幂等 INSERT OR IGNORE

启动时对 YAML 声明的账号执行 `INSERT OR IGNORE`：已存在（含运行时改过密码的）不覆盖。这样 YAML 退化为"初始种子"，运行时改动永远是权威状态。

### D3 通道持久化与合并：库内状态优先

新增表 `channels(node_id, channel_id, name, status, parent_id)`、`channel_media(node_id, channel_id, kind, path, loop, mtu, fps, clock)`、`node_media(node_id, kind, path, loop, mtu, fps, clock)`。

启动合并规则：**以 YAML 静态通道为基线，叠加 sqlite 动态通道；同 id 冲突时库内（运行时）优先**。删除的通道在库中物理删除行（记录"删除"本身，而非墓碑标记——YAML 基线在重启后重新叠加，这带来一个边界：删除了 YAML 声明的通道，重启后会复活）。

- 备选：墓碑表 `deleted_channels` 记录删除意图，启动时从合并结果中剔除 —— 功能更完整但多一张表与逻辑。**采纳简化版**：文档明示"删除 YAML 静态通道在重启后会复活"为已知限制（见 Risks）。运行时新增的通道（绝大多数场景）删除后正确消失。

### D4 NewServer 签名：追加 accounts 依赖

`NewServer(cfg, hub, ver, nodes, scenarios, channels, accounts, streaming)`——第 8 参 `accounts AccountAdminView`（窄接口：List/Add/Remove/SetPassword + 节点 kind 判断所需信息复用 nodes）。为 nil 时账号端点统一 501（与 channels nil 时回 501 的现有模式一致）。全部测试调用点同步补 nil 或 fake。

### D5 上传：echo 原生 multipart + 流式拷贝

`c.FormFile` 取句柄后 `io.Copy` 到目标文件（不整读内存）。文件名清洗 = `filepath.Base` + 扩展名白名单（mp4/ts/mkv/flv/h264/h265，可配置追加）；大小上限默认 2GB，经 `config.yaml` 新增可选 `media.upload_max_bytes` 覆盖。目标目录 `<数据目录>/uploads/`（即容器内 `/var/lib/gb28181-simulator/uploads/`，与 db 同卷，bind mount 持久化）。

### D6 账号 API 路径前缀 /v1/platforms/:id

不复用 `/v1/nodes/:id/...` 前缀：账号是 platform-large 专属概念，独立前缀让 404 语义天然成立（路径不匹配节点类型即 404），也避免 handler 内重复 kind 判断。handler 内仍需校验节点存在性（404）与 kind（404）。

### D7 帮助页：纯静态 Vue 组件，不引 markdown 依赖

HelpView.vue 内嵌结构化内容（el-steps + el-collapse + SVG 架构图），与现有视觉体系一致。理由：内容固定、无需运行时编辑，引 markdown 渲染器徒增体积。README 同步一份精简版说明。

## Risks / Trade-offs

- [删除 YAML 静态通道重启后复活] → 帮助页与 README 明示该限制；如需彻底删除，先在 YAML 移除再重启（模拟器场景可接受）
- [sqlite 单连接写竞争] → 上传/通道/账号操作低频，单写者模型足够；WAL 保证读不阻塞
- [NewServer 签名变更波及全部测试] → 一次性批量更新，先改 server.go 与 main.go，再逐个测试文件补参
- [上传大文件占用磁盘] → 2GB 上限 + uploads 目录与 db 同卷，运维可用 docker volume 配额控制；不做自动清理（Non-goal）
- [seed 不覆盖运行时改动导致"YAML 改密码不生效"困惑] → 帮助文档说明：账号以页面/库内为权威，YAML 仅首次种子

## Migration Plan

1. schema.sql 追加 4 张表（IF NOT EXISTS，老库无损升级）
2. 部署新二进制后首次启动：YAML 账号自动 seed 入库、既有内存态无迁移需求（原本就不持久化）
3. 回滚：旧二进制可正常读旧 schema（新表被忽略）；uploads 目录残留文件无害

## Open Questions

无——D3 的删除复活限制已作为已知限制文档化，不阻塞实施。

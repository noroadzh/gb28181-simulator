# Proposal

## Why

当前模拟器的节点定义仅以 YAML `nodes[]` 形式存在，部署后任何「调整三元组、新增节点、改变级联拓扑」都要改文件 → 重启进程。在对接真实用户平台（10.96.1.125 部署）的演练中，操作员频繁需要按真实平台回传的元数据校正本地配置；这种「改文件-重启-等再注册」的循环与现场节奏不匹配。`NodeService.Create` / `Registry.Unregister` 已经在 Go 层实现，但缺持久化面、HTTP 面和装配顺序，节点无法跨重启存活，也无 WebUI 入口。本 change 把「节点定义 + 注册目标 + 平台账号 + 级联拓扑」四块一并落到 SQLite，并通过 HTTP API 与 WebUI 暴露，让操作员在页面即可完成演练拓扑的全生命周期管理。

## What Changes

- **DB 节点表**：新增 `nodes` 表（id / kind / domain / addr / vendor / cascade_parent / cascade_children JSON / created_at / updated_at），加上 `node_registrations`（每个节点的注册目标）和 `node_platforms`（每个平台节点的账号与有效期），三表构成节点定义的完整真源。
- **YAML 角色降级**：`nodes[]` / `registration` / `platform` 段不再被装配代码消费。若配置文件中残留，组合根记录一条 `warn` 后跳过。配置文件中其它非节点段（http、log、capture、tracing、storage、auto_start、scenario）保持原行为。
- **装配顺序改造**：组合根顺序变为「开 SQLite → 跑 schema → 读 DB 全表 → 装配 NodeService」，让 DB 成为节点定义唯一真源。
- **HTTP API 新增**：`POST /v1/nodes`、`GET /v1/nodes/:id`、`PUT /v1/nodes/:id`、`DELETE /v1/nodes/:id`、`PUT /v1/nodes/:id/cascade`，并对 `POST /v1/nodes/:id/start` / `stop` 行为进行收敛（删除时要求已 stop）。
- **NodeService.Delete 编排**：新增 `Delete(ctx, id)` 方法，先校验 `status == stopped`、再 `Registry.Unregister`、再清 DB 行。Stop 失败时仍可被 Delete 调用方通过 `Unregister` 强清，但默认走两步路径。
- **级联拓扑 API**：把 `cascade_parent` / `cascade_children` 暴露到 `PUT /v1/nodes/:id/cascade`，调用 `NodeProfile.WithCascadeParent/Children` 后触发 `nodereg` 拓扑重算与环路检测；环路错误以 409 返回。
- **WebUI**：节点列表支持「新增/编辑/删除/编辑级联」，从 API 拉取并通过 WebSocket 实时刷新状态。

**BREAKING**

- `release/platform/config.yaml` 和 `release/device/config.yaml` 中的 `nodes` / `registration` / `platform` 段在升级后被忽略。需先在 WebUI 或通过 `POST /v1/nodes` 重建这些节点。
- `auto_start: true` 行为不变，但「自动启动的节点来源」从 YAML 切换为 DB；DB 为空时无任何节点被自动启动。

## Capabilities

### New Capabilities

- `runtime-node-storage`：节点定义、注册目标、平台账号、级联拓扑的持久化模型与 schema 迁移。`specs/runtime-node-storage/spec.md`。
- `runtime-node-http-api`：节点 CRUD + 级联设置的 HTTP API 契约（请求/响应体、状态码、错误码）。`specs/runtime-node-http-api/spec.md`。

### Modified Capabilities

- `node-abstraction`：装配顺序从「YAML `nodes[]` 优先」改为「DB 全表优先」，节点存在性判断从「YAML 解析期失败」改为「DB 读不到」。`specs/node-abstraction/spec.md` 增补 delta。
- `cascade-routing`：级联拓扑设置入口从「仅 `NodeProfile.WithCascadeParent/Children` 编程 API」扩展为「HTTP API + DB 持久化 + nodereg 拓扑重算」。`specs/cascade-routing/spec.md` 增补 delta。
- `node-lifecycle-controls`：节点删除要求先 stop，否则返回 409；`POST /v1/nodes/:id/start` 对来自 DB 的节点与来自编程 API 的节点行为一致。`specs/node-lifecycle-controls/spec.md` 增补 delta。
- `enterprise-skeleton`：组合根装配顺序引入 SQLite 引导（已在 Change 1 实现），本 change 将节点装配源从配置层切换到存储层，DB 空时不装载任何节点。`specs/enterprise-skeleton/spec.md` 增补 delta。

## Impact

- `cmd/gb28181-simulator/main.go`：装配代码改造为「先开 storage，再读 DB 装配 NodeService，最后启动 HTTP/WS」。`platform/config.yaml` 节点的解析路径移除。
- `internal/platform/config/config.go`：`NodeConfig` 解析路径移除或标记 deprecated。`ValidateNodes` 行为改变：仍校验但不再驱动装配。
- `internal/app/node_service.go`：新增 `Delete`、`UpdateProfile` 公开方法；`Create` 改为同时落 DB。
- `internal/adapter/nodereg/registry.go`：现有 `Register` / `Unregister` / `MutateProfile` 复用，不动公开签名。
- `internal/adapter/nodesql/`：新增适配器包，封装 SQLite 的节点 / 注册 / 平台 / 级联表 CRUD。
- `internal/interface/http/`：新增 `node_crud.go`、`node_cascade.go`，并对 `server.go` 增加路由。
- `web/src/views/`：节点列表组件补新增/编辑/级联表单。
- 部署：`release/{platform,device}/config.yaml` 在升级说明中标注已废弃对应段；新部署提供「空白启动 + 页面配节点」的引导。

## Non-goals

- **不**支持通过 WebUI 重命名节点 ID（20 位编码一旦写入就是身份主键，跨节点持久化重命名会污染级联图与会话历史）。
- **不**把场景（scenarios）、抓包（capture）、审计（audit）配置纳入本次存储范围——这些是独立子系统的真源，不与节点定义耦合。
- **不**把 WebUI 的「节点表单」做成所见即所得的 YAML 转换器——它只暴露 DB 列，不做 YAML/JSON 互转。
- **不**实现节点模板库、节点克隆、节点导入/导出批量操作——单节点 CRUD 已覆盖演练场景，批量能力留待后续 change。
- **不**支持跨进程同步节点 DB——多副本部署是另一个 change 的范畴；本 change 假设单进程单 DB 文件。

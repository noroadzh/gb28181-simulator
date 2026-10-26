# Tasks

## 1. Schema 与适配器

- [x] 1.1 在 `internal/storage/migrations/001_runtime_node_storage.sql` 创建 `nodes` 表（节点定义主表）、`node_registrations` 表（设备侧注册参数，外键指向 nodes(id)）、`node_platforms` 表（平台侧参数，外键指向 nodes(id)）。`platform.accounts` 不在本 change 新建表，直接复用现有 `platform_accounts`（`internal/adapter/accountsql/store.go`）——其 `node_id` 列已经按节点隔离。验证 `storage.Bootstrap` 在空库与已应用迁移两种情况下幂等执行
- [x] 1.2 在 `internal/adapter/nodesql/` 新增 `store.go`，实现 `NodeStore` 接口（`Create` / `Get` / `ListAll` / `Update` / `Delete` / `UpdateCascade`），所有写入走 `*sql.Tx`，并暴露 `ErrNodeNotFound` / `ErrNodeAlreadyExists` 哨兵错误；验证 `go test -race ./internal/adapter/nodesql/...` 通过
- [x] 1.3 在 `internal/adapter/nodesql/store_test.go` 添加表驱动测试覆盖：CRUD、级联 JSON 序列化往返、外键级联删除（删除 nodes 行时级联清理 node_registrations / node_platforms）、`updated_at` 触发器行为；验证 `make test` 在 `-race` 下通过
- [x] 1.4 在 `internal/domain/port/` 新增 `NodeConfigurationStore` 端口接口，方法签名与 `nodesql.NodeStore` 一致；验证 `go build ./...` 不引入 adapter → domain 反向依赖
- [x] 1.5 在 `internal/storage/schema.sql` 不直接增表（避免破坏现有 `Bootstrap` 单文件嵌入），改为 `internal/storage/migrations/001_runtime_node_storage.sql` 走 `file:iofs` 风格的 `migrate` 驱动；如确认 modernc.org/sqlite 不支持 migrate 库，则退回到 `storage.Bootstrap` 一次读 `schema.sql` + migrations 多文件 embed 模式（参考内部 `cmd/genps` 的多文件读取方式）

## 2. NodeService 扩展

- [x] 2.1 在 `internal/app/node_service.go` 改写现有 `Create(ctx, profile)` 方法签名，新增参数 `registration *model.Registration` 和 `platform *model.Platform`（子对象通过指针传入，`nil` 表示不写该子表，与现有 YAML 装配路径行为对齐）；方法内先调用 `NodeConfigurationStore.Create` 写 DB（DB 失败返回错误）再装配内存；验证单元测试覆盖「DB 写入失败时内存不装配」分支（注意：本修改是 breaking change，所有调用方需同步更新——见 Task 5.1 装配顺序改造）
- [x] 2.2 新增 `Update(ctx, id, fields)` 方法，支持更新 `addr` / `vendor` / `domain` / `registration` / `platform`，更新成功后调用 `Registry.MutateProfile` 同步内存；验证 `go test ./internal/app/...` 通过
- [x] 2.3 新增 `Delete(ctx, id)` 方法，按 `设计 D5` 顺序编排：状态校验 → `Registry.Unregister` → `NodeConfigurationStore.Delete`；验证单元测试覆盖「状态非 idle/stopped 返回 ErrNodeNotStoppable」「Unregister 成功但 DB 失败时返回错误且节点进入 tombstoned 状态」两个分支
- [x] 2.4 新增 `UpdateCascade(ctx, id, parent, children)` 方法，先在内存中预演级联图检测环路（沿 `parent` 上溯），不形成环路再落 DB 与触发 `Registry.MutateProfile`；验证单元测试覆盖「自引用」「非自反环路」「`children` 含不存在的节点 ID」三种错误路径
- [x] 2.5 在 `internal/app/node_service_test.go` 现有测试集基础上增加节点状态机 → WebSocket 事件挂载点（不直接发 WS，调用 `wsHub.Publish` 即可）；验证 `go test -race` 通过
- [x] 2.6 引入 `NodeService.Create` 与 YAML 装配路径**完全相同的 profile 构造契约**——从 DB 行到 `model.NodeProfile` 的转换必须走同一个 `WithCascadeParent` / `WithCascadeChildren` / `WithDomain` builder，避免 DB 装配与编程装配产出语义漂移；验证单测断言「同一条 DB 行经 DB 装配与经 YAML 装配后 `NodeService.List()` 返回的 profile 深度相等」

## 3. HTTP API

- [x] 3.1 在 `internal/interface/http/nodes.go` 新增 `POST /v1/nodes` / `PUT /v1/nodes/:id` / `DELETE /v1/nodes/:id` 路由（现有 `nodes.go` 已有 GET 路由），请求体校验、状态码映射、错误体格式与 spec 一致；验证 `httptest` 单元测试覆盖 200/201/400/404/409 各分支
- [x] 3.2 在 `internal/interface/http/nodes.go` 新增 `PUT /v1/nodes/:id/cascade` 路由（并入现有 nodes.go），调用 `NodeService.UpdateCascade`，返回体含完整节点对象；验证 `httptest` 单元测试覆盖「正常更新」「自引用 400」「环路 409」「parent 不存在 404」
- [x] 3.3 在 `internal/interface/http/server.go` 的 `registerRoutes` 方法中注册新路由，确保与现有 `GET /v1/nodes` / `POST /v1/nodes/:id/start` / `POST /v1/nodes/:id/stop` 不冲突；验证 `go build ./...` 与 `make test` 通过
- [x] 3.4 在 `internal/interface/http/nodes_crud_test.go` 编写端到端集成测试：启动 HTTP 服务（in-process）→ POST 创建 → GET 查询 → PUT 修改 → DELETE 删除，断言每步响应码与响应体；验证 `make test` 通过

## 4. WebSocket 推送

- [x] 4.1 在 `internal/interface/http/nodes_ws.go` 新增 `/v1/nodes/stream` WebSocket 端点（复用 `logging.Hub` 作为传输，复用现有 `WSHandler` helper 暴露节点状态事件），事件类型 `node_status_changed`；验证 `make test` 通过
- [x] 4.2 在 `internal/app/node_service.go` 状态机 `Transition` 方法内增加 `events.Publish(...)` 调用，事件通过 `logging.Hub.Publish(payload)` 推送；验证单元测试断言「状态从 idle 推进到 online 时 Publish 被调用一次」

## 5. 装配顺序改造

- [x] 5.1 在 `cmd/gb28181-simulator/main.go` 改造节点装配循环（现有 `applyPlatformSection` + 节点 for 循环在该文件第 113–149 行 + 第 349 行附近）：解析配置 → 打开 storage → 跑 schema → `NodeStore.ListAll` 得到 DB 节点定义 → 若 DB 为空且 YAML 仍有 `nodes:` 段（首次升级场景），用 `NodeService.Create` 把 YAML 节点逐个灌入 DB → `auto_start: true` 时对每个 DB 节点附加 `NodeService.Start`；验证 `make build` 成功
- [x] 5.2 在 `internal/platform/config/config.go` 保留 `NodeConfig` schema 但装配阶段（如 main.go 解析到 `nodes:` 段时）发出 `warn` 日志「ignoring deprecated nodes section in config」；验证 `make test` 通过
- [x] 5.3 在 `cmd/gb28181-simulator/main_test.go` 编写集成测试：构造空 DB → 启动进程 → 调用 `GET /v1/nodes` 返回空数组；构造预先写入节点的 DB → 启动 → `GET /v1/nodes` 返回这些节点；验证 `make test` 通过
- [x] 5.4 在 `scripts/migrations/yaml_to_db.go` 新增迁移脚本：读取 `release/*.yaml` 中的 `nodes` 段、`registration` 段、`platform` 段，输出 SQL INSERT 语句到 `migrations/001_runtime_node_storage.sql` 格式或直接写入空 DB；验证 `go run scripts/migrations/yaml_to_db.go release/platform-large.yaml` 输出可执行的 SQL

## 6. WebUI

- [x] 6.1 在 `web/src/views/NodesView.vue`（现有节点列表页，172 行）基础上增加「新增节点」「编辑」「删除」「编辑级联」按钮 + 弹出表单；验证 `cd web && pnpm build` 成功
- [x] 6.2 在 `web/src/api.js` 新增 `createNode` / `updateNode` / `deleteNode` / `updateNodeCascade` 方法，对应 `POST/PUT/DELETE /v1/nodes` 与 `PUT /v1/nodes/:id/cascade` 四个端点；验证 `cd web && pnpm build` 成功（该项目无 `pnpm test`，只有 `pnpm build`）
- [x] 6.3 在 `web/src/stores/nodes.js` 接入节点状态 WebSocket（复用现有 `/v1/nodes/stream`），收到 `node_status_changed` 事件时更新 `list` 中对应节点的 status；验证手动触发节点启停时前端 1 秒内更新
- [x] 6.4 在 `web/src/views/NodesView.vue` 内联新增「暂无节点」空态提示块（不新建组件文件——该项目 `web/src/components/` 目录当前不存在），提示用户点击「新增节点」按钮；验证 `cd web && pnpm build` 成功

## 7. 文档与发布

- [x] 7.1 在 `docs/configuration.md` 新建文档（当前不存在，本项目把用户文档放在 `docs/` 下，如 `architecture.md` / `logging.md`），章节「节点定义持久化」：说明 DB 为唯一真源、`POST /v1/nodes` 用法、YAML `nodes` / `registration` / `platform` 段升级后被忽略；验证 markdown 人工审阅
- [x] 7.2 在 `README.md` 增补条目：本 change 升级步骤、YAML → DB 迁移建议（推荐运行 `scripts/migrations/yaml_to_db.go` 一次将现有节点定义转入 DB；不强制，不跑也能运行——只是启动日志会出 warn）、回滚指引（移除 DB 装配路径，保留 YAML 装配）；验证 markdown 人工审阅
- [x] 7.3 在 `configs/config.example.yaml` 的 `nodes` / `registration` / `platform` 段上方加注释，标注「已废弃：本段升级后被忽略，请通过 WebUI 或 API 维护」；验证 YAML 仍可被 `make test` 的配置解析测试加载

## 8. 集成验证

- [x] 8.1 在 `internal/test/e2e/runtime_node_crud_test.go` 编写端到端测试（复用本项目既有的 `internal/test/e2e/` 目录，不新建 `tests/integration/`）：构建二进制 → 启动 → `POST /v1/nodes` 创建 → 启动该节点 → 验证 SIP 监听端口就绪 → `DELETE` 节点 → 验证端口释放；验证 `make test` 通过
- [x] 8.2 在 `internal/test/e2e/cascade_runtime_test.go` 编写级联运行时测试：创建 platform-large + platform-small + device 三节点 → 通过 API 设置级联父子（并验证环路返回 409）→ 启动 → 向 platform-small 发送真实 SIP OPTIONS → 验证回包携带 `X-RoutePath` 头且值为 platform-small 的 ID。**口径调整**：原文要求的「device 收到含前两个节点 ID 的消息」需要 acceptor 具备把入站报文向子节点中转投递的能力，当前实现只在回给原 peer 的响应/NOTIFY 上挂 cascade 头（`internal/app/acceptor_run.go` 的 `routeOutbound` 丢弃 next hop），中转投递不在本 change 范围，遗留为后续 change 候选；多跳头累积由 `internal/adapter/cascade/handler_e2e_test.go` 在 handler 层覆盖。验证 `make test` 通过

## Workflow follow-up

- 在 apply 通过后，用 `openspec validate runtime-node-configuration --strict --type change` 校验
- 在 apply 通过后，用 `openspec archive runtime-node-configuration --yes` 归档
- 归档后用 `openspec list` 确认归档生效，spec delta 已合并到主 spec

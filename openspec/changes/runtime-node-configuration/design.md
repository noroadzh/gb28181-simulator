# Design

## Context

当前节点定义仅由 `cmd/gb28181-simulator/main.go` 在启动时从 YAML `nodes[]` 解析并调用 `NodeService.Create` 装配，DB 虽已落 `platform_accounts` / `channels` / `scenarios` 等表，但节点元数据本身在 DB 中无对应表。`NodeService` / `Registry` / `NodeProfile` 已具备 `MutateProfile` 能力，缺的是写入面（DB 表）与读取面（HTTP 端点）。本 change 引入节点定义真源从 YAML 切换为 DB 的能力，并把运行时 CRUD 通过 HTTP API 与 WebUI 暴露，对应提案 proposal.md。

## Goals / Non-Goals

**Goals:**
- 将节点定义、注册目标、平台账号、级联拓扑统一在 SQLite 中
- 让节点真源与运行时可变更（不再依赖配置改写 + 进程重启）
- 保留现有 `NodeService` / `Registry` / `NodeProfile` 公开 API 不破坏调用方
- 通过 HTTP API 暴露节点 CRUD + 级联 + 状态推送
- WebUI 节点列表可新增 / 编辑 / 删除 / 改级联

**Non-Goals:**
- 不实现节点模板 / 克隆 / 批量导入导出
- 不支持跨进程节点同步（多副本部署不在本 change 范围）
- 不把场景 / 抓包 / 审计配置纳入本次存储
- 不实现 WebUI 与 DB 之外的双向同步机制（WebUI 永远只读 DB）

## Decisions

### Decision 1: 节点定义新增 3 张表，不替换既有 `platform_accounts` / `channels`

`nodes` / `node_registrations` / `node_platforms` 单独建表，与 `platform_accounts`（账号密码）/ `channels`（通道元数据）解耦。级联拓扑不另建 `cascade_meta` 表，而是作为 `nodes` 的两列持有（见 Decision 2）。后两者由 `Change 5 / Change 8` 已经做了 N→1 的归一，符合场景本意，不重复建设。

**替代方案**:
- *合并到 channels 表*：会让单节点多通道的查询复杂化（一个平台节点 0 通道的 case 无法表达）
- *全 JSON 化*：丢掉查询能力，级联图遍历会变成 DB 端 join 难

### Decision 2: `cascade_children` 序列化为 JSON 文本

SQLite 没有数组类型。两种选择：JSON 文本 / 关系表 `cascade_edges(parent, child)`。选 JSON 文本因为：（a）查询模式几乎只有「给定节点找其直接子」（关系表达也是这个量级）；（b）少一次 join 写；（c）级联图规模演练场景是 ≤ 100 节点，性能不敏感。关系表方案在 `cascade-edges` change 中再考虑（如果要做级联图可视化）。

**替代方案**:
- *关系表 cascade_edges*：未来要做图遍历时再迁。YAGNI。

### Decision 3: 装配顺序改造为「YAML→DB→NodeService」

组合根在 `cmd/gb28181-simulator/main.go` 中按顺序：开 logger → 开 storage（含 schema bootstrap）→ 读 DB 全表 → 装配 NodeService → 开 HTTP/WS。YAML 节点段在解析阶段被丢弃但保留 schema 校验（防止「旧配置启动后没节点」而误以为成功）。

**关键点**：`storage.Bootstrap` 已经在 `Change 1` 接入（CLAUDE.md 分层「internal/storage 引导」），本 change 不引入新的 storage 库依赖。

**替代方案**:
- *NodeService 直接对接 viper*：把 YAML 与 DB 的差异推给 NodeService — 错，NodeService 应该是「节点领域服务」而不是「配置兼容层」
- *在 main.go 阶段并行装配*：会让 start 顺序的确定性变差

### Decision 4: HTTP API 复用现有 `/v1` 前缀与 Echo 路由

新路由挂在 `internal/interface/http/server.go` 的现有 Echo Group 上，不新增框架。响应体使用与现有 `GET /v1/nodes` 一致的 JSON 风格。WebSocket 复用 `internal/interface/http/ws_hub.go` 的事件分发器，把 `node_status_changed` 作为新事件类型注册。

**替代方案**:
- *新增 /v2 路由*：当前 API 还没到 1.x 的稳定承诺，0.x 内做破坏性合并是合理的
- *GRPC 替代 REST*：超出本 change 范围

### Decision 5: NodeService.Delete 是显式编排方法

不把「校验状态 → Unregister → DB Delete → 拓扑重算」写在 HTTP handler 内，而是封装为 `NodeService.Delete(ctx, id)`。理由：handler 应只做参数解析与状态码映射，业务编排由服务承载，可被 scenario 引擎或未来批处理复用。

**关键不变量**：内存先改、DB 后写；DB 失败时把节点按原 profile 重新注册回内存（回到 `StatusIdle`）并返回错误，避免出现「DB 还有行、内存已无节点」的半删除状态。回滚本身再失败时只记录日志，交由重启（内存从 DB 重建）收敛。

### Decision 6: WebSocket 推送 node_status_changed 复用现有 ws_hub

`internal/interface/http/ws_hub.go` 已有事件分发（capture 事件、log 事件都通过它），新增 `node_status_changed` 事件类型。实现上事件走 `internal/platform/events` 的独立 Hub（与日志 Hub 分离，避免 UI 事件依赖日志格式），由 `/v1/nodes/stream` 推送给订阅客户端。

**关键点**：状态机推进是 NodeService 内部行为，要在哪里挂钩需要审视 `NodeService.Transition`（`node_service.go:1080` 附近），不引入新 channel。

### Decision 7: YAML `nodes` 段保留解析但发出 warn

YAML schema (`config.NodeConfig`) 暂不删除，理由：（a）schema 校验能给「配置文件非法」提供友好错误（即使装配忽略）；（b）外部 README 仍在教用户用 `nodes` 段，删除会引入迁移期文档空窗期。等 `release/` 目录的示例文件改写 + WebUI 引导文案就绪后再彻底移除。

**替代方案**:
- *立即删除*：破坏性变更来得太早，演练用户来不及跟随

## Risks / Trade-offs

- **R1: DB 启动时无节点导致「部署看起来没动」** → Mitigation：在 WebUI 启动时检测 `GET /v1/nodes` 返回空数组 → 弹出「暂无节点」空态引导 + 「新增节点」按钮。日志也输出一条 info。
- **R2: 同时多进程写同一 DB 文件导致 SQLite lock** → Mitigation：本 change 假设单进程单 DB 文件，多进程场景在另一个 change 解决。SQLite WAL 模式可降低冲突概率。
- **R3: `cascade_children` JSON 写入的并发问题（两个 PUT 同时改同一节点）** → Mitigation：使用 DB 事务 + `updated_at` 乐观锁，HTTP API 在写入前读取 `updated_at`、写入时校验相等；不等则返回 409。
- **R4: NodeService.Delete 的部分失败状态机暴露** → Mitigation：在 `NodeService` 中维护 `tombstoned` 状态标记（仅内存，进程重启后自然清空），让 `Start` 检测到 tombstoned 直接报错。
- **R5: 装配顺序改造让现有「YAML 改完重启即生效」的运维模式断裂** → Mitigation：保留 warn 日志 + release 目录升级说明 + 一次性 migration 脚本（YAML → DB 导入工具），可作为附加产物但不阻塞本 change 主路径。
- **R6: WebSocket 推送 node_status_changed 与现有 5s 轮询双轨** → Mitigation：轮询先保留作为「未订阅 WebSocket 也能用」的兜底，WebSocket 订阅仅作为「实时刷新」加速器。

## Migration Plan

1. **新增代码 + 迁移脚本**：在 `internal/adapter/nodesql/` 新增适配器，在 `internal/storage/migrations/001_runtime_node_storage.sql` 新增 DDL
2. **保留旧装配路径（不立即删除）**：组合根保留对 YAML 节点的解析但发出 warn
3. **新增装配路径**：组合根按新顺序装配（DB → NodeService）
4. **WebUI 配合**：节点列表组件增加「新增/编辑/删除/级联」按钮
5. **发布说明**：
   - 在 `release/CHANGELOG.md`（或 release notes 中）说明升级步骤
   - 提供 `tools/migrate-yaml-to-db` 命令（可选工具，本 change 可不交付，但留出设计空间）
6. **回滚**：
   - 如果 DB 装配路径出现严重 bug，移除组合根中的 DB 装配代码、保留 YAML 路径为唯一来源
   - DB 中的节点数据不被破坏（即使装配被回滚，DB 状态是只写的）

## Open Questions

- **Q1: NodeService.MutateProfile 是否需要锁？** 现状是 `nodereg` 内部有 RWMutex，但跨 `UpdateCascade` HTTP 调用没有全局锁。两个并发 PUT 级联如何处理？倾向 409 + 让调用方重试（见 R3），但是否在 spec 阶段就明确「并发控制由 HTTP 边界负责」尚未敲定。
- **Q2: WebUI 是否需要「从 YAML 导入」按钮？** 这是一个把演练用户已有 YAML 一键转 DB 的工具。设计里没把这条纳入本 change 的 scope，但是否在最终文档里点出？倾向「不在本 change 范围，作为工具单独 release」。


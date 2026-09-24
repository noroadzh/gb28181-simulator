# Tasks

> 路线图阶段 4 `node-abstraction`：Node 模型（身份/状态机/实例化/多节点共存）。
> 任务粒度 ≤ 2 小时；每任务含明确验收标准；涉及 SIP 字节格式的任务必须含 golden test。
> 承接 `enterprise-skeleton` 归档遗留：**Q4**（transport 对端地址）与 **§11.4**（双进程 INVITE/200 OK）。
>
> Open Questions 处理（见 `design.md`）：Q1 持久化按 Non-Goal 处理（注册表内存态，不建表）；
> Q2 AuditSink 事件本 change 不实现（spec 未要求），节点生命周期事件留 Change 13；
> Q3 多节点共享一个 `Clock`（实现细节，不改变外部行为）。

## 1. Domain：Node 身份值对象

- [x] 1.1 在 `internal/domain/model/node.go` 定义 `NodeID`（20 位 GB/T 28181 编码）、`NodeKind`
      （`device` / `platform-large` / `platform-small`）、`NodeProfile`（身份 + 信令地址 + 归属域 + 厂家）、
      `Node`；沿用既有不可变值对象约定（构造后修改 panic，改值走 `With...`）
      - 验收：`go test ./internal/domain/model -run Node` 通过；`NodeID.String()` 返回原编码
- [x] 1.2 实现 `ParseNodeID`：长度必须 20 且全数字；类型编码段（第 11–13 位）映射 `NodeKind`；
      无法映射时返回错误，不做猜测式默认
      - 验收：合法/长度错/含非数字/类型段不匹配 四类用例（表驱动）通过；错误含原因与输入长度
- [x] 1.3 不可变性：修改 `NodeProfile` / `Node` 字段编译失败或 panic，与 `model.Message` 一致
      - 验收：不可变性测试通过（参照 `credentials_test.go` 的既有写法）

## 2. Domain：状态机

- [x] 2.1 在 `internal/domain/model/node_status.go` 定义状态枚举
      `Idle / Registering / Registered / Online / Offline / Fault` 与哨兵错误 `ErrIllegalTransition`
      - 验收：`errors.Is(err, ErrIllegalTransition)` 可判定
- [x] 2.2 用 `map[Status]map[Status]bool` 表达合法迁移表；非法迁移返回 `ErrIllegalTransition`
      **且不改变当前状态、不 panic**
      - 验收：6×6 全覆盖表驱动测试通过（含 Idle→Online 非法、Fault→Online 非法、Fault→Idle 合法）
- [x] 2.3 `Fault` 为可恢复终态：仅允许迁移回 `Idle`（重置）或 `Offline`（摘除）
      - 验收：对应迁移用例通过

## 3. Domain：端口（含 Q4 承接）

- [x] 3.1 在 `internal/domain/port/node.go` 新增 `NodeRegistry`（Register/Unregister/Get/List）
      与 `NodeLifecycle`（Start/Stop/Status）两个端口接口，参数类型全部来自 `domain/model`
      - 验收：编译通过；接口方法无 adapter 类型
- [x] 3.2 **改 `port.SIPTransport` 签名对齐归档 spec**（`design.md` D4，Q4 的答案）：
      `Send(ctx, msg, dst string) error`、`Receive(ctx) (model.Message, string, error)`
      - 验收：`internal/domain/port/transport.go` 编译通过；签名与
        `openspec/specs/enterprise-skeleton/spec.md` 「Domain ports」描述逐字一致
- [x] 3.3 验证 domain 层仍零外部依赖
      - 验收：`go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...`
        仅输出 `internal/domain/*`

## 4. Adapter：siptransport 对端地址（承接 Q4）

- [x] 4.1 接收循环保留远端地址：UDP 取 `ReadFromUDP` 返回地址，TCP 取 `conn.RemoteAddr()`
      - 验收：新增测试断言 `Receive` 返回的地址等于发送方 `host:port`
- [x] 4.2 `Send` 使用显式 `dst` 参数，不再从报文 URI 反推目标
      - 验收：发往指定地址的测试通过；传空 `dst` 返回错误
- [x] 4.3 无法确定对端地址时**返回错误**，不返回空串
      - 验收：错误路径用例通过（spec 要求显式报错）
- [x] 4.4 编译期断言仍生效
      - 验收：`var _ port.SIPTransport = (*PortAdapter)(nil)` 存在且编译通过；
        `go test ./internal/adapter/siptransport` 全绿

## 5. Adapter：节点注册表

- [x] 5.1 新建 `internal/adapter/nodereg/`，实现 `port.NodeRegistry`：
      `map[string]*entry` + `sync.RWMutex`
      - 验收：`var _ port.NodeRegistry = (*Registry)(nil)` 编译通过
- [x] 5.2 重复 id 注册返回错误且不覆盖既有节点
      - 验收：重复注册用例返回错误；原节点不变
- [x] 5.3 并发安全：多 goroutine 并发注册/注销/查找
      - 验收：`go test -race ./internal/adapter/nodereg` 无数据竞争；无重复 id
- [x] 5.4 注册前校验地址唯一，冲突返回含节点 id 的明确错误（不静默绑随机端口）
      - 验收：同地址二次注册用例通过，错误信息含冲突节点 id
- [x] 5.5 每个节点拥有**按 node_id 打点的独立日志字段**（spec R3 明文要求，design/tasks
      原先未分解）：注册/注销、listener 绑定与释放、状态迁移（含被拒绝的非法迁移）
      均输出带 `node_id` 的日志，两个节点的记录互不串扰
      - 验收：注入 `*slog.Logger` 的测试断言每条记录含本节点 `node_id=` 且不出现另一节点 id；
        `nodereg.New()` 默认走进程 logger

## 6. App：NodeService 用例编排

- [x] 6.1 在 `internal/app/node_service.go` 实现 `NodeService`，构造函数只接受 domain 端口
      （`NodeRegistry` / `NodeLifecycle` / `NodeAdvancer` / `SIPTransport` 工厂 / `Clock`）
      - 验收：编译通过；文件不 import `internal/adapter/...`
- [x] 6.2 实现 Create / Start / Stop / List 四个用例方法
      - 验收：各方法单元测试通过；`Start` 后状态为 `Registering`（design D9）
- [x] 6.3 启动失败回滚：transport 绑定失败时返回错误、释放端口、状态回 `Idle`/`Fault`，不残留半启动
      - 验收：端口占用失败用例通过，端口可立即重新绑定
- [x] 6.4 提供显式推进方法（`MarkRegistered` / `MarkOnline`）供 Change 5+ 与测试调用，
      `Start` 自身不自动推进到 Registered/Online
      - 验收：推进方法测试通过；`Start` 后状态为 `Registering` 而非 `Online`
- [x] 6.5 验证 app 层不依赖 adapter
      - 验收：`go list -deps ./internal/app/...` 筛去标准库后仅含 `internal/domain/...`
        与 `internal/platform/...`

## 7. Platform：节点配置

- [x] 7.1 `internal/platform/config` 新增可选 `nodes:` 列表结构（每项 `id` / `kind` / `domain` / `addr`）
      - 验收：配置解析测试通过；`nodes` 缺省时为空切片而非 nil panic
- [x] 7.2 缺省零节点：配置不含 `nodes:` 时进程行为与本 change 之前完全一致
      - 验收：用现有 `configs/config.example.yaml` 启动，HTTP/日志行为无变化
- [x] 7.3 非法条目（id 长度 ≠ 20、kind 不在枚举）导致**加载失败并报错**，指出条目序号与字段；
      不静默跳过
      - 验收：非法配置用例返回错误；错误信息含条目序号与字段名

## 8. Interface：HTTP 节点端点

- [x] 8.1 新增 `GET /v1/nodes`（列表），返回 JSON 数组，每项含 `id` / `kind` / `status` / `addr`
      - 验收：`curl` 或 httptest 断言 200 与字段齐全；零节点时返回空数组
- [x] 8.2 新增 `GET /v1/nodes/{id}`（详情），未知 id 返回 404 + JSON 错误体
      - 验收：已知 id 200；未知 id 404 且 body 含 `error` 字段
- [x] 8.3 新增 `POST /v1/nodes/{id}/start` 与 `/stop`；非法迁移返回 409 且响应体含当前状态
      - 验收：合法启停返回 200；已启动节点（本 change 为 `registering`）上调 `/start`
        返回 409 且状态不变（`Online` 同理，本 change 不可达，见 design D9）
- [x] 8.4 端点测试
      - 验收：`go test ./internal/interface/http` 全绿；不破坏既有 `/v1/health`、`/healthz`

## 9. cmd 装配

- [x] 9.1 `cmd/gb28181-simulator/main.go` 用 ServiceContext 装配 `NodeService` 与注册表，
      按 `nodes:` 配置注册节点
      - 验收：`go build ./cmd/...` 通过；main.go 仍无业务逻辑
- [x] 9.2 无 `nodes:` 配置时启动行为不变（向后兼容）
      - 验收：启动日志顺序仍为 config → logger → tracing → http；无节点相关错误
- [x] 9.3 修正 `servicectx` 依赖方向（承接 `enterprise-skeleton` 遗留 3）：
      `internal/platform/servicectx/keys.go` 的 `StorageKey` 类型参数由 `*storage.Store` 改为
      `port.Storage`，import 由 `internal/storage` 改为 `internal/domain/port`；
      `main.go` 统一改用 `servicectx.StorageKey`，`MustGet` 返回接口
      - 验收：`go list -deps ./internal/platform/servicectx` 不含 `internal/storage`；
        `go build ./...` 通过；装配入口 `MustGet[port.Storage]` 取回非 nil

## 10. sipprobe 回包（承接 §11.4）

- [x] 10.1 `waitForResponse` 一并返回原始 `sip.Message`（当前丢弃），便于回应请求
      - 验收：函数签名变更编译通过；既有 `sipprobe` 测试全绿
- [x] 10.2 接收模式新增 `--answer` 标志：收到 `sip.Request` 时用 `Receive` 返回的对端地址回送
      200 OK（`sip.NewResponseFromRequest`）
      - 验收：**golden test** 断言回送的 200 OK 报文字节与 fixture 一致（SIP 字节格式任务必须含
        字节级断言）；默认不带 `--answer` 时行为不变
- [x] 10.3 两个 `sipprobe` 互发 INVITE/200 OK：A 用 `--answer`，B 用
      `--send-to ... --expect-status 200`，两者均退出 0
      - 验收：`bin/gb28181-simulator sipprobe` 双进程实测退出码 0（即 `enterprise-skeleton` §11.4
        的未完成任务，本 change 完成）
- [x] 10.4 恢复 `scripts/smoke-sip.sh` 原始断言（A 收、B 发并期望 200）
      - 验收：`make sip-smoke` 通过

## 11. 端到端验证

- [x] 11.1 `go test -race -count=1 -timeout=120s ./...` 全包 100% 通过（既有 159 个 + 新增 ≥ 30 个）
      - 验收：0 FAIL（**单次运行**，不要背靠背重复跑，避免端口竞争假阳性）
- [x] 11.2 多节点共存 e2e：同进程注册两个节点（不同端口），同时启动、各自收发互不串扰；
      停一个不影响另一个
      - 验收：e2e 测试通过；`GET /v1/nodes` 返回 2 个节点
- [x] 11.3 `CGO_ENABLED=0` 五平台编译（linux/amd64、linux/arm64、darwin/amd64、darwin/arm64、
      windows/amd64）
      - 验收：全部 PASS，产物静态链接无 cgo 警告
- [x] 11.4 Golden fixture 全量校验
      - 验收：`find . -name testdata -type d | xargs -I{} sh -c 'cd {} && sha256sum -c golden-sha256'`
        6 份全 OK（auth 3 + sdp 3），新增的 200 OK fixture 一并纳入

## 12. 文档与报告

- [x] 12.1 更新 `docs/architecture.md`：补 Node 抽象、状态机图、多节点注册表说明
      - 验收：文档含状态迁移表与分层依赖说明
- [x] 12.2 更新 `README.md`：技术栈/开发指南补节点配置与 `/v1/nodes` 说明
      - 验收：README 含 `nodes:` 配置示例
- [x] 12.3 产出 `reports/node-abstraction-verify.md`（同 Change 3 报告结构：任务完成度、
      requirement 覆盖、decision 落地、已知限制）
      - 验收：报告含三维度表格与偏差记录
- [x] 12.4 在报告中记录 `enterprise-skeleton` **Q4 与 §11.4 的关闭结论**
      - 验收：报告明确写"Q4 采用扩展 `Receive` 返回地址方案；§11.4 已完成"，并指向
        `openspec/changes/archive/2026-09-24-enterprise-skeleton/design.md` D6
- [x] 12.5 在报告中记录 `enterprise-skeleton` **遗留 3（servicectx 反向依赖）的关闭结论**
      - 验收：报告写明修正前 `servicectx` import `internal/storage`、修正后依赖 `domain/port`
        的对比，并说明该遗留是在归档后核查发现的、`enterprise-skeleton` 归档工件中未记录

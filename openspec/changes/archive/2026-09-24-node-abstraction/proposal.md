# Proposal

## Why

**路线图阶段编号：4（`node-abstraction` — Node 模型(身份/状态机/实例化/多节点共存)）**

Change 1（`project-skeleton`）交付了骨架，Change 2（`core-sip-stack`）交付了节点无关的
SIP/SDP/Digest/transport 能力，本仓库额外完成的 `enterprise-skeleton` 把 `internal/` 重塑为
六边形架构。但三者都没有"**节点**"这个概念：SIP 能力是**节点无关**的（`Send` 连目标地址都要
从 `msg.URI()` 反推），`app/` 层仍是空的占位目录。

而 GB28181 仿真的本质就是**用一批节点伪造一张网**：设备、上级平台、下级平台各有身份，各自注册、
保活、收发信令，且必须在**同一进程内多实例共存**。路线图 Change 5/6/7（`device-node`、
`platform-large-node`、`platform-small-node`）都要以 Node 抽象为地基，Change 9
（`cascade-and-multi-instance`）更是直接依赖多节点共存。所以 Node 抽象必须现在做。

同时本 change 承接 `enterprise-skeleton` 归档时留下的三项未决项：`design.md` **Q4**
（transport 对端地址暴露方式）、任务 **§11.4**（两个 `sipprobe` 互发 INVITE/200 OK），
以及归档后核查发现的 **`servicectx` 反向依赖**（`internal/platform/servicectx/keys.go`
import 了 `internal/storage`，违反 platform 不应依赖 adapter 的分层约束——该项在
`enterprise-skeleton` 归档工件中未记录，本 change 首次纳入）。

## What Changes

- **Node 身份模型**（`internal/domain/model/node.go`）：新增不可变值对象 `NodeID`
  （GB/T 28181 20 位编码，含中心编码/行业编码/类型编码/网络标识/序号的校验规则）、
  `NodeKind`（`device` / `platform-large` / `platform-small`）、`NodeProfile`（身份 +
  信令地址 + 归属域 + 厂家），以及 `Node`。
- **Node 状态机**（`internal/domain/model/node_status.go`）：定义 `Idle → Registering →
  Registered → Online → Offline → Fault` 及合法迁移表；非法迁移返回哨兵错误
  `ErrIllegalTransition`，不 panic。
- **Node 端口**（`internal/domain/port/node.go`）：新增 `NodeRegistry`（注册/注销/查找/遍历）
  与 `NodeLifecycle`（Start/Stop/Status）两个端口，供 `app/` 依赖，`adapter` 实现。
- **实例化与多节点共存**（`internal/app/node_service.go` + `internal/adapter/nodereg/`）：
  `NodeService` 用例编排；进程内注册表支持多个 `Node` 并存，各自持有独立 transport listener、
  独立生命周期、独立日志字段（`node_id`）。
- **配置**：`internal/platform/config` 新增 `nodes:` 列表结构（每节点 id/kind/domain/addr），
  兼容现有单实例配置（缺省视为零节点，不改变现有启动行为）。
- **HTTP API**：`internal/interface/http` 新增 `/v1/nodes`（列表）、`/v1/nodes/{id}`（详情）、
  `/v1/nodes/{id}/start|stop`（单节点启停）。
- **transport 对端地址**（承接 Q4）：`port.SIPTransport` 的 `Receive` 返回对端地址，
  `Send` 接受显式目标地址，使节点间定向通信成为可能。
- **sipprobe 回包**（承接 §11.4）：`sipprobe` 接收模式可对入站 `sip.Request` 回 200 OK，
  使两个 `sipprobe` 能互发 INVITE/200 OK 并通过 `scripts/smoke-sip.sh` 原始断言。
- **servicectx 依赖方向修正**（承接归档后核查项）：`servicectx.StorageKey` 的 key 类型由
  `*storage.Store` 改为 `port.Storage`，`keys.go` 改为依赖 `internal/domain/port`，
  消除 platform 层对 adapter 层的反向依赖。

## Non-Goals

显式排除不属于本阶段的 GB28181 能力：

- 不实现 `device` / `platform-large` / `platform-small` 三种身份的**具体协议行为**
  （注册/保活/目录查询/点流）——分别是路线图 Change 5 / 6 / 7。本 change 只定义
  `NodeKind` 枚举与状态机骨架，三种身份的行为留给后续 change 填充。
- 不实现 MANSCDP+ XML 编解码、PS 封装/解封装、RTP 分包（路线图 Change 3
  `core-manscdp-and-ps`，本仓库尚未执行）。
- 不实现四种媒体源抽象与转封装管道（Change 8）。
- 不实现级联路径与任意拓扑演练（Change 9）。
- 不实现动态目录/报警、录像与回放、移动位置（Change 10）。
- 不实现 GB28181-2022 增量能力与 `X-GB-Ver` 协商（Change 11）。
- 不实现 SM2 互认证与 SM3 Note 完整性（Change 12）。
- 不实现异常流注入、抓包面板与 pcap 导出（Change 13）。
- 不实现完整 Web 管理界面（Change 14）；本 change 只提供上述 JSON API，不改前端。
- 不实现 YAML 场景脚本引擎与报告产出（Change 15）。
- 不引入需要 CGO 的依赖；不引入 wire/fx 等 DI 框架（继续手写 ServiceContext）。

## Capabilities

### New Capabilities

- `node-abstraction`: Node 身份模型、状态机、进程内多节点注册与生命周期管理，以及承接
  `enterprise-skeleton` 遗留的 transport 对端地址、sipprobe 回包能力与 servicectx
  依赖方向修正。

### Modified Capabilities

- `core-sip-stack`: 扩展「Diagnostic CLI `sipprobe`」——接收模式需能对入站请求回 200 OK
  （当前只收不发，现有 scenario 依赖外部 client）；并新增 transport 对端地址与目标地址的
  要求（`enterprise-skeleton` §11.4 的依赖）。

**注（不产生 spec 变更）**：`enterprise-skeleton` 的「Domain ports define contracts」已规定
`SIPTransport` 为 `Send(ctx, Message, dst) error` / `Receive(ctx) (Message, string, error)`，
但当前实现是 `Send(ctx, msg) error` / `Receive(ctx) (msg, error)`——**是实现对不上已归档的
spec**，不是 spec 写错。本 change 把实现对齐到 spec，因此 `enterprise-skeleton` 无需
MODIFIED（spec 文本保持原样）。这同时回答了归档时留下的 **Q4**：采用"扩展 `Receive` 返回
对端地址"方案，而非新增 `ReceiveFrom` 方法。

## Impact

- **新增代码**：`internal/domain/model/node*.go`、`internal/domain/port/node.go`、
  `internal/app/node_service.go`、`internal/adapter/nodereg/`。
- **修改代码**：`internal/domain/port/transport.go`（`Send`/`Receive` 签名）、
  `internal/adapter/siptransport/`（对端地址 + 目标地址）、`internal/sipprobe/`（回包）、
  `internal/platform/config/`（节点配置）、`internal/interface/http/`（节点端点）、
  `internal/platform/servicectx/keys.go`（`StorageKey` 类型改为 `port.Storage`）、
  `cmd/gb28181-simulator/main.go`（装配 NodeService，统一使用 `servicectx.StorageKey`）。
- **依赖**：无新增第三方依赖（仍是纯 Go 无 CGO）。
- **兼容性**：`port.SIPTransport` 签名变更属 **BREAKING**（对实现方），影响
  `internal/adapter/siptransport` 与其测试；`config` 新增 `nodes:` 为可选，向后兼容。
- **测试**：现有 159 个测试必须保持通过；新增 Node 模型/状态机/注册表/端点测试 ≥ 30 个。
- **脚本**：`scripts/smoke-sip.sh` 恢复原始双进程断言。

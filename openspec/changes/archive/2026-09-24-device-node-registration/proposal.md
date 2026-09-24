# Proposal

## Why

路线图阶段 **#5 `device-node`：device 身份完整实现**。Change 4 `node-abstraction` 交付了节点骨架后，节点启动只能推进到 `registering` 便停在原地——`internal/app/node_service.go` 明确写着"registration is driven by the identity implementations in Change 5/6/7"，且 HTTP `/start` 的既有约定就是"start 后状态为 `registering`"。也就是说当前进程内没有一个节点真正向国标平台注册过，仿真能力为零。

本次把路线图 #5 拆出第一块可独立交付的能力：**device 身份的注册全流程**（REGISTER → 401 挑战 → Digest 重发 → 200 OK → `registered` → `online`）。选它作为 #5 的第一块，是因为它是三种身份里唯一"必须主动上行"的最小闭环：只需 UAC 一侧，不需要 UAS 注册受理、目录、媒体即可端到端验证；心跳、注销、目录上报等其余 device 行为随后在同一 capability 下增量补齐。

同时本 change 修正两个已暴露的实现缺陷，它们是注册能否成立的前置阻塞：`model.Message → gosip` 转换丢弃调用方全部自定义头（`internal/adapter/siptransport/transport_port.go`，`nodereg/e2e_test.go` 已用注释记录该限制），以及 Digest 只有"服务端校验 + 生成挑战"方向、缺少客户端侧"解析挑战 + 组装 Authorization 头"的能力。

## What Changes

- **新增 device 注册用例**：`internal/app/` 下新增 device 注册器（use case），编排完整 REGISTER 事务：构造 REGISTER（Contact / Expires / 可选 `X-GB-Ver`）→ 发送至配置的上级平台 → 收到 401 后解析 `WWW-Authenticate` → 用 Digest（qop=auth）计算 response 并组装 `Authorization` 头 → 以同一 Call-ID、递增 CSeq 重发 → 收到 200 OK 后推进状态。注册器只依赖 domain 端口，不 import adapter。
- **启动即注册**：`POST /v1/nodes/{id}/start` 对 device 身份节点在绑定 listener 后同步完成上述事务；成功推进 `registering → registered → online`，失败（超时 / 401 后仍被拒 / 403 / 5xx）回落 `Fault` 并在日志与 HTTP 错误体中给出原因。**BREAKING**：与 Change 4 定稿的"`/start` 后状态为 `registering`"约定不一致，故同步修订该约定（见 Modified Capabilities）。
- **新增 domain 端口 `Authorizer`**：客户端侧 Digest 能力（解析挑战、生成 Authorization 头、cnonce/nc 管理），配 adapter 挂在 `internal/adapter/auth`。现有 `port.Authenticator` / `port.Challenger` 语义都是"服务端收请求"，不复用。
- **修复 `model.Message → gosip` 头丢失**：转换必须透传调用方给出的全部头部（Contact / Expires / Authorization / From / To / Call-ID），使 REGISTER 报文在字节层面合法。
- **修复 Via transport 硬编码**：`BuildRequest` 目前把 Via 的 `Transport` 固定为 `UDP`，TCP 节点会与真实绑定协议不一致。
- **新增注册配置**：`nodes[]` 条目新增注册所需字段（上级平台地址、鉴权用户名/密码、expires、transport），缺省时行为与今天一致（不注册，仅到 `registering`）；非法取值在配置加载阶段报错并指出条目序号与字段。
- **新增测试用 UAS**：能收 REGISTER → 回 401 + `WWW-Authenticate` → 校验 `Authorization` → 回 200 OK 的进程内测试对端，用于真 UDP 回环验证（当前仓库没有任何 fake / mock `SIPTransport`，也无会发 401 的对端）。

## Capabilities

### New Capabilities

- `device-node`: device（IPC/NVR/DVR）身份的行为规范，本 change 覆盖其注册全流程：REGISTER 事务编排、401 Digest 挑战应答、注册成功/失败的状态推进与可观测性。后续 device 的保活、注销、目录与点播在此 capability 下增量添加。

### Modified Capabilities

- `node-abstraction`: 两处 requirement 行为变化 ——（1）"Node configuration is declarative and backward compatible"：`nodes[]` 条目除 `id`/`kind`/`domain`/`addr` 外可携带注册参数，并新增其校验；（2）"HTTP API exposes node inventory and per-node control"：`/start` 对已配置注册的 device 节点不再停在 `registering`，而推进到 `online`；注册失败回落 `Fault`，HTTP 返回错误而非静默成功。
- `core-sip-stack`: 两处 requirement 行为变化 ——（1）"Digest challenge and response"：补齐客户端方向能力，即解析服务端 `WWW-Authenticate` 与组装完整 `Authorization` 头（现有 spec 只约定了 challenge 生成与 response 计算）；（2）"SIP message round-trip preserves wire bytes"：新增"调用方显式给出的头部在传输层转换后不丢失"的场景约束。

## Impact

- **代码**：`internal/app/`（新增注册用例与 `NodeService` 编排改动）、`internal/domain/model/`（注册配置值对象）、`internal/domain/port/`（新增 `Authorizer`）、`internal/adapter/auth/`（挑战解析与 Authorization 组装）、`internal/adapter/sip/`（Via transport、`WithContact`/`WithExpires`）、`internal/adapter/siptransport/`（头透传修复）、`internal/platform/config/`（配置字段与校验）、`interface/http/`（`/start` 语义与错误体）。
- **配置**：`nodes[]` 新增可选注册段；`configs/config.example.yaml` 同步；新增字段中的密码受既有 `log.redact_keys` 脱敏覆盖。
- **依赖**：不新增第三方依赖；Digest 复用 Change 2 已落地的 `internal/adapter/auth`（标准库 `crypto/md5` 与既有 `HashFunc` 替换点），SIP 报文构造复用 gosip 与 `internal/adapter/sip/builder.go`。
- **API**：`POST /v1/nodes/{id}/start` 的响应语义变化（对 device 节点而言成功即 `online`），返回体新增注册失败原因；`GET /v1/nodes` 的 `status` 字段可能首次出现 `registered` / `online` / `fault`。
- **测试**：新增测试 UAS 与注册流程的字节级 golden test；既有 `nodereg/e2e_test.go`、`node_service_test.go` 中断言"`/start` 后为 `registering`"的用例需按新约定更新。

# Proposal

## Why

**Change 14（web-management-ui）的 Web 前端与后端 HTTP API 之间存在多处契约不一致。** 最严重的 bug 是：用户在「新增通道」对话框中保留前端预填的设备号时，后端返回"channel_id must be a 20-digit number"，保存失败。根因是后端 `generateChannelID` 函数从未按 `channel-id-auto-gen` spec 正确实现，生成了含下划线的不合规格式；同时前端预填逻辑也复制了同样错误的格式。此外，前端账号管理页对列表 API 响应路径解析有误，以及多处 placeholder / 校验规则与 spec 要求不一致。这些不一致在交付初期未被端到端测试覆盖，需要系统性修复并补充测试。

## What Changes

### 后端修复
1. **修复 `generateChannelID`**（`internal/interface/http/channels.go`）：按 `channel-id-auto-gen` spec 生成正确的 20 位纯数字通道 ID：`前6位行政码` + `00`（行业码） + `13`（通道类型码） + `10位补零序号`。
2. **新增 `auto_generated` 响应字段**（`channelResponse`）：当通道 ID 由后端自动生成时响应 `auto_generated: true`，用户显式传入时为 `false`。
3. **移除 `generateChannelID` 中的下划线前缀**：旧实现生成 `${nodeID}_${seq}` 格式，不再使用。

### 前端修复
4. **移除通道 ID 预填**（`ChannelListView.vue`）：用户打开「新增通道」对话框时不再预填 ID，改为留空让后端生成。同步修正 placeholder 文案与校验规则。
5. **修正 `AccountsView.vue` 响应解析**：`api.listNodes()` 返回数组而非 `{nodes:[]}` 对象，移除错误的 `j.nodes` 访问。
6. **统一校验规则**：前端 `chIdRule` 校验规则与后端 `isValidChannelID`（20 位纯数字）严格对齐。

## Capabilities

本 change 不引入新能力，而是修复已 spec'd 能力的实现落地。

### New Capabilities

*（无）*

### Modified Capabilities

- `channel-id-auto-gen`：`generateChannelID` 的实现必须符合 spec 规定的 20 位格式；`channelResponse` 必须包含 `auto_generated` 布尔字段。**这是本次最重要的 spec 修正**。
- `channel-web-ui`：前端「新增通道」对话框的 placeholder、校验规则与响应处理必须与 `channel-id-auto-gen` spec 保持一致。
- `platform-account-management`：Web 账号管理页对 `GET /v1/nodes` 列表 API 的响应路径解析必须正确（列表端点返回数组，不返回 `{nodes:[]}` 包装）。

## Non-goals

- 不修改 GB/T 28181 SIP 信令层的任何行为
- 不修改媒体源的类型或配置逻辑
- 不新增通道级 API 端点
- 不修改节点身份（device/platform-large/platform-small）的后端实现
- 不涉及 YAML 配置文件的格式变更
- 不修改 `generateChannelID` 的并发安全机制（mutex 已存在，保持不变）

## Impact

### 影响的代码文件

| 文件 | 变更类型 |
|------|---------|
| `internal/interface/http/channels.go` | 修复后端 ID 生成逻辑 + 新增响应字段 |
| `web/src/views/ChannelListView.vue` | 移除预填 + 修正 placeholder 与校验规则 |
| `web/src/views/AccountsView.vue` | 修正响应解析路径 |

### 关联的 OpenSpec Specs

- `channel-id-auto-gen`（已有 spec，本次修正实现偏差）
- `channel-web-ui`（已有 spec，本次对齐前端实现）
- `platform-account-management`（已有 spec，本次对齐前端响应解析）

### 关联路线图阶段

**Change 14（web-management-ui）** 的一部分。

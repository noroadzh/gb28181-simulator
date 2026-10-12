# channel-id-auto-gen Specification

## Purpose
TBD - created by archiving change fix-catalog-aggregate-and-channel-autogen. Update Purpose after archive.

## Requirements

### Requirement: 通道 ID 自动生成

`POST /v1/nodes/:id/channels` 的请求体中 `channel_id` 字段从必填改为可选。当请求体未提供 `channel_id` 或其值为空字符串时，服务端 MUST 自动生成一个符合 GB/T 28181 20 位编号规范的通道 ID，并在响应中返回生成的 ID；`channel_id` 显式传入时 MUST 优先使用显式值。

自动生成规则 MUST 满足：
- 前 6 位取自所属节点 ID 的前 6 位行政区域码；
- 第 7–8 位为行业码 `00`；
- 第 9–10 位为类型码 `13`（表示通道）；
- 第 11–20 位为 10 位十进制自增序号，从 1 开始，冲突时递增重试；
- 生成的 ID MUST 在该节点的已有通道集合中唯一。

响应体 MUST 包含 `auto_generated` 布尔字段，指示 ID 是服务端生成（true）还是用户显式指定（false）。

#### Scenario: 请求体省略 channel_id 时自动生成

- **WHEN** `POST /v1/nodes/:id/channels` 请求体仅包含 `{"name":"Cam-1","status":"ON"}`，节点 ID 前 6 位为 `340200`
- **THEN** 响应 `201 Created`，body 中 `id` 为 20 位国标编号，形如 `3402000013<10位序号>`
- **AND** 响应中 `auto_generated = true`

#### Scenario: 生成的编号与已有通道不冲突

- **WHEN** 同一节点下已存在自动生成的通道 ID `34020000130000000001`
- **AND** 再次发送省略 `channel_id` 的新增请求
- **THEN** 响应中的 `id` 为 `34020000130000000002`（序号递增，不重复）

#### Scenario: 显式传入 channel_id 时优先使用

- **WHEN** `POST /v1/nodes/:id/channels` 请求体包含 `"channel_id":"34020000001320000099"`
- **THEN** 响应中 `id = "34020000001320000099"`
- **AND** 响应中 `auto_generated = false`

#### Scenario: 自动生成的编号可通过前端确认或修改

- **WHEN** 用户在 Web 界面「新增通道」对话框中把 ID 输入框留空
- **THEN** 输入框 placeholder 提示「留空自动生成」
- **AND** 保存成功后，前端展示服务端返回的实际编号，用户可后续编辑

### Requirement: 通道 ID 生成格式必须符合 20 位纯数字规范

`generateChannelID` 的实现 MUST 严格遵循以下格式，不允许使用下划线或其他分隔符：

- 第 1–6 位：所属节点 ID 的前 6 位行政区域码（数字）
- 第 7–8 位：行业码，固定为 `00`
- 第 9–10 位：类型码，固定为 `13`（表示通道）
- 第 11–20 位：10 位十进制自增序号，从 1 开始，冲突时递增重试，左侧补零

生成的字符串 MUST 恰好 20 个 ASCII 数字字符，不含字母、下划线或任何其他分隔符。

示例：节点 ID 前 6 位为 `340200`，第二次自动生成的通道 ID 为 `34020000130000000002`。

自动生成时，序号计数器（nextSeq）MUST 在节点级别的 mutex（`autoIDMu`）保护下计算，以确保并发请求不产生重复 ID。

#### Scenario: 空通道列表生成首张 ID

- **WHEN** 节点 `34020000002000000001` 没有任何已存在的通道
- **AND** 客户端调用 `POST /v1/nodes/34020000002000000001/channels` 且请求体不含 `id` 字段
- **THEN** 后端生成 `34020000130000000001`
- **AND** 该字符串恰好 20 个 ASCII 数字字符
- **AND** 该字符串通过 `isValidChannelID` 校验

#### Scenario: 已有通道存在时序号递增

- **WHEN** 节点下已存在通道 `34020000130000000001`
- **AND** 客户端再次省略 `id` 字段提交新增请求
- **THEN** 后端生成 `34020000130000000002`
- **AND** 不与已有 ID 重复

### Requirement: 新增通道响应必须包含 `auto_generated` 字段

`POST /v1/nodes/:id/channels` 的成功响应体（状态码 201）MUST 包含布尔字段 `auto_generated`：

- 当请求体中 `id` 字段为空或省略时，`auto_generated` MUST 为 `true`
- 当请求体中 `id` 字段显式传入有效值时，`auto_generated` MUST 为 `false`

响应体中 `id` 字段 MUST 始终包含最终使用的通道 ID（无论是用户传入还是自动生成）。

#### Scenario: 自动生成通道 ID 时响应 auto_generated=true

- **WHEN** `POST /v1/nodes/:id/channels` 请求体为 `{"name":"Cam-1","status":"ON"}`（不包含 `id` 字段）
- **THEN** 响应状态码为 `201 Created`
- **AND** 响应 body 中 `id` 为 20 位纯数字国标编号，形如 `34020000130000000001`
- **AND** 响应 body 中 `auto_generated` 为 `true`

#### Scenario: 显式传入通道 ID 时响应 auto_generated=false

- **WHEN** `POST /v1/nodes/:id/channels` 请求体包含 `"id":"34020000001320000099"`
- **THEN** 响应中 `id = "34020000001320000099"`
- **AND** 响应中 `auto_generated` 为 `false`

#### Scenario: 并发添加通道时序号不重复

- **WHEN** 同一节点下已有通道 ID `34020000130000000001`
- **AND** 两个客户端同时发送不包含 `id` 字段的新增请求
- **THEN** 两个请求中只有一个获得 `34020000130000000001`（旧序号）
- **AND** 另一个获得 `34020000130000000002`（递增后序号）
- **AND** 两个响应中 `auto_generated` 均为 `true`

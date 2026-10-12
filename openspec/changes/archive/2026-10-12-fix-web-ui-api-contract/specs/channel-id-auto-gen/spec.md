# Spec Delta

## ADDED Requirements

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

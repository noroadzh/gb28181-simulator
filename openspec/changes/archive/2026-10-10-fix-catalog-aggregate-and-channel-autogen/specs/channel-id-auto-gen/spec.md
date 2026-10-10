# channel-id-auto-gen Delta

## ADDED Requirements

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

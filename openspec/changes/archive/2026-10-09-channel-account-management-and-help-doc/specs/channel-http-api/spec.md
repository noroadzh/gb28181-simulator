# Spec Delta

## ADDED Requirements

### Requirement: 通道增删 HTTP 端点

系统 MUST 提供以下端点对 device 节点的通道进行运行时增删：

- `POST /v1/nodes/:id/channels` —— 新增通道（body：channel_id + name + status?）；成功回 201 与新建通道对象；重复 id 回 409；非 device 节点回 404
- `DELETE /v1/nodes/:id/channels/:ch` —— 删除通道；成功回 204；不存在回 404；非 device 节点回 404

新增/删除 MUST 即时反映到后续的 `GET /v1/nodes/:id/channels` 与 Catalog 查询结果。

#### Scenario: 新增通道后可查询到

- **WHEN** POST 新增通道 `34020000001320000099`，随后 GET `/v1/nodes/:id/channels`
- **THEN** 返回列表含该通道

#### Scenario: 新增重复 id 回 409

- **WHEN** POST 一个已存在的 channel_id
- **THEN** 回 409 Conflict

#### Scenario: 删除通道后查询不到

- **WHEN** DELETE 一个存在的通道，随后 GET 列表
- **THEN** 列表不含该通道

#### Scenario: 非 device 节点回 404

- **WHEN** 对 platform-large 节点调用通道增删端点
- **THEN** 回 404 Not Found

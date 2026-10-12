# Spec Delta: channel-http-api

## MODIFIED Requirements

### Requirement: 通道增删 HTTP 端点

> 旧条款：对 `device` 节点调用 `POST /v1/nodes/:id/channels` 与 `DELETE /v1/nodes/:id/channels/:ch` 可成功；非 `device` 节点回 404。
>
> 新条款：对 `device` / `platform-large` / `platform-small` 节点调用上述两个端点均可成功；非这三类节点回 404。新增的通道即时反映到 `GET /v1/nodes/:id/channels` 与节点后续的 Catalog 响应。
>
> 命名说明：下方 `非 device 节点回 404` 这一 Scenario 的标题沿用主 spec 原标题未改，但其规则已收窄为"非 platform 节点回 404"——保留标题是为满足 MODIFIED 块的 scenario 防丢失校验（按标题精确匹配），语义以 WHEN/THEN 为准。

系统 MUST 提供以下端点对节点的通道进行运行时增删：

- `POST /v1/nodes/:id/channels` —— 新增通道（body：channel_id + name + status?）；成功回 201 与新建通道对象；重复 id 回 409；非 platform 节点回 404
- `DELETE /v1/nodes/:id/channels/:ch` —— 删除通道；成功回 204；不存在回 404；非 platform 节点回 404

新增 / 删除 MUST 即时反映到后续的 `GET /v1/nodes/:id/channels` 与 Catalog 查询结果。

#### Scenario: 新增通道后可查询到

- **WHEN** POST 新增通道 `34020000001320000099` 至 `device` 节点，随后 GET `/v1/nodes/:id/channels`
- **THEN** 返回列表含该通道

#### Scenario: 新增重复 id 回 409

- **WHEN** POST 一个已存在的 channel_id
- **THEN** 回 409 Conflict

#### Scenario: 删除通道后查询不到

- **WHEN** DELETE 一个存在的通道，随后 GET 列表
- **THEN** 列表不含该通道

#### Scenario: 非 device 节点回 404

> 标题沿用主 spec 原标题，规则已收窄为"非 platform 节点回 404"——保留标题是为满足 MODIFIED 块的 scenario 防丢失校验（按标题精确匹配），语义以 WHEN/THEN 为准。

- **WHEN** POST / DELETE 通道对未知节点类型（如未来引入的中间件节点）
- **THEN** 回 404 Not Found

#### Scenario: 对 device 节点调用的原有行为不变

- **WHEN** POST / DELETE 通道对 `device` 节点
- **THEN** 行为与该 capability 原有契约一致

#### Scenario: 对 platform-large 节点新增通道成功

- **WHEN** POST `/v1/nodes/{platform-large-id}/channels` body 含合法 channel_id / name
- **THEN** 回 201 + 新建通道对象；该通道持久化到 channel store

#### Scenario: 对 platform-large 节点删除通道成功

- **WHEN** DELETE `/v1/nodes/{platform-large-id}/channels/{channel-id}` 且该通道存在
- **THEN** 回 204；channel store 中该通道被删除

#### Scenario: 新增通道后立即出现在 Catalog 响应中

- **WHEN** POST 新增通道至 `platform-large` 节点
- **AND** 第三方上级随即发送 `MESSAGE(Catalog)` 至该 `platform-large`
- **THEN** Catalog 响应的 `DeviceList` 含该通道，按 device id 升序排列

#### Scenario: 删除通道后立即从 Catalog 响应中消失

- **WHEN** DELETE 一条已存在于 `platform-large` 节点的通道
- **AND** 第三方上级随即发送 `MESSAGE(Catalog)` 至该 `platform-large`
- **THEN** Catalog 响应的 `DeviceList` 不含该通道

#### Scenario: 新增通道时 ID 必须符合 20 位国标码格式

- **WHEN** POST `/v1/nodes/:id/channels` body 的 channel_id 不是 20 位数字
- **THEN** 回 400 Bad Request，错误信息指明 ID 格式要求
# Spec Delta

## ADDED Requirements

### Requirement: 账号管理页必须正确解析节点列表 API 响应

AccountsView 加载节点列表时 MUST 将 `api.listNodes()` 返回的数组直接作为节点数据使用，不得按 `{nodes: [...]}` 路径访问。

- `GET /v1/nodes` 端点返回的是 JSON 数组 `[{"id":"...","kind":"...","status":"..."}, ...]`，不是 `{nodes: [...]}`
- AccountsView 中加载节点列表用于判断当前选中节点是否为 platform 类型时，MUST 直接遍历数组而非访问 `response.nodes`

#### Scenario: 账号管理页正确加载节点列表并判断平台类型

- **WHEN** 用户访问账号管理页，AccountsView 调用 `api.listNodes()`
- **THEN** 前端将返回的数组直接赋值给 `nodes.value`
- **AND** `isPlatform` 计算属性正确识别出 kind 以 `platform` 开头的节点
- **AND** 若无 platform 节点，页面显示说明信息而非空表格

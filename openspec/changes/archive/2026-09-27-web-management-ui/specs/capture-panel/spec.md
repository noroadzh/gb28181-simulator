# Spec Delta

## ADDED Requirements

### Requirement: 前端资源嵌入与静态服务

模拟器二进制必须（MUST）通过 `embed.FS` 嵌入前端构建产物，并在 `/web/` 路径下提供静态文件服务。当配置 `web.enabled=false` 时，服务必须（MUST）完全不挂载前端路由，只暴露 REST API。前端必须（MUST）使用 Hash 路由以兼容静态文件服务。

#### Scenario: 默认启动可访问前端

- **WHEN** 模拟器以默认配置启动，客户端请求 `GET /web/`
- **THEN** 响应返回嵌入的 `index.html`，页面中引用的 js/css 资源均从 `/web/` 路径加载成功

#### Scenario: 关闭 web 开关后纯 API 模式

- **WHEN** 配置 `web.enabled=false` 后启动，客户端请求 `GET /web/`
- **THEN** 返回 404，REST API 端点不受影响

### Requirement: 节点概览页

节点概览页必须（MUST）调用 `GET /v1/nodes` 展示所有节点的 ID、类型、监听地址、在线状态与故障计数，以卡片网格布局呈现。页面必须（MUST）提供手动刷新按钮与按节点类型筛选的能力。

#### Scenario: 展示已注册节点

- **WHEN** 平台节点与设备节点均已注册，用户打开节点概览页
- **THEN** 页面呈现两个节点卡片，各字段与 `GET /v1/nodes` 返回一致

#### Scenario: 按类型筛选

- **WHEN** 页面存在 device 与 platform-large 两类节点，用户在筛选器中选择 `device`
- **THEN** 页面只展示 device 类型的节点卡片

### Requirement: 抓包面板页

抓包面板页必须（MUST）允许用户选择一个节点，并通过 `GET /v1/nodes/:id/capture?limit=N` 展示该节点近期的 wire events，包括方向、时间戳、本地/对端端点与字节大小。点击事件行必须（MUST）展开 hexdump 预览原始 payload。页面必须（MUST）提供 `GET /v1/nodes/:id/capture.pcap` 的下载按钮。

#### Scenario: 查看注册交互的抓包

- **WHEN** 设备完成一次注册，用户在抓包面板选择设备节点
- **THEN** 事件列表中可见 receive 方向的 REGISTER 与 transmit 方向的 200 OK，展开后可读出原始 SIP 报文

#### Scenario: 下载 pcap 文件

- **WHEN** 用户在抓包面板点击下载按钮
- **THEN** 浏览器收到 `Content-Type: application/vnd.tcpdump.pcap` 的二进制响应，可用 Wireshark 打开

### Requirement: 空状态与错误兜底

当节点无抓包数据或 API 请求失败时，页面必须（MUST）展示明确的空状态或错误提示，而不是空白或浏览器默认报错。

#### Scenario: 无抓包数据

- **WHEN** 所选节点的 capture 缓冲为空
- **THEN** 页面显示"暂无抓包数据"提示而非报错

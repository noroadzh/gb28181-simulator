# Spec Delta

## ADDED Requirements

### Requirement: 按节点 wire 抓包

节点收发的每条 SIP 消息都必须（MUST）被捕获，包括完整原始 payload 字节（而非截断预览）、方向（发送/接收）、本地与对端端点、传输协议、时间戳，并必须（MUST）带有所属节点 ID tag，以确保并存节点的事件不会混淆。抓包覆盖节点使用的所有传输上的请求与响应。

#### Scenario: 双向事件都被捕获

- **WHEN** 一个设备节点与平台交换 REGISTER 及其 200 OK
- **THEN** 抓包缓冲中包含一个 receive 事件（REGISTER）与一个 transmit 事件（200 OK），各自保留线上发送的精确字节

#### Scenario: 节点之间不混淆

- **WHEN** 两个节点同时运行，各自与自己的对端交互
- **THEN** 查询节点 A 的抓包只返回带节点 A tag 的事件

### Requirement: 有界环形缓冲

capture store 必须（MUST）有界：每节点最多保留配置数量的事件；满时驱逐最旧事件以容纳新事件。capture 绝不能阻塞或反压 SIP 传输：写入事件是尽力而为且立即返回。

#### Scenario: 最旧事件被驱逐

- **WHEN** 每节点容量为 100，且已捕获 150 条事件
- **THEN** store 只保留最近的 100 条，查询 API 按捕获顺序返回它们

#### Scenario: capture 永不阻塞信令

- **WHEN** 事件产生速度快于消费速度
- **THEN** 传输层收发延迟不受影响，SIP 路径上观察不到等待事件的现象

### Requirement: capture 默认关闭

未安装 capture store 时系统行为必须（MUST）与本变更前完全一致：wire events 继续落入既有 no-op emitter，不发生缓冲，也不为 capture 保留内存。

#### Scenario: 默认启动不捕获

- **WHEN** 模拟器在配置中没有 capture 段的情况下启动
- **THEN** capture API 无数据，传输行为与变更前基线一致

### Requirement: 实时订阅扇出

capture store 必须（MUST）允许消费者订阅并在事件被捕获时实时接收。慢或卡住的订阅者不能拖慢 capture 或其他订阅者：其未投递的事件被丢弃，而不是无界排队。取消订阅必须（MUST）停止投递并释放订阅者。

#### Scenario: 订阅者实时收到事件

- **WHEN** 一个消费者订阅，随后一条 REGISTER 被捕获
- **THEN** 消费者无需轮询即可收到该事件

#### Scenario: 慢订阅者被跳过而非阻塞

- **WHEN** 一个订阅者从不读自己的通道，而流量持续产生
- **THEN** capture 继续运行，其他订阅者持续收到事件，卡住的订阅者只是错过这些事件

### Requirement: pcap 导出

HTTP API 必须（MUST）提供 `GET /v1/nodes/:id/capture.pcap`，生成包含该节点缓冲事件的经典 pcap 文件（合成 Ethernet/IPv4/UDP 帧），使标准工具（Wireshark、tshark）将 payload 识别为 SIP。文件必须（MUST）使用每条事件记录的地址填充 IP/UDP 层，并保留原始捕获时间戳。请求不存在节点的抓包必须（MUST）返回 404；缓冲为空的节点必须（MUST）生成合法的空 pcap 文件。

#### Scenario: 导出的文件可在 Wireshark 中按 SIP 解析

- **WHEN** 一个节点交换过 REGISTER 与 200 OK，客户端下载其 pcap
- **THEN** 文件按经典 pcap 解析，包含两个 UDP 包及原始 payload 与时间戳，标准 SIP 解码生效

#### Scenario: 未知节点返回 404

- **WHEN** 客户端请求一个不存在节点 ID 的 pcap
- **THEN** API 返回 404

### Requirement: 抓包查询 API

HTTP API 必须（MUST）提供 `GET /v1/nodes/:id/capture`，按捕获顺序以 JSON 返回节点缓冲的事件（方向、端点、传输、时间戳、大小、原始 payload），并支持可选 `limit` 参数仅返回最近事件。请求未知节点必须（MUST）返回 404。查询不得驱逐或修改缓冲。

#### Scenario: 查询按顺序返回最近事件

- **WHEN** 节点缓冲中有 50 条事件，客户端请求 limit=10
- **THEN** API 返回最近的 10 条事件（其中按最旧优先排列），且此后缓冲仍保留全部 50 条

#### Scenario: payload 字节被保留

- **WHEN** 某捕获事件的 payload 含有 Digest 凭据
- **THEN** 查询 API 返回完全相同的字节——抓包是诊断记录，不做脱敏

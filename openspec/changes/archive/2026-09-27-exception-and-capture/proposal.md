# 提案：异常流量与抓包（路线图 #13）

## 背景

路线图第 **#13 阶段（`exception-and-capture`）**。模拟器的目标是验证对端设备的鲁棒性，但当前每个节点都只能“表现正常”：每个被服务的请求都按规范回答，不支持的方法则静默丢弃，因此无法观察对端在面对 4xx/5xx 回复、慢响应或静默对等端时的表现。与此同时，Change 2（`siptransport`）在每次发送/接收时发出的 `audit.WireEvent` 流仍然落到 `NopEmitter` 里，排查互通问题只能依赖 tcpdump 等外部工具，而不是直接读取模拟器已经看到的报文。

## 变更内容

1. **故障注入配置（Fault Injection Profiles）** — 每个节点新增可选的故障段，用于描述异常行为：按 SIP 方法映射的预制错误回复（如 REGISTER→403、INVITE→486、MESSAGE→500）、响应延迟（固定/抖动）、概率性静默丢弃，以及心跳黑洞（设备停止回复 Keepalive）。故障注入为可选；未配置故障段的节点行为与之前完全一致。

2. **运行时故障 API** — `POST /v1/nodes/:id/faults` 与 `DELETE /v1/nodes/:id/faults` 可在运行时设置与清空节点的故障配置，对应现有的触发告警等动作 API。

3. **UAS 异常编排** — acceptor 在每次处理请求前先查询故障配置：预制回复会跳过正常 handler，延迟回复会先 Sleep 再作答，被丢弃的请求会被计数并记录日志。不支持的方法还会产生可配置的应答（默认 `501 Not Implemented`），同时保留默认的静默丢弃。

4. **抓包管道（Capture Pipeline）** — 新 `internal/adapter/capture` 包实现 `port.CaptureStore`，将传输层上报的 wire events 写入按节点隔离的环形缓冲（保留完整原始字节，而非 256 字节预览），并为实时消费者提供扇出订阅通道（抓包面板 UI 属于 Change 14）。

5. **pcap 导出** — capture adapter 会将节点缓冲的事件序列化为经典 pcap 文件（使用 `gopacket` 的纯 Go `pcapgo` 写入器），在 SIP payload 外合成 Ethernet/IPv4/UDP 帧（不依赖 CGO，不做真实抓包）。对外暴露为 `GET /v1/nodes/:id/capture.pcap`。

6. **抓包查询 API** — `GET /v1/nodes/:id/capture` 返回近期事件（方向、端点、时间戳、大小、原始 payload），并支持 `limit` 参数，方便 Change 14 面板和 CI 脚本读取会话记录，而无需下载文件。

## 能力边界

### 新增能力

- `exception-injection`：按节点可选故障配置（按方法预制错误、延迟、丢弃、心跳黑洞）、acceptor/keeper 编排点、运行时故障 HTTP API 以及异常事件统计。
- `capture-and-pcap`：按节点 wire-event 抓包写入有界环形缓冲、实时订阅扇出、纯 Go pcap 文件导出、抓包查询/下载 HTTP API。

### 修改的能力

（无 — 故障注入与抓包都是增量特性；所有现有 fixture 与 spec 场景默认关闭，行为保持不变。）

## 非目标

- Web 抓包面板（hexdump 视图、实时刷新、下载按钮）— 属于 Change 14 `web-management-ui`，本次只提供后端端点。
- 媒体面抓包（RTP/PS 包转存到 pcap）— 当前仅支持信令面（SIP/SDP payload）。
- SRTP/SM4 payload 解密。
- GB 35114 B 级审计日志签名。
- 场景脚本（YAML 驱动的故障脚本与报告）— Change 15 `scenario-engine` 将驱动此处构建的故障 API。
- 实时网卡抓包（libpcap/AF_PACKET）；pcap 文件由模拟器自身观察到的报文合成生成。

## 影响范围

- `internal/domain/model` — 新增故障配置值对象；为 port 层新增 wire-event 抓包条目类型。
- `internal/domain/port` — 新增 `CaptureStore` / fault store 端口。
- `internal/adapter/capture` — 新增包：环形缓冲、订阅扇出、`pcapgo` 写入器。引入 `github.com/google/gopacket`（纯 Go，无 CGO）。
- `internal/adapter/audit` + `internal/adapter/siptransport` — `WireEvent` 增加所属节点 ID；传输构造时附加 node tag。
- `internal/app` — acceptor/keeper 在服务前咨询可注入的 fault port；app 仍然只依赖 `domain/port`。
- `internal/interface/http` — 新增故障与抓包端点。
- `cmd/gb28181-simulator/main.go` — 安装 capture adapter 作为全局 audit emitter。

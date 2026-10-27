# Proposal

## Why

模拟平台的媒体面是空的：平台节点收到带 SDP 的 INVITE 一律回 200，却把设备回传的 RTP 丢进 `nullESWriter`；`/v1/flv` 出的画面来自平台自己的本地媒体源，不是下级设备的真实流。作为一台平台模拟器，这意味着「上级平台经我拉下级设备的流」和「我在界面上看下级设备」都只有信令、没有画面。本 change 补上平台侧媒体面的地基：**平台主动向下级设备拉流并持有实时会话**，后续的级联媒体中继与平台录像都建立在它之上。

路线图编号：#41 `platform-live-pull`（#40 归档后的首个新阶段）。

## What Changes

- **平台发起实时视频 INVITE**：platform-large / platform-small 节点向其下级设备（模拟子节点或已注册的真实设备）发送视频 INVITE，并解析设备应答的 SDP（`y=` SSRC、`c=`/`m=` 端点、`a=setup`）。
- **媒体接收**：UDP 监听端口（默认）或附录 D 的 TCP 被动监听，收到 RTP 后经 `RTPDeizer` 还原为 PS 帧。
- **实时会话注册表**：以 `(nodeID, channelID)` 为键持有会话，包含 PS 帧总线、对话 Call-ID、传输方式、最近收包时间；同一通道重复 start 先拆旧会话。
- **FLV 出流**：`GET /v1/flv/:nodeID/:channelID` 优先使用实时会话，没有会话时才回落到节点本地媒体源；新增 `?source=live|local` 显式选择。
- **HTTP 控制面**：`POST /v1/nodes/:id/channels/:ch/live` 拉起实时会话，`POST /v1/nodes/:id/channels/:ch/live/stop` 停止；错误语义与既有控制面一致（无目标设备 404 / 不可达 502 / 无应答 504）。
- **生命周期**：任一方 BYE、INVITE 超时、空闲超时、节点停止都会结束会话并释放端口；设备侧行为不变。

## Capabilities

### New Capabilities

- `platform-live-pull`: 平台节点主动向设备拉取实时视频、接收 RTP 并持有 (节点, 通道) 维度的实时会话与 PS 帧总线。

### Modified Capabilities

- `flv-media-gateway`: HTTP-FLV 的数据源从「仅本地媒体源」扩展为「实时会话优先、本地源兜底」，并增加 `?source=` 选择。
- `channel-http-api`: 新增实时拉流的 start / stop 两个通道级端点及其失败语义。

## Impact

- `internal/app`：新增实时会话服务与平台侧视频 INVITE 流程；`acceptor` 增加「发起视频 INVITE 并拿回应答 SDP」的事务方法。
- `internal/app/streaming`：Gateway 接受外部 PS 源（实时会话），保留现有本地源路径。
- `internal/interface/http`：新增两个路由；FLV handler 的源选择逻辑。
- `internal/domain/port`：新增实时源提供者端口（app 与 streaming 之间）。
- **不影响**：设备节点出流路径、对讲/回放现有语义、GB35114 信令。

## Non-goals

- 不做平台侧录像落盘与录像查询（决策归入后续 change `platform-recording`，第一版即要求可查询可回放，但不在本 change）。
- 不做级联 INVITE 转发与媒体桥接（后续 change `cascade-media-relay`）。
- 不做转码：设备发什么编码就出什么（PS → FLV 现有管线支持 H.264/H.265/MPEG-4 Part 2）。
- 不补 DeviceStatus / Broadcast / 报警查询等信令命令（后续 change `device-control-coverage`）。
- 不做平台向设备发起 TCP 媒体之外的传输协商增强（TLS/SRTP）。

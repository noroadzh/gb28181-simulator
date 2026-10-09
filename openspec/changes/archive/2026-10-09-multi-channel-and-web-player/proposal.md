# Proposal: multi-channel-and-web-player

## Why

当前模拟器已具备单设备多通道的数据模型（`NodeProfile.Channels`），但 Web UI 和流媒体分发能力尚未对齐 EasyGBS demo：一设备多通道的前端可视化、HTTP-FLV 实时播放、PTZ 云台控制、录像回放、语音对讲等功能均缺失。本期补齐这些能力，使模拟器具备与 EasyGBS demo（去除报警查询/算法仓/服务扩展）相近的功能覆盖度。

对应路线图：Change #14 `web-management-ui` 的深度补全，同时覆盖 Change #8 `media-sources` 的分发端（HTTP-FLV）和 Change #10 `dynamic-catalog-alarm-and-playback` 的前端回放。

## What Changes

### 新增能力

- **多通道前端可视化**：device 节点可在 Web 展开通道列表，显示每个通道的名称/ID/在线状态，点击通道进入播放详情页
- **HTTP-FLV 流媒体网关**：Go 内置 PS→FLV 转封装，监听 18090 端口，前端 flv.js 通过 HTTP-FLV 拉流播放
- **通道级媒体源配置**：每个通道可有独立 MediaConfig，缺省时复用 device 级配置（向后兼容 #19 media-source-config）
- **PTZ 云台控制**：前端八方向按钮 + 变倍/变焦/光圈 + 速度调节 + 预置位，通过 HTTP API 触发 DeviceControl MANSCDP
- **录像回放**：设备端录像 RecordInfo 查询，前端录像列表 + 回放播放器（倍速/暂停/拖动）
- **语音对讲**：前端拾音按钮，触发 SIP INVITE（audio）建立 TalkSession，PCMU 编码
- **快照抓图**：单帧截图 HTTP API，复用现有 capture 基础设施
- **用户登录页**：简单 admin/admin 登录，含密码修改
- **Dashboard 统计升级**：通道总数/在线数/离线数/流数量

### 新增 HTTP API

| Method | Path | 说明 |
|--------|------|------|
| GET | `/v1/nodes/:id/channels` | 通道列表 |
| GET/PUT/DELETE | `/v1/nodes/:id/channels/:ch/media` | 通道级媒体源 |
| POST | `/v1/nodes/:id/channels/:ch/ptz` | PTZ 控制 |
| GET | `/v1/nodes/:id/channels/:ch/records` | 录像查询 |
| POST | `/v1/nodes/:id/channels/:ch/playback` | 回放控制 |
| POST | `/v1/nodes/:id/channels/:ch/talk/start` | 开始对讲 |
| POST | `/v1/nodes/:id/channels/:ch/talk/stop` | 停止对讲 |
| GET | `/v1/nodes/:id/channels/:ch/snapshot` | 快照抓图 |
| GET | `/v1/flv/:nodeID/:channelID` | HTTP-FLV 拉流地址 |

### 新增前端路由

- `/login` — 登录页
- `/devices` — 设备列表（含通道列表视图）
- `/channel/:nodeID/:channelID` — 通道详情（播放 + PTZ + 对讲 + 录像）
- `/record/:nodeID/:channelID` — 录像回放页

### 修改行为

- `NodeProfile.MediaConfig` 扩展为 `ByChannel map[string]MediaConfig`，空 map 时回退单实例（向后兼容）
- device 节点 Catalog 响应支持多通道条目（MANSCDP catalogItem），channel ID 正确映射

## Capabilities

### New Capabilities

- `multi-channel-devices`：device 节点的多通道数据模型、通道级 SIP 路由（Channel-ID）、Catalog 序列化、多通道注册流程
- `flv-media-gateway`：Go 内置 PS→HTTP-FLV 转封装网关，复用现有 PS+RTP 管道，监听 18090
- `channel-http-api`：通道列表/媒体源/PTZ/录像查询/对讲/快照 HTTP API
- `channel-web-ui`：flv.js 集成、ChannelDetailView、RecordView、PTZ 控制面板、设备树组件
- `talk-session`：TalkAcceptor/TalkSession 端口定义、gosip INVITE/BYE 实现、PCMU 音频
- `user-auth-ui`：登录页 + 密码修改 + 会话管理
- `dashboard-stats`：Dashboard 通道/离线/流列表统计

### Modified Capabilities

- `media-source-config`：`MediaConfig` 增加 `ByChannel map[string]MediaConfig` 字段，GET/PUT 端点扩展 channel 粒度操作
- `device-node`：Catalog 响应支持多通道条目；SIP INVITE 路由按 Channel-ID 匹配 channel 级媒体源

## Impact

- **新增包**：`internal/adapter/talk/`（talk session）、`internal/app/flvgateway/`（HTTP-FLV 网关）
- **修改包**：`internal/domain/model/`（MediaConfig 扩展）、`internal/app/`（channel 级 pipeline、acceptor talk 处理）、`internal/interface/http/`（新增路由和 handler）、`web/src/`（新增视图和组件）
- **依赖**：flv.js（npm，BSD）、golang.org/x/net/html（FLV 封装，纯 Go）、golang.org/x/net/websocket（对讲音频上行）
- **向后兼容**：`MediaConfig.ByChannel` 为空时完全回退到单实例行为，不破坏现有 API
- **不引入**：ZLMediaKit、EasyPlayer-Pro、商业组件；无 CGO 依赖

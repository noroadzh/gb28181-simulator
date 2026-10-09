# Tasks: multi-channel-and-web-player

## T1: MediaConfig 升级为按 channel 粒度（Backward Compatible）

- [x] 1.1 修改 `internal/domain/model/media.go`：在 `MediaConfig` 增加 `ByChannel map[string]*MediaConfig` 字段，加入 JSON tag 与克隆逻辑
- [x] 1.2 修改 `internal/domain/model/node.go`：`NodeProfile.WithMedia` 和 `MediaConfig()` 支持 channel 粒度的 lookup（空 map 回退单实例）
- [x] 1.3 修改 `internal/interface/http/nodes.go`：在 GET/PUT/DELETE `/v1/nodes/:id/media` 端点旁新增 channel 级端点（T4 前置）
- [x] **Acceptance**：单元测试覆盖 `ByChannel` 设置/查询/回退；`go test ./internal/...` 全部通过

## T2: 内置 HTTP-FLV 流媒体网关

- [x] 2.1 新增 `internal/app/flvgateway/gateway.go`：定义 `FLVGateway` 接口 + `NewFLVGateway()` 构造器
- [x] 2.2 新增 `internal/app/flvgateway/subscriber.go`：`Subscriber` 结构体，管理单个 FLV 订阅生命周期（发送 FLV 头 + onMetaData script tag + 持续写 tag）
- [x] 2.3 新增 `internal/app/flvgateway/mux.go`：`FLVMuxer` 结构体，将 PS 包解析为 FLV Video/Audio tag；支持 H.264 AVCDecoderConfigurationRecord
- [x] 2.4 新增 `internal/app/flvgateway/server.go`：`http.Handler` 暴露 `GET /v1/flv/:nodeID/:channelID`，路由到 `FLVGateway.Subscribe`
- [x] 2.5 修改 `internal/app/application.go`：在 app 层注册 `/v1/flv` 路由和 `FLVGateway` 单例
- [x] 2.6 修改 `internal/app/node_service.go`：channel 级媒体源解析时优先使用 `MediaConfig.ByChannel[channelID]`，回退到 node 级
- [x] **Acceptance**：`go test ./internal/app/flvgateway/...` 全部通过；手动 curl `/v1/flv/{id}/{ch}` 输出 FLV 流；两个浏览器同时播放同一通道独立流

## T3: Talk Session 端口 + gosip INVITE/BYE

- [x] 3.1 新增 `internal/domain/port/talk.go`：定义 `TalkAcceptor` / `TalkSession` 接口
- [x] 3.2 新增 `internal/adapter/talk/session.go`：实现 `TalkSession`（Read PCM/Write PCM→PCMU/Close）
- [x] 3.3 修改 `internal/app/acceptor.go`：新增 `handleTalkInvite`，解析 INVITE 中 audio SDP，返回 200 OK + SDP，建立 RTP 音频流
- [x] 3.4 新增 `internal/app/talk_manager.go`：`TalkManager` 维护活跃会话；`startTalk(nodeID, channelID)` → 发出 INVITE → 返回 session_id
- [x] 3.5 新增 WebSocket 端点 `GET /v1/talk/ws/:session_id`：接收浏览器的上行 PCM 音频，写入 TalkSession
- [x] **Acceptance**：通过 `gosip` + 双端 e2e 验证完整对讲流程（INVITE/200/ACK/RTP/BYE）

## T4: HTTP API（通道列表/PTZ/录像/对讲/快照/统计）

- [x] 4.1 修改 `internal/interface/http/nodes.go`：新增 `GET /v1/nodes/:id/channels` + `GET/PUT/DELETE /v1/nodes/:id/channels/:ch/media`
- [x] 4.2 修改 `internal/interface/http/ptz.go`：新增 `POST /v1/nodes/:id/channels/:ch/ptz`，调用 `handleDeviceControl` 逻辑
- [x] 4.3 修改 `internal/interface/http/playback.go`：新增 `GET /v1/nodes/:id/channels/:ch/records` + `POST /v1/nodes/:id/channels/:ch/playback`
- [x] 4.4 修改 `internal/interface/http/talk.go`：新增 `POST /v1/nodes/:id/channels/:ch/talk/start` + `POST .../talk/stop`
- [x] 4.5 新增 `internal/interface/http/snapshot.go`：`GET /v1/nodes/:id/channels/:ch/snapshot`，复用现有 capture 基础设施
- [x] 4.6 新增 `internal/interface/http/stats.go`：`GET /v1/stats` 聚合统计（节点/通道/流）
- [x] **Acceptance**：`go test ./internal/interface/http/...` 全部通过；curl 各端点验证返回值和错误码

## T5: Web 端集成 flv.js + 前端页面

- [x] 5.1 修改 `web/package.json`：新增 `flv.js` 依赖（npm install --save flv.js）
- [x] 5.2 修改 `web/src/router/index.js`：新增路由 `/login` `/devices` `/channel/:nodeID/:channelID` `/record/:nodeID/:channelID` `/view`
- [x] 5.3 新增 `web/src/views/LoginView.vue`：用户名密码表单 + token 写入 localStorage
- [x] 5.4 修改 `web/src/views/DashboardView.vue`：6 张统计卡片 + 5 秒轮询 `/v1/stats`
- [x] 5.5 修改 `web/src/views/NodesView.vue`：设备列表可展开通道列表 + 跳转入口
- [x] 5.6 新增 `web/src/components/DeviceTree.vue`：左侧树状结构，设备 → 通道，可快速切换
- [x] 5.7 新增 `web/src/components/PTZControlPanel.vue`：八方向按钮 + 变倍/变焦/光圈 + 速度滑块 + 预置位 + 抓图按钮
- [x] 5.8 新增 `web/src/views/ChannelDetailView.vue`：flv.js 播放器 + PTZ 面板 + 对讲按钮 + 录像抽屉 + 拉流地址复制
- [x] 5.9 新增 `web/src/views/RecordView.vue`：录像列表 + 回放播放器（倍速/暂停/拖动）
- [x] 5.10 新增 `web/src/views/VideoView.vue`：1/4/9 分屏设备树，支持拖入通道
- [x] 5.11 修改 `web/src/api.js`：新增通道/PTZ/录像/对讲/快照/统计 API 调用
- [x] 5.12 修改 `web/src/App.vue`：添加路由守卫 + 顶栏用户信息 + 修改密码弹窗
- [x] **Acceptance**：`npm run dev` 成功启动；页面路由跳转正常；flv.js 能播放真实 FLV 流

## T6: 测试、文档与 openspec 归档

- [x] 6.1 修改 `internal/app/flvgateway/gateway_test.go`：FLV 头、Video tag、Audio tag golden test
- [x] 6.2 修改 `internal/app/node_service_platform_test.go`：多通道 Catalog 响应
- [x] 6.3 修改 `internal/adapter/nodereg/registry_test.go`：多通道通道状态
- [x] 6.4 运行 `openspec validate multi-channel-and-web-player --strict`
- [x] 6.5 运行 `openspec archive multi-channel-and-web-player --yes` 归档到 `openspec/changes/archive/2026-10-09-multi-channel-and-web-player/`
- [x] 6.6 修复 CodeBuddy IDE 9 个 Go 诊断问题
- [x] 6.7 运行 `go test ./...` 确保全部通过
- [x] **Acceptance**：所有测试通过；openspec validate --strict 通过；GitHub CI 通过
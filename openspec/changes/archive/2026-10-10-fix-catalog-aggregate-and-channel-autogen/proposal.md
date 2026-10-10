# Proposal

## Why

用户在实际联调中反馈 5 类问题：

1. **第三方平台拉取设备树为空**：第三方平台对接 platform-large 后发送 `Catalog` 查询，返回 `SumNum = 0`。根因是 `answerCatalog` 只返回「已注册的下游设备 ID」+「平台自身 profile channels」，不展开下游设备的通道。当设备服务带 7 个通道注册上来后，第三方只能看到 1 条设备记录甚至看不到通道级设备，无法形成完整设备树。
2. **设备树层级缺失**：即使设备注册成功，第三方看到的设备树也缺少「设备 → 通道」两级结构，与真实 NVR/IPC 的目录形态不符。
3. **通道编号必须手填**：`POST /v1/nodes/:id/channels` 强制要求 `channel_id` 必填，用户新增通道时需手动编造 20 位国标编号，易错且繁琐。
4. **循环播放开关误导**：媒体源表单提供 `loop` 开关，但对非 `file` 类型静默忽略，用户勾选后不生效也无任何提示。
5. **日志只能在服务器上看**：后端已暴露 `/v1/logs/stream` WebSocket，但 Web 界面没有日志查看入口，排障必须登录服务器。

## What Changes

- **Catalog 聚合下游通道**：`answerCatalog` 在返回已注册设备的基础上，逐个设备查找其关联节点 profile 的 channels，将通道作为 `ParentID = 设备ID` 的子条目追加进 `DeviceList`，`SumNum` 相应累加。
- **注册失败可观测**：注册流程在摘要日志中补充 `source_ip` / `transport` / 失败原因（密码不匹配、未知账号等），让「设备没注册上来」从日志即可定位。
- **通道 ID 自动生成**：`handleChannelAdd` 当 `channel_id` 为空时自动生成 `${nodeID 前 10 位行政区码}${行业码}${类型码}${6 位自增序号}`（20 位国标格式），响应中回填生成值；显式传入仍然优先。
- **loop 类型校验**：`NodeService.SetMedia` / `SetChannelMedia` 当 `Loop == true` 且 `Kind != "file"` 时返回 400 错误 `loop is only supported for file sources`，杜绝静默忽略。
- **Web 实时日志页面**：新增 `/logs` 路由与侧边栏「实时日志」入口；页面通过 `api.logsStream()` 订阅 WebSocket，支持级别筛选（trace/debug/info/warn/error）、关键字过滤、暂停/继续、清屏、自动滚动开关、条数上限（默认 1000 条防内存膨胀）。
- **前端通道表单优化**：新增通道对话框 ID 输入框 placeholder 说明「留空自动生成」，保存后回填生成的编号；`MediaPanel.vue` 的 loop 开关在非 file 类型时禁用并给出 tooltip。

## Capabilities

### New Capabilities

- `web-realtime-logs`：Web 界面实时日志查看能力——`/logs` 路由、级别/关键字筛选、暂停继续、容量上限与自动滚动。

### Modified Capabilities

- `platform-large-node`：目录查询（Catalog）应答从「仅在线设备」扩展为「在线设备 + 每个设备关联节点 profile 中的通道（`ParentID` 指向设备）」，`SumNum` 为设备与通道条目总数。
- `channel-http-api`：`POST /v1/nodes/:id/channels` 的 `channel_id` 字段从必填改为可选；为空时服务端自动生成 20 位国标编号并在响应中返回。
- `media-source-config`：`PUT /v1/nodes/:id/media` 与 `PUT /v1/nodes/:id/channels/:ch/media` 新增校验——`loop=true` 仅允许 `kind=file`，否则 400。

## Impact

- **代码改动**：`internal/app/acceptor.go`（Catalog 聚合 + 注册日志）、`internal/app/node_service.go`（loop 校验）、`internal/interface/http/channels.go`（ID 自动生成）、`web/src/views/LogView.vue`（新增）、`web/src/views/ChannelListView.vue`、`web/src/views/MediaPanel.vue`、`web/src/router/index.js`、`web/src/App.vue`。
- **协议兼容性**：Catalog 响应新增通道条目为增量行为，符合 GB/T 28181 目录语义（设备与通道均为目录项）；`channel_id` 放宽为可选属于向后兼容。
- **其他**：不影响媒体转发、级联路由、抓包、场景引擎。

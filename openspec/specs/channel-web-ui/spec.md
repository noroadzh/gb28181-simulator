# channel-web-ui Specification

## Purpose
为通道级操作（实时播放、PTZ、回放、对讲、快照）提供 Vue 3 前端页面与组件，使用 Element Plus 构建监控专业风格界面。

## Requirements

### Requirement: 登录页与认证
- `/login` 页面 MUST 提供用户名密码表单（默认 admin/admin）
- 登录成功 MUST 跳转到 `/` 并将 JWT/session token 写入 `localStorage`
- 路由守卫 MUST 检查 token，无 token 强制跳转到 `/login`
- 顶栏 MUST 显示当前用户与"修改密码"按钮，点击弹出修改表单

#### Scenario: 未登录访问受限页面
- **WHEN** 用户未登录访问 `/devices`
- **THEN** 跳转到 `/login`

#### Scenario: 登录成功
- **WHEN** 表单提交 admin/admin 正确凭据
- **THEN** 200 响应，token 写入 localStorage，跳转到 `/`

### Requirement: Dashboard 统计升级
- `/` 页面 MUST 通过 `/v1/stats` 拉取统计并以 6 张卡片展示：总节点/在线节点/总通道/在线通道/离线通道/活跃流
- 卡片 MUST 每 5 秒刷新一次（轮询）

#### Scenario: Dashboard 渲染
- **WHEN** 浏览器打开 `/`
- **THEN** 6 张卡片显示当前统计数据

### Requirement: 设备列表与通道列表
- `/devices` 页面 MUST 以表格形式列出所有 device 节点
- 每个设备行 MUST 可展开，展开后显示该设备所有 channel（id、name、状态徽标、媒体源类型）
- 点击 channel 行 MUST 跳转到 `/channel/:nodeID/:channelID`

#### Scenario: 查看多通道设备
- **WHEN** 用户点击设备"展开"按钮
- **THEN** 表格展开显示该设备所有 channel

#### Scenario: 进入通道详情
- **WHEN** 用户点击 channel 行"播放"按钮
- **THEN** 跳转到 `/channel/{nodeID}/{channelID}`

### Requirement: 通道详情页（实时播放 + PTZ + 对讲 + 录像）
- `/channel/:nodeID/:channelID` 页面 MUST 包含：
  - 顶部视频窗口（flv.js 拉流地址 `/v1/flv/{nodeID}/{channelID}`）
  - 左侧设备树（设备 → 通道，可快速切换）
  - 右侧 PTZ 面板（八方向按钮 + 变倍/变焦/光圈 + 速度滑块 + 预置位调用 + 抓图按钮）
  - 底部对讲按钮（点击后建立 WebSocket 上行 `/v1/talk/ws/:session_id`，下采样到 PCMU 8kHz）
  - 录像列表（侧边抽屉）：触发 RecordInfo 查询 + 跳转到 `/record/...`

#### Scenario: 实时播放
- **WHEN** 页面加载完成
- **THEN** flv.js 创建 player 实例并 attach 到 video 元素，拉流地址为 `/v1/flv/{nodeID}/{channelID}`

#### Scenario: PTZ 上方向
- **WHEN** 用户点击 PTZ 面板"↑"按钮
- **THEN** POST `/v1/nodes/{nodeID}/channels/{channelID}/ptz` body=`{cmd:"up", speed:128}`

#### Scenario: 抓图
- **WHEN** 用户点击 PTZ 面板"快照"按钮
- **THEN** GET `/v1/nodes/{nodeID}/channels/{channelID}/snapshot`，返回的 JPEG 在浏览器新窗口展示或下载

#### Scenario: 开始对讲
- **WHEN** 用户点击"开始对讲"按钮
- **THEN** POST `/v1/nodes/{nodeID}/channels/{channelID}/talk/start`，200 + session_id，建立 WebSocket `/v1/talk/ws/{session_id}` 上行音频

### Requirement: 录像回放页
- `/record/:nodeID/:channelID` 页面 MUST 包含：
  - 录像列表（左侧）：通过 `/v1/nodes/.../records?start=&end=` 拉取
  - 视频窗口：选中录像后通过 `/v1/nodes/.../channels/.../playback/flv?sn=...` 拉流播放
  - 播放器控制：播放/暂停/拖动条/倍速(0.5/1/2/4)/停止

#### Scenario: 查询录像
- **WHEN** 页面打开
- **THEN** 自动拉取最近 7 天录像并以表格展示

#### Scenario: 播放录像
- **WHEN** 用户点击录像行"播放"
- **THEN** POST playback action=play 创建回放会话，flv.js 订阅 `/v1/.../playback/flv?sn=...`

#### Scenario: 倍速切换
- **WHEN** 播放器处于播放态，用户选择"2x"
- **THEN** POST playback action=speed + speed=2

### Requirement: 视频调阅页（多分屏）
- `/view` 页面 MUST 提供 1/4/9 画面布局切换
- 每个画面 MUST 支持从设备树拖入通道
- 一键清屏 MUST 释放所有画面订阅

#### Scenario: 四分屏
- **WHEN** 用户切换到 4 分屏
- **THEN** 页面显示 4 个视频窗口，每个绑定一个通道

### Requirement: 拉流地址复制
- 通道详情页 MUST 显示当前通道拉流地址（HTTP-FLV URL），点击"复制"按钮复制到剪贴板

#### Scenario: 复制拉流地址
- **WHEN** 用户点击"复制"
- **THEN** `navigator.clipboard.writeText("/v1/flv/{nodeID}/{channelID}")`

### Requirement: 新增通道对话框与上传绑定入口

ChannelListView MUST 提供"新增通道"按钮，点击弹出对话框收集 channel_id（20 位国标编码校验）、name 与可选 status，提交后调用 `POST /v1/nodes/:id/channels` 创建并刷新列表。每张通道卡片的"媒体源"操作 MUST 支持两种方式：手动输入 URL（RTSP/HLS/file 路径）与上传文件（调用上传端点，成功后自动回填容器内路径到该通道媒体源）。

#### Scenario: 新增通道对话框

- **WHEN** 点击"新增通道"按钮
- **THEN** 弹出对话框，含 channel_id、name、status 输入；channel_id 需为 20 位编码，否则禁用提交

#### Scenario: 上传文件绑定通道媒体源

- **WHEN** 在通道卡片的"媒体源"操作中选择"上传文件"，上传一个 mp4
- **THEN** 上传成功后该通道的媒体源自动绑定为 `{kind:"local_file", path:<返回路径>}`，无需手动填路径

#### Scenario: 手动输入 URL 绑定通道媒体源

- **WHEN** 在"媒体源"操作中选择"手动输入"，输入 `rtsp://...`
- **THEN** 该通道媒体源按 URL 前缀解析 kind 并保存

### Requirement: platform 节点账号入口与节点列表可见性

NodesView MUST 为 platform-large 节点卡片显示"账号"入口按钮，点击跳转账号管理页；device 节点保留"通道"与"媒体源"入口按钮。侧边栏菜单 MUST 新增"账号管理"项（在节点概览之后），点击进入账号管理页并默认选中第一个 platform 节点。

#### Scenario: platform 节点有账号入口

- **WHEN** 节点列表中某节点 kind 为 platform-large
- **THEN** 该节点卡片显示"账号"按钮；不显示"通道"按钮

#### Scenario: 侧边栏有账号管理入口

- **WHEN** 用户查看侧边栏菜单
- **THEN** 菜单含"账号管理"项，点击进入账号管理页

### Requirement: Root Path Redirects to Dashboard

Web 路由的根路径 `/` MUST 重定向到 `/dashboard`，使打开 Web 界面的用户直接看到仪表盘视图。

#### Scenario: User opens root path

WHEN user navigates to the web UI root path "/"
THEN browser MUST redirect to "/dashboard"
AND Dashboard page MUST load immediately showing system overview, channel list, and live log

### Requirement: Account List Displays Data Correctly (No "undefined" String)

`AccountsView.vue` 表格列的字段名 MUST 与后端返回的 JSON key 大小写一致，消除 "undefined" 显示。后端 JSON 返回 `username`（小写）与 `created_at`（小写）。

#### Scenario: Account list renders with data

WHEN platform node has accounts in the system
AND user navigates to the accounts management page
THEN table MUST display correct username values (not "undefined")
AND table MUST display correct creation timestamps (not "undefined")
AND no "undefined" string MUST appear anywhere in the account list

### Requirement: Accounts Page Shows Clear Message for Non-Platform Nodes

当用户通过 URL 直接访问账号管理页时，若当前选中的节点不是 platform 类型（或无 platform 节点），页面 MUST 显示明确的说明信息，而非仅显示空表格或无数据提示。

#### Scenario: Device node user visits accounts page

WHEN user navigates to the accounts management page
AND the selected node is a device node (not platform)
THEN page MUST display a visible warning or notice: "账号管理仅适用于 platform 类型节点，当前节点类型为 device"
AND table MUST remain empty or show an appropriate empty state

### Requirement: Channel Upload Entry Is Directly Accessible

通道卡片的媒体源配置入口 MUST 支持直接从卡片操作区域点击"上传媒体"按钮打开上传对话框，无需通过嵌套下拉菜单进入。

#### Scenario: User uploads media file to a channel

WHEN user is on the channel list page
AND clicks the upload/media button on a channel card
THEN upload dialog MUST open immediately without navigating through dropdown menus
AND selected file MUST be uploaded and bound to that channel automatically

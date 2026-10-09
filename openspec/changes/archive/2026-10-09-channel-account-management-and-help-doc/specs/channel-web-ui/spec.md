# Spec Delta

## ADDED Requirements

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

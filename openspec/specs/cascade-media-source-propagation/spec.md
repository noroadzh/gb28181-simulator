# cascade-media-source-propagation Specification

## Purpose
定义级联拓扑下，媒体源配置通过 SIP catalog XML 扩展段在节点间的传播行为：device 节点 catalog 响应携带媒体源元数据，platform 节点 catalog 请求解析并回填到本地通道配置，使 HTTP API 与 Web UI 能在平台侧看到下级节点的媒体源。

## Requirements

### Requirement: Device 节点 catalog 响应携带媒体源扩展段

当 device 节点响应上游 platform 的 catalog 查询（GB/T 28181-2016 A.2.4.1）时，对每个携带媒体源配置（`MediaConfig.Kind != ""`）的 catalog Item，MUST 在标准 Item 段后追加 `<Extension>` 子段，其中包含嵌套的 `<MediaSource>` 元素，元素属性按 `kind`、`path`、`loop`、`fps`、`clock`、`mtu`、`ssrc` 的顺序依次写入。当未配置媒体源时 MUST 省略该子段。

#### Scenario: device 节点有通道级媒体源
- **WHEN** device 节点 `NodeProfile` 中存在 `mediaConfigByChannel["34020000130000000001"] = {Kind: "file", Path: "/uploads/a.mp4", Loop: true, FPS: 25, Clock: 90000}`，platform 拉取 catalog
- **THEN** catalog XML Item `DeviceID="34020000130000000001"` 内 MUST 包含 `<Extension><MediaSource kind="file" path="/uploads/a.mp4" loop="true" fps="25" clock="90000"/></Extension>`

#### Scenario: device 节点有节点级媒体源但无通道级
- **WHEN** device 节点 `NodeProfile.mediaConfig = {Kind: "file", Path: "/uploads/b.mp4"}` 且 `mediaConfigByChannel` 不包含该通道 ID，platform 拉取 catalog
- **THEN** 该通道的 catalog Item 内 MUST 包含 `<Extension><MediaSource kind="file" path="/uploads/b.mp4" .../></Extension>`，回退到节点级配置
- **AND** 其他未在 `mediaConfigByChannel` 中的通道也按相同规则回退到节点级

#### Scenario: device 节点无媒体源
- **WHEN** device 节点既无 `mediaConfig` 也无 `mediaConfigByChannel`
- **THEN** 所有 catalog Item MUST NOT 包含 `<Extension>` 子段

#### Scenario: 通道级覆盖节点级
- **WHEN** device 节点 `mediaConfig = {Kind: "file", Path: "/uploads/node.mp4"}` 且 `mediaConfigByChannel["34020000130000000001"] = {Kind: "rtsp", Path: "rtsp://cam/stream"}`
- **THEN** 该通道的 catalog Item 内 `<MediaSource>` MUST 描述 rtsp 配置（kind="rtsp" path="rtsp://cam/stream"），不是节点级 file

#### Scenario: 多种 MediaSource 类型
- **WHEN** device 节点通道配置 `Kind` 为 `file` / `rtsp` / `hls` / `synthetic` 中任一种
- **THEN** catalog Item 的 `<MediaSource kind="...">` MUST 正确反映实际 kind 值

### Requirement: Platform 节点 catalog 解析并回填媒体源

当 platform 节点收到 device 的 catalog 响应（标准 MANSCDP `Response/CmdType=Catalog`）时，对每个 catalog Item，MUST 解析 `<Extension>/<MediaSource>` 子段（如果存在）并按 `DeviceID` 作为 channelID 写入本地通道级 `MediaConfig`。**`Extension` 段为非标准字段，platform 节点在解析失败或段缺失时 MUST 保持该通道 `has_media=false` 的当前行为，不返回错误**。

#### Scenario: platform 解析到带 MediaSource 的通道
- **WHEN** platform 收到 device 的 catalog 响应，包含 `<Item><DeviceID>34020000130000000001</DeviceID>...<Extension><MediaSource kind="file" path="/uploads/a.mp4" loop="true" fps="25" clock="90000"/></Extension></Item>`
- **THEN** platform 本地通道 `34020000130000000001` 的 `mediaConfigByChannel[34020000130000000001]` MUST 被设为 `{Kind: "file", Path: "/uploads/a.mp4", Loop: true, FPS: 25, Clock: 90000}`

#### Scenario: platform 收到无 MediaSource 的通道
- **WHEN** platform 收到 device 的 catalog 响应，Item 内不包含 `<Extension>` 段
- **THEN** platform 本地该通道的 `mediaConfigByChannel` MUST 不被修改；后续 HTTP API 仍按 `channel-http-api` spec 的「channel 优先 → node fallback」规则响应

#### Scenario: 解析失败时静默降级
- **WHEN** platform 解析 catalog 响应时 `<Extension>` 段 XML 格式错误（属性缺失、类型非法等）
- **THEN** platform MUST 记录 warn 日志并跳过该 Item 的媒体源回填；catalog 响应整体解析 MUST NOT 失败；其他 Item 仍按规则处理

#### Scenario: platform 不接受任意 Item 注入
- **WHEN** platform 收到的 catalog 响应中包含 `DeviceID` 长度非 20 位或包含非法字符
- **THEN** platform MUST 拒绝该 Item（不写入本地），其他合法 Item 仍正常处理

### Requirement: 媒体源回填与 catalog 重拉

platform 节点 MUST 在以下时机重新解析 device 的 catalog 响应并应用媒体源回填：
1. platform 节点启动完成后首次 catalog 拉取周期
2. device 节点重新注册（REGISTER 200 OK）后下一次 catalog 拉取周期
3. platform HTTP API 显式触发 catalog 刷新（若存在该端点）

**回填 MUST 覆盖已有的 channel 级 `MediaConfig`**，实现「最近一次 catalog 响应为准」语义，但 MUST NOT 修改 `nodeProfile.mediaConfig`（节点级媒体源是平台本地配置，不受 device 端覆盖）。

#### Scenario: device 移除媒体源后重新上报
- **WHEN** platform 本地通道 `34020000130000000001` 已存在 `mediaConfigByChannel`；下一周期 device catalog 响应中该 Item 不再包含 `<MediaSource>` 段
- **THEN** platform MUST 清除该通道的 `mediaConfigByChannel[34020000130000000001]`

#### Scenario: device 媒体源变更被覆盖
- **WHEN** platform 本地通道 `34020000130000000001` 当前 `mediaConfigByChannel` = `{Kind: "file", Path: "/uploads/old.mp4"}`；下一周期 device 响应 `<MediaSource kind="file" path="/uploads/new.mp4"/>`
- **THEN** platform MUST 更新为 `{Kind: "file", Path: "/uploads/new.mp4"}`

#### Scenario: 节点级配置不被 device 覆盖
- **WHEN** platform 本地 `nodeProfile.mediaConfig = {Kind: "synthetic", Path: "test"}`；device 响应中不包含节点级 media 段（device catalog 只针对 channel，节点级不在响应中）
- **THEN** platform 的 `nodeProfile.mediaConfig` MUST 保持原值 `{Kind: "synthetic", Path: "test"}` 不变

### Requirement: 互操作性与标准降级

扩展段 MUST 仅出现在 `<Item>` 内，不影响 catalog 响应的标准字段。**未发送扩展段的设备（真实国标 IPC/NVR）MUST 仍能被 platform 正常解析，行为与现状一致**：`has_media=false`，节点级 `has_media` 仍按 `channel-http-api` spec 的回退规则响应。

#### Scenario: 真实国标设备（无扩展段）兼容
- **WHEN** device 节点（真实国标设备）catalog 响应仅包含标准字段，`<Item>` 内无 `<Extension>` 段
- **THEN** platform MUST 正常解析 catalog 并在本地创建通道（不带 `mediaConfigByChannel`）；`GET /v1/nodes/{pid}/channels` 响应 `has_media` 按 `channel-http-api` spec fallback 规则解析（可能为 true 若 platform 节点级有配置）

#### Scenario: 扩展段不影响标准 XML 解析器
- **WHEN** 任何标准 MANSCDP XML 解析器（不考虑 simulator 扩展）解析带 `<Extension>` 段的 catalog 响应
- **THEN** 解析器 MUST 能正常识别所有标准字段（DeviceID / Name / Status 等），`<Extension>` 段被忽略，不报错

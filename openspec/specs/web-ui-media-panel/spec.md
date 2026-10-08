# web-ui-media-panel Specification

## Purpose
Define the Web UI panel for per-node media source configuration.
This is the UI-side counterpart to the API defined in `specs/media-source-config/spec.md`.
It extends the existing `web-management-ui` capability.

---

## Requirements

### Requirement: NodesView has a MediaSource tab

In `NodesView.vue`, the node detail dialog (`el-dialog`) MUST contain an additional tab labeled "媒体源"
(`el-tab-pane` with `label="媒体源"`). This tab is only visible for `kind = device` nodes; platform nodes
have no media source to configure.

#### Scenario: Device node shows media tab

- **WHEN** a user clicks on a device node in `NodesView.vue`
- **THEN** the node detail dialog opens with at least these tabs: "基本信息", "通道/设备", "故障注入", "媒体源", "抓包"

#### Scenario: Platform node hides media tab

- **WHEN** a user clicks on a platform-large or platform-small node in `NodesView.vue`
- **THEN** the node detail dialog does NOT include the "媒体源" tab

---

### Requirement: Media tab shows current config

The "媒体源" tab MUST display the node's current `MediaConfig` in read-only form,
or a "未配置媒体源" empty state with a "设置媒体源" button.

#### Scenario: Node with no media config

- **WHEN** a user opens the media tab for a device node with no configured source
- **THEN** the tab shows "未配置媒体源" and a primary button "设置媒体源"

#### Scenario: Node with media config

- **WHEN** a user opens the media tab for a device node with a configured source
- **THEN** the tab displays: Kind (label)、Path (truncated)、Loop、MTU、Clock、FPS
- **AND** shows "播放中" indicator

---

### Requirement: Media tab provides a configuration form

Clicking "设置媒体源" MUST open an `el-form` form in a new `el-dialog` or inline card:

- `kind`: `el-select` with four options: "本地文件 (file)", "RTSP (rtsp)", "HLS (hls)", "合成图 (synthetic)"
- `path`: `el-input` with `placeholder`. Hidden when `kind = synthetic`.
  - For `kind = file`: shows a file browser button that opens a server-side file picker dialog.
  - For `kind = rtsp/hls`: shows a URL input with protocol prefix.
- `loop`: `el-switch`, default true for file/synthetic, false for rtsp/hls
- `mtu`: `el-input-number` (600–1500), default 1400
- `ssrc`: `el-input-number` (0 = auto), default 0
- `clock`: `el-input-number` (default 90000)
- `fps`: `el-input-number` (1–120, synthetic only), default 25

#### Scenario: Form submission sends PUT

- **WHEN** a user fills in the form and clicks "保存"
- **THEN** the frontend sends `PUT /v1/nodes/:id/media` with the JSON body
- **AND** on `200` response, refreshes the displayed config and closes the form

#### Scenario: Form validation errors

- **WHEN** a user submits without a `path` while `kind = "file"`
- **THEN** the form shows inline validation "请输入文件路径"
- **AND** no request is sent

#### Scenario: Delete configuration

- **WHEN** a user clicks the "删除" button on an existing media tab
- **THEN** the frontend sends `DELETE /v1/nodes/:id/media`
- **AND** on `204`, the tab resets to the empty state

---

### Requirement: Live playback state indicator

The "媒体源" tab MUST show a live status indicator refreshed every 2 seconds:

| State | Indicator |
|-------|-----------|
| 未配置 | 灰色 "未配置" |
| 已配置 | 蓝色 "已配置" |
| 播放中 | 绿色圆点 + "播放中" |
| 播放失败 | 红色 + "播放失败" |

The refresh is driven by the `loadMedia` action (GET `/v1/nodes/:id/media`) polling on an interval,
gated by the dialog's `visible` state so it stops polling when the user closes the tab.

#### Scenario: Status updates after save

- **WHEN** a user saves a new media config
- **THEN** the indicator immediately changes to "已配置"
- **AND** if a platform node then sends an INVITE, it changes to "播放中"

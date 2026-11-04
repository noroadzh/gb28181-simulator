# web-ui-platform-ops Specification

## Purpose

定义平台节点在 Web UI 上的拉流与录像库操作面：与设备通道页（本地媒体 / PTZ / 对讲 / RecordInfo）分流，消费既有 `platform-live-pull` / `platform-recording` HTTP API。

## Requirements

### Requirement: 平台通道拉流操作面

当当前节点 `kind` 为平台（`platform-large` / `platform-small` 或以 `platform` 开头）时，通道详情页 MUST 提供：

- 传输选择（`udp` / `tcp`）与「开始拉流」「停止拉流」；
- 活动会话的 `session_id` / `transport` / `local_port` 展示（进入页时 SHOULD 调用 `GET …/live` 恢复状态）；
- 拉流成功后可用 `source=live` 的 FLV 预览。

平台通道详情 MUST NOT 展示设备专属的 PTZ 云台面板与语音对讲入口。未拉流时开始播放 MUST 给出可辨识提示，而非静默当作本地媒体源成功。

#### Scenario: 平台拉流并预览

- **WHEN** 运维在平台通道详情选择 UDP 并开始拉流且下级在线
- **THEN** UI 展示会话信息，随后开始播放可看到 live FLV

#### Scenario: 设备页不受影响

- **WHEN** 节点为 `device`
- **THEN** 仍展示 PTZ / 对讲 / 本地媒体播放路径

### Requirement: 平台录像库操作面

当节点为平台时，录像回放页 MUST 使用 `GET …/recordings` 列表与 `GET …/recordings/:id/flv` 播放；MUST NOT 对平台录像条目调用设备侧 `POST …/playback`。

设备节点录像页行为保持既有 RecordInfo + playback INVITE 路径。

#### Scenario: 平台录像回放

- **WHEN** 平台通道已有录像分段且运维点选一条
- **THEN** 播放器以 recording FLV 出流，且未向设备发回放 INVITE

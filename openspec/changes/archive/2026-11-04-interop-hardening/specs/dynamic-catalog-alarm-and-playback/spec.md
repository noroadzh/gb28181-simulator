# Spec Delta

## MODIFIED Requirements

### Requirement: playback-control-handling

设备节点 MUST 接受入站 `PlaybackControl`（MESSAGE / MANSCDP）：在存在对应回放会话时，`Play` / `Pause` / `Teardown`（及带 `Scale` 的倍速）MUST 调用与 MANSRTSP INFO 共用的回放控制路径，真正改变会话状态（播放/暂停/结束/倍速）；参数合法时回 `200 OK`，参数非法回 `400 Bad Request`。无对应会话时，`Pause`/`Teardown`/`Scale` MUST NOT 静默成功——MUST 回可辨识失败（400/404 或 ERROR 体）。仅 ACK 而不调用回放控制路径视为不合规。

#### Scenario: start playback

- **WHEN** 平台向已有回放会话的设备发送 PlaybackControl Play（合法通道）
- **THEN** 设备回 `200 OK`，会话处于播放态（可被后续 MediaStatus 或观测 API 反映）

#### Scenario: pause 改变会话

- **WHEN** 设备在播放中收到 PlaybackControl Pause
- **THEN** 回 `200 OK`，会话进入暂停；媒体输出停止或保持暂停语义

#### Scenario: teardown 结束会话

- **WHEN** 设备收到 PlaybackControl Teardown
- **THEN** 回 `200 OK`，回放会话释放

#### Scenario: 无会话时 pause 失败

- **WHEN** 设备在无回放会话时收到 Pause
- **THEN** 回可辨识失败，不得仅 200 空操作

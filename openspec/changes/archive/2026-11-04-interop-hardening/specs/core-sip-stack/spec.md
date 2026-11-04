# Spec Delta

## MODIFIED Requirements

### Requirement: PlaybackPort 扩展 Scale 语义

`PlaybackPort` 接口必须（MUST）支持带 `scale` 的播放控制：`Play(scale)`（1.0 正常，0 暂停，负数为倒放）、暂停与结束会话的明确操作；`PlaybackState` MUST 暴露当前 `Scale`。stub adapter 对非法 scale（NaN 或 ±Inf）返回 `ErrPlaybackUnsupported`。

2016 风格 `PlaybackControl` MESSAGE 路径必须（MUST）解析 Cmd（Play/Pause/Teardown）与可选 Scale，并调用 `PlaybackPort`（或与 MANSRTSP INFO 共用的同一应用层控制入口）。仅回 `200 OK` 而不调用控制入口视为不合规。

#### Scenario: PlaybackControl MESSAGE 调用 PlaybackPort

- **WHEN** 对端发送 `MESSAGE`，body 为 PlaybackControl Play 且存在/可创建对应回放上下文
- **THEN** 节点调用播放控制入口（scale 默认 1.0），回 `200 OK`

#### Scenario: PlaybackControl Pause 调用暂停

- **WHEN** MESSAGE body 为 PlaybackControl Pause 且会话存在
- **THEN** 节点调用暂停入口，回 `200 OK`

#### Scenario: 非法 scale 被拒绝

- **WHEN** 控制携带非法 Scale（NaN / ±Inf）
- **THEN** 控制入口返回 `ErrPlaybackUnsupported`（或等价），acceptor 回 `400 Bad Request`

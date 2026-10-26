# Spec Delta

## ADDED Requirements

### Requirement: 通道详情页的对讲音频

点击「开始对讲」后，前端 MUST 采集麦克风音频（8kHz 单声道）、编码为 μ-law，并以二进制帧发往 `/v1/nodes/:id/channels/:ch/talk/audio?session=<id>`；收到的二进制帧 MUST 解码后播放。点击「停止对讲」MUST 关闭该 WebSocket 并调用 `talk/stop`。采集被拒或会话建立失败 MUST 显示错误且不进入「对讲中」状态。

#### Scenario: 发起对讲

- **WHEN** 用户点击「开始对讲」，浏览器授予麦克风权限
- **THEN** 建立 talk 会话并连接音频 WebSocket，页面进入「对讲中」，麦克风音频以 μ-law 帧上行

#### Scenario: 停止对讲

- **WHEN** 用户点击「停止对讲」
- **THEN** WebSocket 关闭、`talk/stop` 被调用、页面退出「对讲中」

#### Scenario: 麦克风被拒绝

- **WHEN** 浏览器拒绝麦克风权限
- **THEN** 显示可读错误，不发起任何音频上行

### Requirement: 录像回放页的播放

点击某条录像 MUST 先以该条目的 `sn` 与起止时间调用 `POST /v1/nodes/:id/channels/:ch/playback`（`action=play`），成功后再以 `/playback/flv?sn=<sn>` 作为 FLV 源播放。MUST NOT 依赖 records 响应中的 `playback_url`。离开页面或停止播放 MUST 调用 `teardown`。

#### Scenario: 播放一条录像

- **WHEN** 用户点击录像列表中的一条
- **THEN** 会话建立成功，播放器用该会话的 `/playback/flv?sn=` 播放，时长与条目一致

#### Scenario: 会话建立失败

- **WHEN** `play` 返回错误（设备拒绝或不可达）
- **THEN** 页面显示该错误，不创建播放器

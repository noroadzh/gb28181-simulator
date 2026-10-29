# platform-large-node Delta

## MODIFIED Requirements

### Requirement: 平台从录像库受理回放 INVITE

平台节点收到 `s=Playback` 的 INVITE 且其录像库中存在目标通道与时间窗的录像时，MUST 以 200 OK + SDP（平台接收端口、`a=sendonly`）应答，并把对应窗口的录像以 PS/RTP 发送到请求方声明的端点（UDP，或附录 D 的 TCP）。窗口结束或录像耗尽时 MUST 停止发送。库中没有匹配录像时 MUST 返回 404。

对**实时视频**（不带 `s=Playback` 的 `m=video`）INVITE，平台 MUST 按 `cascade-media-relay` 中继到目标通道所属的下级，而不是用通用对话分支回 200 后丢弃媒体。

#### Scenario: 回放 INVITE 成功

- **WHEN** 上级平台用 `s=Playback` + 时间窗 INVITE 本平台，库中有匹配录像
- **THEN** 本平台回 200 + SDP，并把 PS/RTP 发到请求方端点

#### Scenario: 无匹配录像

- **WHEN** 回放请求的时间窗与库中任何分段都没有交集
- **THEN** 返回 404，不发送媒体

#### Scenario: 实时视频 INVITE 被中继

- **WHEN** 上级平台对属于在线下级的通道发起实时视频 INVITE
- **THEN** 平台向该下级发起视频 INVITE 并桥接媒体，而不是本地应答

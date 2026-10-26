# Spec Delta

## ADDED Requirements

### Requirement: device 节点接受音频 INVITE

`device` 节点 MUST 接受携带 audio m-line 的 INVITE（PCMU）并以 200 OK + SDP 应答：`c=` 为本节点地址，`m=audio` 为本地 RTP 端口。不含 audio m-line 的 INVITE MAY 以 488 拒绝。MUST NOT 对音频 INVITE 回 405。

#### Scenario: 音频 INVITE 被受理

- **WHEN** 平台向 device 节点发送携带 PCMU audio 的 INVITE
- **THEN** 节点回 200 OK，SDP 含 `m=audio <port> RTP/AVP 0` 与 `c=IN IP4 <节点地址>`

#### Scenario: 非音频 INVITE 被拒绝

- **WHEN** INVITE 的 SDP 只有 video m-line
- **THEN** 节点回 488 Not Acceptable Here

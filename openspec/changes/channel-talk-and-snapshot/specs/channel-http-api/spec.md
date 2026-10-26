# Spec Delta

## ADDED Requirements

### Requirement: 对讲会话生命周期

`TalkStart` MUST 建立 SIP 音频对话（INVITE，SDP 携带 PCMU）并返回 session id 与浏览器可连接的 WSS 地址；`TalkStop` MUST 发送 BYE 并回收会话。对端 BYE、媒体超时或节点停止 MUST 回收会话；重复 `TalkStop` MUST 幂等。

#### Scenario: 建立与结束对讲

- **WHEN** 对在线 device 通道调用 `talk/start`，随后 `talk/stop`
- **THEN** 第一次返回 200 与 session_id/WSS 地址；第二次返回 204，且上游收到 BYE

#### Scenario: 对端先挂断

- **WHEN** 设备侧先发 BYE
- **THEN** 本地会话被回收，再次 `talk/stop` 仍返回 204（幂等）

### Requirement: 快照来源与失败语义

`Snapshot` MUST 从通道媒体源的最近关键帧解码出 JPEG。无媒体源 MUST 返回 503；节点不是 `device` MUST 返回 501；取帧超时 MUST 返回 504。MUST NOT 返回空 body 的 200。

#### Scenario: 有媒体源时抓到 JPEG

- **WHEN** 通道配置了本地文件媒体源并已产生关键帧
- **THEN** 返回 200，`Content-Type: image/jpeg`，body 为可解码的 JPEG

#### Scenario: 无媒体源

- **WHEN** 通道未配置媒体源
- **THEN** 返回 503 与 JSON 错误体

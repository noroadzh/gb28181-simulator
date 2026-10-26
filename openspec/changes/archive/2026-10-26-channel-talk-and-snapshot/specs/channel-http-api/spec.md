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

`Snapshot` MUST 对**能直接产出像素**的媒体源生成 JPEG：合成测试图（彩条/时钟等）直接渲染当前帧；需要解码的视频编码（H.264 / H.265 / MPEG-4 等）MUST 返回 501 并说明原因——本 change 不引入解码器依赖。无媒体源 MUST 返回 503；节点不是 `device` MUST 返回 501；MUST NOT 返回空 body 的 200。

#### Scenario: 合成测试图源抓到 JPEG

- **WHEN** 通道配置了合成测试图媒体源
- **THEN** 返回 200，`Content-Type: image/jpeg`，body 为可解码的 JPEG（SOI 头 0xFFD8）

#### Scenario: 需要解码的源返回 501

- **WHEN** 通道配置的是 H.264/H.265 等需要解码的文件或流
- **THEN** 返回 501，错误体说明"需要解码器" 

#### Scenario: 无媒体源

- **WHEN** 通道未配置媒体源
- **THEN** 返回 503 与 JSON 错误体

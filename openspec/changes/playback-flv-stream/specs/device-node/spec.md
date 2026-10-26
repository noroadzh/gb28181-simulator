# Spec Delta

## ADDED Requirements

### Requirement: device 节点受理回放 INVITE

`device` 节点 MUST 受理带历史回放语义的 INVITE（SDP `s=Playback`、`u=<deviceID>:<sn>`、`t=<start> <end>`），以 200 OK + SDP 应答，并按其 `t=` 时间窗从通道媒体源定位起播，把 PS/RTP 发往 SDP 声明的平台端口。到达 `end`、收到 BYE/TEARDOWN 或节点停止时 MUST 停止发送并释放端口。

#### Scenario: 按时间窗回放

- **WHEN** 平台以 `t=2026-10-01T00:00:00 2026-10-01T00:05:00` 发起回放 INVITE
- **THEN** 设备回 200 OK，并从该起点开始发送 RTP，直到终点或 TEARDOWN

#### Scenario: 无可用录像

- **WHEN** 通道媒体源不存在或时间窗完全在文件之外
- **THEN** 设备回 404 Not Found，平台将错误返回给调用方

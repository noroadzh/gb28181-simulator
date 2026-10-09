# Spec Delta: talk-session

## Purpose

支撑 GB28181 语音对讲（Talk）流程。该能力定义 `TalkAcceptor` / `TalkSession` 端口契约，并基于 gosip 实现 SIP INVITE/BYE 流程，音频编码使用 PCMU（最简单、零依赖）。

## ADDED Requirements

### Requirement: TalkAcceptor 端口
`internal/domain/port` MUST 定义 `TalkAcceptor` 接口，方法 `AcceptTalk(ctx, deviceID, channelID) (TalkSession, error)`，返回 `TalkSession` 实例或错误。

#### Scenario: 接受对讲邀请
- **WHEN** 上游通过 SIP INVITE 请求音频对讲
- **THEN** `AcceptTalk` 返回 `TalkSession`（200 OK），上层可通过 Read/Write 收发音频

### Requirement: TalkSession 接口
`TalkSession` MUST 暴露 `Read(ctx) ([]byte, error)`、`Write(ctx, pcm []byte) error`、`Close() error`。Read 返回的字节为 PCMU 解码后的 16-bit 8kHz PCM；Write 接受的字节为 16-bit 8kHz PCM，内部编码为 PCMU 后经 RTP 发送。

#### Scenario: 读取下行音频
- **WHEN** 通话建立后调用 Read
- **THEN** 返回 320 字节（20ms 帧）的 PCM 数据

#### Scenario: 写入上行音频
- **WHEN** 调用 Write 传入 320 字节 PCM
- **THEN** 内部编码为 160 字节 PCMU，通过 RTP 发送到对端

### Requirement: SIP INVITE/BYE 流程
gosip 适配器 MUST 在收到 INVITE（audio SDP）后：
1. 校验 SDP 携带的 PCMU 编码能力
2. 返回 200 OK + 自己的 SDP（PCMU）
3. 建立 RTP 接收/发送 goroutine
收到 BYE 或上层 Close 时 MUST 发送 BYE 并释放资源

#### Scenario: 完整对讲流程
- **WHEN** 上游 INVITE → 服务器 200 OK → 上游发送 RTP → 上游 BYE
- **THEN** 服务器正确解码下行音频，正确编码上行音频，BYE 后释放端口

### Requirement: Web 音频上行通道
系统 MUST 提供 WebSocket 端点 `/v1/talk/ws/:session_id` 接收浏览器的上行音频（浏览器以 16-bit PCM 8kHz 发送），编码为 PCMU 后写入 TalkSession。

#### Scenario: Web 上行音频
- **WHEN** 浏览器建立 WebSocket 连接并发送 PCM 字节
- **THEN** 字节被编码为 PCMU 并通过 RTP 发送

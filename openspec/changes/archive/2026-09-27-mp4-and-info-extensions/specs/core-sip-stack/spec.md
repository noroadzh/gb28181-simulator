# Spec Delta

## ADDED Requirements

### Requirement: INFO 方法按 Content-Type 分发

节点必须（MUST）对入站 `INFO` 请求按 `Content-Type` 路由：
1. `Application/MANSRTSP`（或 body 首行为 `PLAY` / `PAUSE`）→ 解析为 MANSRTSP 回放控制命令，路由到 `PlaybackPort`，返回 `200 OK`（port 不支持时回 `481 Call/Transaction Does Not Exist` 或 `400 Bad Request`）。
2. `Application/MANSCDP+XML` 且 body 的 `CmdType` 为 `MediaStatus` → 复用 `handleMediaStatus`，返回 `200 OK`。
3. 其余 body → `200 OK`（宽容处理），记录 debug 日志。

未知 body 形状必须（MUST）不使节点崩溃或中断事务。

#### Scenario: INFO + MANSRTSP PLAY 路由到 playback port

- **WHEN** 对端发送 `INFO`，body 为 `PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: 2.0\r\nRange: npt=10-\r\n\r\n`，`Content-Type: Application/MANSRTSP`
- **THEN** 节点解析出 `Command=PLAY, Scale=2.0, Seek=10`，调用 `PlaybackPort.Play(scale=2.0)`，回 `200 OK`

#### Scenario: INFO + MANSCDP MediaStatus 走 handleMediaStatus

- **WHEN** 对端发送 `INFO`，`Content-Type: Application/MANSCDP+XML`，body 为 `<Notify><CmdType>MediaStatus</CmdType><DeviceID>...</DeviceID></Notify>`
- **THEN** 节点调用 `MediaStatusPort.HandleMediaStatus` 并回 `200 OK`，与 MESSAGE 路径行为一致

#### Scenario: 未知 INFO body 宽容应答

- **WHEN** 对端发送 `INFO`，body 为任意二进制
- **THEN** 节点回 `200 OK`，记录 debug 日志指出无法识别的 body 形状

### Requirement: MANSRTSP 解析器（2022 §G）

`internal/adapter/mansrtsp` 包必须（MUST）提供解析器，把 MANSRTSP body 解析为结构化命令：
- 请求行：`PLAY` / `PAUSE`（method）。
- `Scale` 头：浮点倍速（负数为倒放，0 表示暂停）；缺失时默认 1.0。
- `Range: npt=<start>-<end>` 头：起止时间（秒或 `now`）；缺失时不 seek。
- 无法解析的 body 返回清晰错误。

解析器必须（MUST）被 golden test 覆盖（字节级断言 body → 字段）。

#### Scenario: 解析含 Scale 与 Range 的 PLAY

- **WHEN** 输入 body 为 `PLAY RTSP/1.0\r\nCSeq: 7\r\nScale: 4.0\r\nRange: npt=3600-\r\n\r\n`
- **THEN** 解析结果为 `Command=PLAY, CSeq=7, Scale=4.0, SeekFrom=3600s`

#### Scenario: 解析 PAUSE 无附加头

- **WHEN** 输入 body 为 `PAUSE RTSP/1.0\r\nCSeq: 8\r\n\r\n`
- **THEN** 解析结果为 `Command=PAUSE, Scale=1.0, SeekFrom=0`（默认值）

### Requirement: PlaybackPort 扩展 Scale 语义

`PlaybackPort` 接口必须（MUST）扩展：`Play` 方法新增 `scale float64` 参数（1.0 为正常倍速，0 为暂停，负数为倒放）；`PlaybackState` 新增 `Scale float64` 字段（当前生效倍速）。stub adapter 对非法 scale（NaN 或 ±Inf）返回 `ErrPlaybackUnsupported`。2016 风格 `PlaybackControl` MESSAGE 路径必须（MUST）同步接通到 `PlaybackPort`（当前实现仅回 200 OK 不调 port）。

#### Scenario: PlaybackControl MESSAGE 调用 PlaybackPort

- **WHEN** 设备发送 `MESSAGE`，body 为 `<CmdType>PlaybackControl><PlaybackControl>Play</PlaybackControl><MediaID>...</MediaID></Notify>`
- **THEN** 节点调用 `PlaybackPort.Play(scale=1.0)`，回 `200 OK`

#### Scenario: 非法 scale 被拒绝

- **WHEN** MANSRTSP 命令携带 `Scale: NaN`
- **THEN** `PlaybackPort.Play` 返回 `ErrPlaybackUnsupported`，acceptor 回 `400 Bad Request`

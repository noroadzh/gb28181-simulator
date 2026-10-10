# media-sources 规范

## Purpose

`media-sources` 是 GB28181 的媒体承载层：四种媒体源（本地文件、RTSP、HLS、
合成图）的统一抽象，以及 PS↔RTP 双向转封装管道。它让"能描述媒体"成为
"能产出媒体"——为后续的点播（INVITE）、目录媒体信息提供可消费的流。

## Requirements

### Requirement: 4 种媒体源共享同一抽象

系统 MUST 暴露单一的 `MediaSource` 接口，其用途是持续产出携带表示时间戳的 ES 帧，且与下游传输无关。系统 MUST 提供该接口的四个实现：本地文件、RTSP、HLS 和合成源；每个实现都是一个独立的适配器，不感知 PS 或 RTP。

媒体源只负责产出"带时间戳的 ES 帧流"，四种来源靠同一个接口暴露，和承载层
（PS/RTP）正交。

#### Scenario: 每个源打开并产出 ES 帧

- **WHEN** 一个配置好的媒体源被打开
- **THEN** 它按序产出 ES 帧，每帧携带一个表示时间戳
- **AND** 无法读取的源返回清晰的错误而不是静默挂起

#### Scenario: 源只关闭一次

- **WHEN** 一个媒体源被关闭
- **THEN** 其底层资源（文件句柄、RTSP session、HLS segment reader）被释放，后续读取返回错误

#### Scenario: 源仅在来源上不同，形态一致

- **WHEN** 一个文件、一个 RTSP session、一个 HLS playlist 和合成源分别被打开
- **THEN** 每个都产出相同的 `ESFrame` 形态；消费者无需知道某一帧来自哪个源

#### Scenario: 合成源可复现

- **WHEN** 合成源使用相同的 seed 被打开两次
- **THEN** 两次产出字节级相同的 ES 输出，使端到端测试与 golden test 具有确定性且离线可用

### Requirement: PS 封装与解封装是字节级精确的

系统 MUST 将 ES 帧封装为节目流（PS）包——
系统起始码 `00 00 01 BA`、pack header、包起始码 `00 00 01 BB` 或 `00 00 01 E0`、
PES header 与 payload——并 MUST 将 PS 流解封装回 ES，通过定位 `00 00 01 BA`
并利用 pack header 长度切割帧，不依赖外部边界。两个方向都 MUST 被字节级 golden test 覆盖。

PS 封装/解封装以字节级 golden test 锁定，封包的每一层（系统、包、PES）都要能
被解包端还原。

#### Scenario: 帧经过往返后不变

- **WHEN** 一个 ES 帧先封装再解封装
- **THEN** 得到的 ES 字节与原始帧相等，且表示时间戳得以保留

#### Scenario: PS 流被自身头部切割

- **WHEN** 一个连续的 PS 流被解封装
- **THEN** 每帧边界由 `00 00 01 BA` 起始码和 pack header 长度推导得出，而非依赖任何注入的 marker

### Requirement: RTP 打包与重组是字节级精确的

系统 MUST 将 PS 帧切片为不超过可配置 MTU（默认 1400 字节）的 RTP 包，
在帧的最后一个切片上设置 `marker bit = 1`，递增 sequence numbers，
并从帧的 PTS 以 90 kHz 域打 RTP timestamp 戳。系统 MUST 将 RTP 包按 SSRC
和 payload type 分组重组回 PS 流，按序列号排序并去重。两个方向都 MUST
被字节级 golden test 覆盖。

RTP 分包/收包以字节级 golden test 锁定：分包时的 marker、sequence、timestamp
与收包重组的顺序、去重都要可复现。

#### Scenario: PS 帧切片为 MTU 大小的包

- **WHEN** 一个 PS 帧被分包为 RTP
- **THEN** 每个 payload 至多为 MTU 大小，sequence number 严格递增，且帧的最后一个包携带 marker bit

#### Scenario: 切片包重组回原始帧

- **WHEN** 收到分包了一个 PS 帧的 RTP 包（可能乱序或带重复）
- **THEN** 它们重组为字节级相同的 PS 帧，按序列号排序并去重

### Requirement: MediaService 提供出站管道入口

系统 MUST 提供将媒体源与 PS packetizer 和 RTP packetizer 组合为出站管道的功能，
并暴露 `Open/Close/Read` 语义，使调用方能够从实时媒体源拉取 RTP 包。媒体源故障
MUST 通过节点生命周期上报，且不中断信令。

`MediaService` 是出站管道的入口：把源、PS 封装、RTP 分包串起来，对外暴露
开/关/读；源失败只报错到节点生命周期，不中断信令。

媒体源关闭语义（2026-09-30 fix-concurrency-lifecycles 新增的不变量）：任何媒体源的 `Close()` MUST 使此前 `Open()` 返回的 reader 的阻塞中的 `Read()` 在 100ms 内返回非 nil 错误，无论调用方传入的 context 是否已取消；源内部的下流 goroutine（如 HLS 分片下载循环）MUST 在 `Close()` 后退出。重复调用 `Close()` MUST 幂等且不 panic。

#### Scenario: Open 把配置好的源接入管道

- **WHEN** 以源名称和 transport 配置调用 `Open`
- **THEN** 创建 `MediaService` 管道：源 → PS → RTP
- **AND** `Read` 产出 sequence number 递增且以源 PTS 作为 RTP timestamp 的 RTP 包

#### Scenario: 失败的源不会杀死节点

- **WHEN** 底层媒体源无法打开或在读取时报错
- **THEN** `Read` 返回该错误，节点被上报存在媒体故障，而信令生命周期不受影响

#### Scenario: Close 恰好释放管道一次

- **WHEN** 对已打开的 `MediaService` 调用 `Close`
- **THEN** 源被关闭，管道被拆除，后续读取返回 closed 错误

#### Scenario: Close 打断慢分片的阻塞 Read（fix-concurrency-lifecycles 新增）

- **WHEN** HLS 源正在等待对端产出下一个分片（可能耗时数秒），调用方在未取消 context 的情况下调用 `Close()`
- **THEN** `Open()` 返回的 reader 的阻塞 `Read` 在 100ms 内返回非 nil 错误，内部下载 goroutine 随之退出

#### Scenario: Close 幂等（fix-concurrency-lifecycles 新增）

- **WHEN** 对同一媒体源实例连续调用两次 `Close()`
- **THEN** 第二次调用无错误无 panic 返回，且第一次调用启动的 goroutine 不残留

### Requirement: 按需声明 SDP capability module

为 INVITE 或其 `200 OK` 生成的 SDP  MUST 仅在节点 profile 启用对应 capability module
且对端协商了 GB/T 28181-2022 时，才包含 H.265（payload 100）、AAC（payload 97）
与 G.722.1（payload 99）的 `a=rtpmap` 条目。Capability module 默认关闭；
不含显式 module 配置的 profile MUST 产出与今天一致的 SDP，不做改动。

#### Scenario: 2022 对端启用 H.265 module

- **WHEN** 一个启用了 H.265 module 的 device 节点应答来自 2022 对端的 INVITE
- **THEN** SDP 包含 `a=rtpmap:100 H265/90000`

#### Scenario: module 已启用但对端为 2016

- **WHEN** 同一个节点应答来自未携带 `X-GB-Ver: 2022` 注册的对端的 INVITE
- **THEN** SDP 不包含 H.265 rtpmap 行

#### Scenario: module 默认关闭

- **WHEN** 一个 profile 未声明任何 capability module，且 2022 对端发送 INVITE
- **THEN** SDP 与 2022 引入前的生成结果字节级相同

### Requirement: 本地文件源支持 mp4 容器（魔数嗅探，字节级不变）

当 `MediaConfig.Kind` 为 `file` 且 `Path` 指向的文件以 `ftyp` box（offset 4，ASCII `ftyp`）开头时，`FileSource` 必须（MUST）走 mp4 容器 demux 路径而非裸 Annex-B 流路径。非 mp4 文件必须（MUST）保持当前行为（直接返回底层文件的 `io.ReadCloser`，由 `StreamESReader` 以 `00 00 00 01` 起始码切割 NAL）。`MediaConfig` 不引入新的 `SourceKindMP4`；同一个 `file` kind 自动适配。

#### Scenario: .mp4 文件触发容器 demux

- **WHEN** `MediaConfig{Kind: "file", Path: "/tmp/v.mp4"}` 打开的文件前 12 字节为 `[0,0,0,<len>,'f','t','y','p',...]`
- **THEN** `FileSource.Open` 返回 `MP4Demuxer`；`MediaService.OpenSource` 从该 reader 产出 `model.ESFrame`，每帧带真实容器 PTS
- **AND** 非 mp4 文件（如 `.ps`、`.h264`、`.bin`）的返回值与本次变更前字节级一致

#### Scenario: 非 mp4 文件不受影响

- **WHEN** `FileSource.Open` 打开的文件不包含 `ftyp` box
- **THEN** 返回原始文件流，`StreamESReader` 继续以 `00 00 00 01` 切割 NALU，PTS 按 FPS 合成递增

### Requirement: mp4 容器解析产出 Annex-B 帧与容器 PTS

`MP4Demuxer` 必须（MUST）完成以下功能：
1. 解析 `moov/trak/stbl/stsd`，识别 `avc1` / `hvc1`（视频）与 `mp4a`（音频）sample entry。
2. 视频 sample：将 AVCC/HVCC 的 4 字节长度前缀 NALU 转换为 Annex-B `00 00 00 01` start code，逐 NALU 作为一帧输出，帧内无粘连。
3. 音频 sample（AAC）：从 `esds` atom 解析 AudioSpecificConfig，逐帧生成 7 字节 ADTS 头，原封不动拼接原始 AAC frame。
4. 根据 `stts/stsc/stsz/stco` 计算每个 sample 的 PTS（90 kHz 域），经 `model.ESFrameWithPTS` 挂载。
5. EOF 后自动循环回到首个 sample（文件结束不是流结束）。
6. 遇到不支持的编码（如 VP9、HE-AAC v2）或损坏的 box 时返回清晰错误而非 panic。

#### Scenario: H.264 AVCC→Annex-B 转换

- **WHEN** mp4 文件中一个 video sample 的原始 NALU 长度为 5，内容为 `[0,1,2,3,4]`
- **THEN** demuxer 输出一帧，字节为 `[0,0,0,1,0,1,2,3,4]`

#### Scenario: AAC 帧带 ADTS 头

- **WHEN** 容器包含 AAC sample，且 `esds` 解析出 ASC = `[0x12,0x10]`
- **THEN** 该 sample 输出为 7 字节 ADTS 头 + 原始 AAC frame 字节；ADTS 头中的 `syncword=0xFFF`、`profile`、`sampling_freq_index`、`channel_configuration` 与 ASC 匹配

#### Scenario: 容器 PTS 转换为 90 kHz 域

- **WHEN** 容器 track timescale = 90000，sample 的 decode time = 180000
- **THEN** 对应 ESFrame 的 PTS = 180000
- **AND** 当 track timescale = 30000 时，同 sample 的 PTS = 540000（`90000 * decodeTime / 30000`）

#### Scenario: EOF 循环回到首个 sample

- **WHEN** mp4 文件被读到最后一个 sample 后再次 Read
- **THEN** 返回文件第一个 sample 的字节与 PTS，不返回 io.EOF

### Requirement: 逐帧 PTS 的可选接口（port 层）

`port` 包必须（MUST）提供可选接口 `ESFrameReader`，其 `Read(ctx context.Context) (model.ESFrame, error)` 签名与 `ESReader` 相同。`MediaService.OpenSource` 必须（MUST）在包装 `StreamESReader` 前对 `src.Open` 返回的 reader 做类型断言；若 reader 实现 `ESFrameReader`，则直接作为出站 ES 帧流使用；否则走既有 FPS 合成 PTS 路径。该接口由 mp4 adapter 实现，domain/app 层不感知 mp4。

#### Scenario: mp4 source 直接供给 PS packetizer

- **WHEN** 打开一个 mp4 文件，`MP4Demuxer` 实现 `port.ESFrameReader`，且 `MediaService.OpenSource` 断言通过
- **THEN** 返回的 ESReader 即 demuxer 本身；后续 PS packetizer 读到的 ESFrame 的 PTS 与容器 sample 时间戳一致（90 kHz 域）

#### Scenario: 裸流文件照旧使用 FPS PTS

- **WHEN** 打开一个 `.h264` 裸流文件，reader 不实现 `ESFrameReader`
- **THEN** `MediaService.OpenSource` 返回 `StreamESReader`；PTS 按 `Clock / FPS` 步进递增，与本次变更前行为一致

### Requirement: golden test 锁定 mp4 的 Annex-B 输出与 PTS

系统 MUST 包含一个 golden test fixture：一个硬编码的最小 mp4 文件（至少包含一个 H.264 sample 与一个 AAC sample），断言 `MP4Demuxer` 产出的视频帧起始码为 Annex-B、AAC 帧带 ADTS 头、PTS 在 90 kHz 域为预期值。测试数据以 `testdata/minimal.mp4` 或硬编码 `[]byte` 存放，不依赖外部资源。

#### Scenario: 字节级 golden test 通过

- **WHEN** 测试读取 `testdata/minimal.mp4` 并通过 `MP4Demuxer` 产出所有帧
- **THEN** 视频帧第一个字节为 `0x00 0x00 0x00 0x01`；最后一帧 PTS 等于容器中最后一个 video sample 的 90 kHz 时间戳
- **AND** 修改 Annex-B 转换逻辑会导致 golden 断言失败，保护 demux 字节级精确性

### Requirement: 媒体源 kind 接受 `local_file` 别名

`MediaConfig.Normalize()` MUST 将 `Kind == "local_file"` 替换为 `SourceKindFile ("file")`，确保前端通过上传文件自动绑定时使用的可读 kind 值与后端 MediaSourceFactory 一致。该归一化发生在所有写入路径（`SetMedia()` / `SetChannelMedia()` / 数据库恢复）的最早入口，因此已有数据库中存储的 `local_file` 记录在重启后自然被规整。

#### Scenario: `local_file` 被归一化为 `file`

- **GIVEN** 一个 `MediaConfig{Kind: "local_file", Path: "/data/v.mp4"}`
- **WHEN** 调用 `cfg.Normalize()`
- **THEN** 返回值的 `Kind == "file"`（即 `SourceKindFile`）
- **AND** `Path` 保持原值不变
- **AND** `Normalize().Validate()` 不返回错误
- **AND** 通过 `MediaSourceFactory` 可以打开为 `FileSource`

#### Scenario: `file` 原样保留

- **GIVEN** 一个 `MediaConfig{Kind: "file", Path: "/data/v.mp4"}`
- **WHEN** 调用 `cfg.Normalize()`
- **THEN** 返回值的 `Kind` 仍为 `file`（无副作用）
- **AND** `Path` 保持原值

#### Scenario: 其他 kind 不被归一化影响

- **GIVEN** 任意 `MediaConfig{Kind: "rtsp"|"hls"|"synthetic"|"remote_file", ...}`
- **WHEN** 调用 `cfg.Normalize()`
- **THEN** `Kind` 保持原值
- **AND** 仅 MTU/FPS/Clock 默认值被填充

### Requirement: 上传结果可直接绑定为媒体源（跨接口一致性）

PUT `/v1/nodes/:id/channels/:ch/media` 与 PUT `/v1/nodes/:id/media` MUST 接受 `kind: "local_file"` 并将其归一化为 `file` 后存盘。下次启动节点时，`ChannelMediaConfig(ch)` / `MediaConfig()` 返回的 kind 字段值 MUST 为 `file`，确保 FLV 流媒体端点 `/v1/flv/:nodeID/:channelID` 可成功打开。

#### Scenario: 通道级 `local_file` 上传后绑定

- **GIVEN** 通过 `POST /v1/nodes/:id/upload` 上传得到 path
- **WHEN** 发送 `PUT /v1/nodes/:id/channels/:ch/media` body `{"kind":"local_file","path":<返回路径>}`
- **THEN** 响应 200，body 中 `kind` 字段返回 `"file"`（已归一化）
- **AND** `GET /v1/flv/:nodeID/:channelID` 返回 200 + `Content-Type: video/x-flv` 而非 404

#### Scenario: 节点级 `local_file` 绑定

- **GIVEN** 任意节点 ID
- **WHEN** 发送 `PUT /v1/nodes/:id/media` body `{"kind":"local_file","path":"/data/v.mp4"}`
- **THEN** 响应 200，body 中 `kind` 字段返回 `"file"`
- **AND** 重启节点后 `GET /v1/nodes/:id/media` 仍返回 `kind: "file"`

### Requirement: PS 管道并发安全

`MediaService.SubscribePS` MUST 在所有并发场景下（reader 自然 EOF、ctx 取消、caller 主动调用 cleanup 函数）保证返回的 PS 帧 channel 只被关闭一次。任何路径下都不应触发 `panic: close of closed channel`。

#### Scenario: reader 自然 EOF 触发关闭
- **WHEN** 媒体源读到 EOF，reader goroutine 正常退出
- **THEN** 返回的 channel 被关闭一次，caller 后续调用 `cleanup()` 不会触发 panic

#### Scenario: ctx 取消触发两条关闭路径
- **WHEN** ctx 被取消，reader goroutine 因 `<-ctx.Done()` 退出，同时 caller 调用 `cleanup()` 关闭 src
- **THEN** channel 仅被关闭一次（无 panic），caller 可正常从 channel 的 range 循环退出

#### Scenario: caller 主动调用 cleanup
- **WHEN** caller 在 channel 消费完成前调用 `cleanup()` 函数
- **THEN** src 被关闭，channel 在 reader 自然 EOF 或 ctx 取消时被关闭一次（无 panic）

### Requirement: mp4 容器支持 MPEG-4 Part 2 视频轨道

`MP4Demuxer` MUST 支持 `mp4v`（MPEG-4 Part 2 Visual）sample entry，不再将其作为不支持的编码拒绝：

1. 解析 `mp4v` sample entry 的 `esds` box，提取 DecoderSpecificInfo（tag 0x05）作为轨道 config；config 内容为 VOS/VO/VOL 头序列，自带 `00 00 01` 起始码，原样作为参数集使用。
2. 视频样本是完整 VOP byte-stream（非 length-prefixed NALU），MUST 原样输出，不做 AVCC→Annex-B 长度前缀重写。
3. 每个循环的第一个视频样本前 MUST 前置一次轨道 config。
4. 样本首部若不含 `00 00 01` 起始码，MUST 在其前补 VOP 起始码 `00 00 01 B6`。
5. 循环回卷（`Loop`）语义与既有视频轨道一致：回卷后重新从带 config 前置的首样本开始。
6. 无 `esds` box 或 DecoderSpecificInfo 为空的 `mp4v` 轨道 MUST 产生清晰错误（跳过该轨道），不 panic。

#### Scenario: mp4v 轨道正常解复用
- **WHEN** 打开一个含 `mp4v` 视频轨道（带 esds DecoderSpecificInfo）的 mp4 文件
- **THEN** 第一个视频帧以 VOS/VO/VOL config 字节开头，后续帧为各样本原始字节；每帧 PTS 为容器 sample 的 90 kHz 时间戳

#### Scenario: 样本缺失 VOP 起始码被补齐
- **WHEN** 某视频样本首 4 字节不是 `00 00 01` 起始码
- **THEN** 输出帧在该样本前补 `00 00 01 B6`，其余字节不变

#### Scenario: 循环回卷重新前置 config
- **WHEN** mp4v 文件读到最后一个样本后再次 Read（Loop 开启）
- **THEN** 新一轮首帧再次以 config 字节开头，与第一轮逐字节一致

#### Scenario: 缺失 esds 的 mp4v 轨道被拒绝
- **WHEN** `mp4v` sample entry 不含 `esds` box
- **THEN** 解析返回清晰错误信息并跳过该轨道，进程不崩溃

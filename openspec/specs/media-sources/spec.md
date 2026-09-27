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

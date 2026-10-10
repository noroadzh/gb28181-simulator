# Design: mpeg4-part2-support

## Context

现状与动机见 proposal.md。关键技术事实：

- MPEG-4 Part 2 码流使用 `00 00 01` 起始码序列（B0=VOS、B5=VO、20~2F=VOL、B3=GOV、B6=VOP），与系统的 Annex-B 切分链路（`StreamESReader`、`ParsePS`、`PSDepacketizer`）天然兼容，无需引入新的分帧逻辑。
- MP4 容器中 `mp4v` sample entry 是 VisualSampleEntry 的具体化，box 布局与 `avc1` 相同（fixed 头偏移一致），后随可选 `esds` box；`esds` 解析基础设施（`walkDescriptorsFrom`/`descriptorLength`）已存在（audioSpecificConfig 取 tag 0x05 在用），可直接复用。
- mp4v 样本是完整 VOP byte-stream，**不是** length-prefixed NALU——与 avc1/hvc1 的 AVCC→AnnexB 重写路径不同，走"原样透传"更简单也更快。
- `PSPacketizer` 每条流一个实例（`MediaService.SubscribePS` 每次新建；multicodec 测试亦按此用法），实例级"每流仅一次 PSM"状态可放在 packetizer 内。
- `PSDepacketizer` 按 `00 00 01 E0/C0` 掩码定位 PES、`ParsePS` 同样按 0xE0/0xC0 掩码扫描，`0xBC` 均不命中，PSM 天然被跳过——需补测试固化该行为。

## Goals / Non-Goals

**Goals**

- mp4v 轨道接入既有 demux→PS→RTP 管道，热路径零额外开销（样本不重写、config 每循环前置一次、PSM 每流一次）
- 收流端可从 PS 层准确识别编码（PSM），不依赖猜测
- FLV 网关行为诚实：识别 MPEG-4、明确报错、不输出坏数据

**Non-Goals**

- SDP `a=rtpmap:97 MPEG4/90000` 协商与 RFC 3016 ES 直挂 RTP（见 proposal Non-goals）
- 转码、RTSP/HLS 源的 MPEG-4 载荷、MPEG-1/2 视频

## Decisions

### D1: mp4v 解析走"复用 esds 基础设施 + 原样透传"而非新写描述符解析

`parseSampleEntries` 新增 `case "mp4v"`：复用 `walkDescriptorsFrom` 提取 tag 0x05（DecoderSpecificInfo）作为 `paramSets`；`mp4Track.codec="mp4v"`、`nalLenSize=0` 标记非 length-prefixed 轨道。帧输出路径对 `codec=="mp4v"` 分支：不调用 AVCC→AnnexB 重写，直接拼接。

- *替代方案*：引入 `github.com/abema/go-mp4` 重写整个 demuxer——被否，改动面与回归风险过大，现有自研 demuxer 已有字节级 golden test 保护。
- *config 缺失*：无 esds/DSI 为空时返回错误并跳过轨道（与现有"不支持的编码"容错语义一致）。

### D2: config 前置放在帧输出层而非 packetizer

`mp4Track` 携带 `paramSets`（即 DSI），`ReadFrame`/读样本循环在"每循环首个视频样本"时前置 config。与 avc1/hvc1 的 SPS/PPS 前置时机语义对齐，`Loop` 回卷天然重新生效（回卷即重建读样本状态）。

### D3: PSM 惰性检测放在 PSPacketizer 实例内，流首插入一次

首个视频 ES 帧到达时按其首起始码字节判定 stream_type（0xB0/0xB3/0xB5/0xB6、0x20~0x2F → 0x10；既有 H.264/HEVC 判定沿用现有探测逻辑），构造 PSM（含 CRC-32/MPEG-2，poly 0x04C11DB7，纯 Go 约 15 行查表/按位实现）后 prepend 到该帧 PS 输出。音频 stream_type 仅在流中存在音频 PES 时声明（当前管道视频帧先行，MPEG-4 场景按 0xE0 单条目声明即可，H.264/H.265+AAC 场景沿用现状——PSM 只保证"不误报"，不追求全量描述）。

- *替代方案*：由 MediaService 在管道层注入独立 PSM 帧——被否，PSM 与首帧的时序耦合在 packetizer 内更内聚，且不改变 `SubscribePS` 的帧序列语义之外的任何接口。
- *共享实例风险*：若未来 packetizer 变为跨流共享，实例级状态会误判——实现前核实 `MediaService` 每流新建实例（当前代码即如此），并在 PSM 状态注释中写明该前提。

### D4: FLV 网关"先判 MPEG-4 再判 AVC"并终止会话

`VideoCodec` 新增 `CodecMPEG4`（`String()`="mpeg4"）。`DetectCodec` 重排判定顺序：先检查起始码后字节是否为 MPEG-4 特征（0xB0/0xB3/0xB5/0xB6、0x20~0x2F），再走既有 AVC/HEVC 判定——否则 `0xB6&0x1F=22` 会先命中 AVC 区间，误判无法修复。`runPipeline` 检测到 `CodecMPEG4`：写 error 日志（含 node/channel/编码名与"请走 GB28181 出流验证"提示）后 return（defer 已负责关通道与会话清理）。

- *替代方案*：FLV 定义私有 tag 承载 MPEG-4（如某些播放器的扩展）——被否，无标准、flv.js 不认，纯属伪支持。

### D5: 测试素材优先 ffmpeg 生成，回退程序化构造

`internal/adapter/media/testdata/mpeg4-part2.mp4`：优先用 `ffmpeg -f lavfi -i testsrc=duration=1:size=176x144:rate=12 -c:v mpeg4 ...` 生成小文件一次性入库（<50KB）。若实现环境无 ffmpeg，则在测试中用 go-mp4 库或手写最小 box 序列程序化构造含 mp4v 轨道的最小 MP4。不依赖 `/tmp` 临时文件。

## Risks / Trade-offs

- [PSM 声明与实际编码不符（探测误判）] → 探测只认起始码字节特征，误判空间极小；探测失败（罕见杂乱字节）时不发 PSM，保持现状行为，宁缺毋错
- [mp4v 样本分帧假设（一样本一 VOP）与个别封装不符] → 样本缺起始码时补 `00 00 01 B6` 已覆盖主流 ffmpeg/手机构造；极端交错样本超出模拟器素材范围，记日志不崩溃
- [DetectCodec 判定顺序变更影响现有 HEVC 判定] → HEVC 起始码后首字节（0x40/0x26 等）与 MPEG-4 特征集不相交，重排不影响；用既有 multicodec 测试回归固化
- [crc32 MPEG-2 与标准库 ieee 不同] → 自实现并在单测中用已知向量（如 ISO 13818-1 标准示例值）校验

## Migration Plan

纯增量、无配置/存储/接口变更：合入即生效，回滚即还原。`go test ./...` 全绿为发布门槛；PSM 对真实收流端（WVP/ZLMediaKit）的兼容性建议在联调环境用现有抓包能力（capture-and-pcap）抽查一次。

## Open Questions

无。

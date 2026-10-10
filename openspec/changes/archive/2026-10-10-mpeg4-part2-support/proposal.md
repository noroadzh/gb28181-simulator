# Proposal: mpeg4-part2-support

## Why

模拟器目前不能把 MPEG-4 Part 2（ISO/IEC 14496-2，MP4 容器 `mp4v` 轨道）编码的本地文件作为媒体源：`MP4Demuxer.parseSampleEntries` 显式拒绝 `mp4v`；且 FLV 网关的 `DetectCodec` 会把 MPEG-4 起始码字节误判为 H.264 NALU 类型（如 `0xB6 & 0x1F == 22` 落入 AVC 区间），按 AVC 封 FLV tag 输出坏数据。MPEG-4 Part 2 码流同样使用 `00 00 01` 起始码（B0/B5/B3/B6/20~2F），与现有 Annex-B 切分链路天然兼容，补齐成本低；真实国标平台的 PS 通道本就定义了 stream_type=0x10（ISO/IEC 14496-2 Visual），属于 GB/T 28181 附录 C 的既有能力，宜尽早支持以扩大模拟器的素材兼容面。

## What Changes

- `MP4Demuxer` 支持 `mp4v` sample entry：解析 `esds` 提取 DecoderSpecificInfo（VOS+VO+VOL config，自带起始码），作为轨道参数集在每个循环的首个视频样本前前置；样本缺失 VOP 起始码时补 `00 00 01 B6`；样本为完整 VOP byte-stream，不做 AVCC→AnnexB 长度前缀重写
- `PSPacketizer` 在流首发送一次 PSM（Program Stream Map，`00 00 01 BC`，含 CRC-32/MPEG-2）：按首个视频 ES 帧的起始码惰性判定编码，声明 stream_type——视频 0x10（MPEG-4 Visual）/ 0x1B（H.264）/ 0x24（H.265），音频 0x0F（AAC）；使收流端可从 PS 层准确识别编码
- FLV 网关新增 `CodecMPEG4` 编码识别并优先判定（修复误判 bug）：检测到 MPEG-4 源时，Web 预览会话写明确日志并终止（FLV 容器与浏览器均不支持该编码，报错降级优于输出坏数据）；GB28181 PS/RTP 出流不受影响
- 内置小体积 mp4v 测试素材（`testdata/`），覆盖解复用、PS 打包、网关降级全链路回归；既有 H.264/HEVC 链路行为不变

## Capabilities

### New Capabilities

（无——本次为既有能力的增量，不引入新能力目录。）

### Modified Capabilities

- `media-sources`：「mp4 容器解析产出 Annex-B 帧与容器 PTS」需求扩展——`mp4v` 视频轨道从"显式拒绝"变为受支持：解析 esds DecoderSpecificInfo、首帧前置 config、按需修复 VOP 起始码、循环回卷语义与既有视频轨道一致
- `core-manscdp-and-ps`：「PS 封装器将 ES 帧打包进 pack header」需求扩展——流首可选携带 PSM，按编码惰性声明 elementary_stream_map 的 stream_type；PSM 对现有 PS 解包端透明
- `flv-media-gateway`：「FLV 转封装」需求补充降级语义——上游解出 MPEG-4 Part 2 码流时，网关 MUST NOT 按 AVC 封装输出，而是明确报错并终止该预览会话

## Impact

- 代码：`internal/adapter/media/mp4_demuxer.go`（mp4v 解析）、`internal/adapter/media/ps_packetizer.go`（PSM 生成与编码惰性检测、CRC-32/MPEG-2 小函数）、`internal/app/streaming/flv.go`（`CodecMPEG4`、`DetectCodec` 判定顺序）、`internal/app/streaming/gateway.go`（MPEG-4 降级分支）
- 测试：`internal/adapter/media/mp4_demuxer_test.go`、新增 PSM 单测、`internal/app/streaming/multicodec_test.go`（MPEG-4 用例从"预期被拒"改为"预期成功"并移除 `/tmp` 临时文件依赖）、网关降级行为测试
- 素材：`internal/adapter/media/testdata/` 新增小体积 mp4v 文件（ffmpeg `-vcodec mpeg4` 一次性生成后入库；生成环境无 ffmpeg 时改用程序化构造的最小 MP4）
- 兼容性：无 API 破坏；不改 port 接口签名；不引入 CGO/新第三方依赖；现有 H.264/HEVC 全链路（demux→PS→RTP→FLV）与 SDP 生成行为不变

## Non-goals

- SDP `a=rtpmap:97 MPEG4/90000` 能力协商与 RFC 3016 MPEG-4 ES 直挂 RTP 打包（多数国标平台实际走 PS 封装，本次不实现裸 ES RTP 路径）
- Web FLV 预览对 MPEG-4 源的服务端/浏览器端转码（违背"纯 Go 无 CGO"约束；浏览器 `<video>`/MSE 亦不原生支持该编码）
- RTSP/HLS 拉流源的 MPEG-4 Part 4 Part 2 载荷支持（仅覆盖本地文件源）
- MPEG-1/MPEG-2 视频及其他非 NALU 编码的接入

## 路线图阶段

本 change 为已交付阶段的增量增强，不对应新阶段编号，横跨：
- 阶段 3 `core-manscdp-and-ps`（PS 封装 PSM 声明）
- 阶段 8 `media-sources`（mp4 demux mp4v 轨道）
- 阶段 14 `web-management-ui` 关联的 `flv-media-gateway`（预览降级语义）

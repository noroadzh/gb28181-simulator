# Tasks

## 1. mp4v 解复用（media-sources）

- [x] 1.1 `internal/adapter/media/mp4_demuxer.go`：`parseSampleEntries` 新增 `mp4v` 分支——复用 `walkDescriptorsFrom` 提取 esds DecoderSpecificInfo（tag 0x05）为轨道 config，`codec="mp4v"`、`nalLenSize=0`；无 esds/DSI 为空时返回清晰错误。验证：构造含/不含 esds 的 sample entry 单测通过
- [x] 1.2 `internal/adapter/media/mp4_demuxer.go`：帧输出路径为 `mp4v` 增加"原样透传 + 每循环首样本前置 config + 样本缺起始码时补 `00 00 01 B6`"分支。验证：解复用断言首帧以 config 开头、PTS 为 90 kHz 容器时间戳
- [x] 1.3 准备 `internal/adapter/media/testdata/mpeg4-part2.mp4`：优先 `ffmpeg -f lavfi -i testsrc=duration=1:size=176x144:rate=12 -c:v mpeg4` 生成后入库，否则程序化构造最小 mp4v MP4。验证：文件存在且被新测试成功解复用
- [x] 1.4 `internal/adapter/media/mp4_demuxer_test.go` 新增 mp4v 用例：轨道解析、config 前置、起始码补齐、Loop 回卷重新前置、缺 esds 报错。验证：`go test ./internal/adapter/media/ -run MPEG4` 全绿

## 2. PSM 声明（core-manscdp-and-ps）

- [x] 2.1 `internal/adapter/media/ps_packetizer.go`：新增 CRC-32/MPEG-2（poly 0x04C11DB7）实现与已知向量单测。验证：测试向量校验通过
- [x] 2.2 `internal/adapter/media/ps_packetizer.go`：按首个视频 ES 帧起始码惰性判定 stream_type（0x10/0x1B/0x24），构造 PSM（`00 00 01 BC`，ISO 13818-1 §2.5.4 布局 + CRC）并在该帧 PS 输出前插入，每流仅一次；探测失败时不发 PSM。实现前核实 `MediaService` 每流新建 packetizer 实例并在状态注释中写明前提。验证：`go test ./internal/adapter/media/ -run PSM` 全绿
- [x] 2.3 新增 `internal/adapter/media/ps_packetizer_psm_test.go`：PSM 字节布局与 CRC、MPEG-4→0x10/H.264→0x1B/H.265→0x24 判定、每流仅一次、`PSDepacketizer` 与 `ParsePS` 对带 PSM 流的解析不受影响。验证：`go test ./internal/adapter/media/ ./internal/app/streaming/` 相关用例全绿

## 3. FLV 网关识别与降级（flv-media-gateway）

- [x] 3.1 `internal/app/streaming/flv.go`：`VideoCodec` 新增 `CodecMPEG4`（String="mpeg4"）；`DetectCodec` 先判 MPEG-4 起始码特征（0xB0/0xB3/0xB5/0xB6、0x20~0x2F）再判 AVC/HEVC。验证：新增单测覆盖 MPEG-4 识别与既有 AVC/HEVC 用例不变
- [x] 3.2 `internal/app/streaming/gateway.go`：`runPipeline` 检测到 `CodecMPEG4` 时写 error 日志并终止会话，不输出 AVC/HEVC tag。验证：网关降级行为测试通过（日志 + 流关闭 + 无视频 tag）

## 4. 既有测试迁移与全量回归

- [x] 4.1 `internal/app/streaming/multicodec_test.go`：`TestParsePSMPEG4Source`/`TestMulticodecFLV` 的 MPEG-4 用例改为预期成功（demux→PS→ParsePS 识别为 mpeg4），移除 `/tmp` 依赖改用 testdata 素材。验证：相关用例不再 Skip
- [x] 4.2 全量回归：`go build ./... && go vet ./... && go test ./...` 全绿，确认 H.264/HEVC 全链路与 SDP 生成行为不变。验证：CI 等价命令本地通过

## 5. 归档

- [x] 5.1 `openspec validate --strict` 通过、code review 后按 openspec 流程归档并同步主 specs。验证：`openspec validate --strict --changes` 无错误，归档目录与 specs 更新齐全

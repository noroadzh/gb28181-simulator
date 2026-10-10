// Package media — PS/RTP packetizers and media sources.
//
// All concrete implementations of the media ports live here:
//   - PS packetizer (ES → PS)
//   - PS depacketizer (PS → ES)
//   - RTP packetizer (PS → RTP)
//   - RTP depacketizer (RTP → PS)
//   - Four media sources (file, RTSP, HLS, synthetic)
//
// The app layer (internal/app/) depends only on the ports in
// internal/domain/port/media.go; this package has no upstream dependencies
// except internal/domain/model and internal/domain/port.
package media

import (
	"encoding/binary"
	"fmt"
	"log/slog"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// defaultMuxRate is the nominal stream bit-rate in bits per second.
// 9_000_000 = 9 Mbps, a safe default that fits most SD/HD streams.
// The MPEG-2 program_mux_rate field is measured in 50-byte units,
// so we convert bits/s → bytes/s → 50-byte units: muxRate/8/50.
const defaultMuxRate = 9000000

// PSPacketizer turns ES frames into program-stream packets.
//
// Every ES frame becomes one PS frame. The PS frame always starts with
//
//	00 00 01 BA <pack header>
//	00 00 01 Ex <PES header with PTS>   (E0 = video, C0 = audio)
//	<ES payload>
type PSPacketizer struct {
	muxRate uint32
	// agg is the optional error aggregator. nil disables aggregation; the
	// Record method is itself nil-safe but skipping the call entirely keeps
	// the hot loop cheaper.
	agg *ErrorAggregator
	// psm 跟踪 Program Stream Map（ISO/IEC 13818-1 §2.5.4）的声明状态：
	// 首个可识别编码的视频帧之前插入一次 PSM，此后不再发送。实例与 PS 流
	// 一一对应（MediaService.SubscribePS 每次订阅经工厂新建实例），且只在
	// 单个读帧 goroutine 内被调用，因此该状态无需加锁。
	psm psmState
}

// psmState 是 PSM 声明的惰性状态机。
type psmState uint8

const (
	// psmPending 尚未遇到视频帧，等待在首个视频帧上探测编码类型。
	psmPending psmState = iota
	// psmSent PSM 已随首个视频帧插入，后续帧不再插入。
	psmSent
	// psmUnsupported 首个视频帧编码无法识别，按"宁缺毋错"原则不声明 PSM，
	// 且不再重试（与不发送 PSM 的旧行为等价）。
	psmUnsupported
)

// stream_type 取值，见 ISO/IEC 13818-1 表 2-29 "Stream type assignments"。
const (
	streamTypeMPEG4Visual byte = 0x10 // MPEG-4 Visual（ISO/IEC 14496-2）
	streamTypeAVC         byte = 0x1B // AVC（ITU-T H.264 | ISO/IEC 14496-10）
	streamTypeHEVC        byte = 0x24 // HEVC（ITU-T H.265 | ISO/IEC 23008-2）
)

// psmESVideoID 是 PSM elementary_stream_map 中视频条目的
// elementary_stream_id，与 PES stream_id 0xE0 一致。
const psmESVideoID byte = 0xE0

// NewPSPacketizer returns a PSPacketizer ready to emit PS frames. A nil
// aggregator disables error aggregation (the Packetize hot loop returns the
// original error without recording it).
func NewPSPacketizer(agg *ErrorAggregator) *PSPacketizer {
	return &PSPacketizer{muxRate: defaultMuxRate, agg: agg}
}

// streamIDForKind returns the MPEG-2 PES stream_id for the given frame kind.
func streamIDForKind(kind model.ESFrameKind) byte {
	if kind == model.ESFrameAudio {
		return 0xC0 // audio stream
	}
	return 0xE0 // video stream (default)
}

// detectVideoStreamType 通过 ES 载荷首起始码后的特征字节判定视频流类型。
//
// 决策依据（ISO/IEC 13818-1 表 2-29 / ITU-T H.264 §7.3.1 / H.265 §7.3.1.1）：
//   - 0x10  : MPEG-4 Visual 视频起始码。约定：0xB0=VISUAL_OBJ_SEQ、
//     0xB3=VISUAL_OBJ、0xB5/0xB6=VIDEO_OBJ_LAYER_*，以及 0x20~0x2F 的
//     VOL 扩展（FOURCC mp4v 中以这些 VOP/对象层头开头）。
//   - 0x1B  : AVC，Annex B 起始码后字节 == 0x67 即 SPS NAL unit（NAL
//     header 0x67 = forbidden_zero_bit=0 + nal_ref_idc=3 + nal_unit_type=7）。
//     0x68=PPS、0x65=IDR、0x41/0x61/0x27 等其他 slice/sei 类型也归为 AVC。
//   - 0x24  : HEVC，NAL unit header 2 字节（H.265 §7.3.1.1）：
//     byte0 = forbidden_zero_bit(1) | nal_unit_type(6) | nuh_layer_id_hi(1)，
//     byte1 = nuh_layer_id_lo(5) | nuh_temporal_id_plus1(3)。
//     nal_unit_type = (byte0 & 0x7E) >> 1。流首帧典型 NAL：VPS(32)→0x40、
//     SPS(33)→0x42、PPS(34)→0x44、IDR_W_RADL(19)→0x26、IDR_N_LP(20)→0x28。
//     判据：forbidden_zero_bit=0 且 nal_unit_type ∈ {32,33,34,19,20} 且
//     nuh_temporal_id_plus1 = byte1&0x07 ≥ 1（H.265 §7.4.2 强制要求）。
//
// 判定顺序 HEVC → MPEG-4 → AVC：HEVC 优先是因为 IDR_W_RADL(0x26)/IDR_N_LP(0x28)
// 落在 MPEG-4 VOL 区间 0x20~0x2F 内，靠 nuh_temporal_id_plus1≥1 消歧。
//
// 返回 0 表示无法识别（"宁缺毋错"，调用方应回退到不发送 PSM 的旧行为）。
func detectVideoStreamType(payload []byte) byte {
	_, off := skipAnnexBStartCode(payload)
	if off < 0 || off >= len(payload) {
		return 0
	}
	b0 := payload[off]

	// HEVC：2 字节 NAL header（H.265 §7.3.1.1）。先于 MPEG-4 判定，以消歧
	// 0x26/0x28 与 VOL 区间 0x20~0x2F 的重叠。
	if b0&0x80 == 0 && off+1 < len(payload) {
		hevcType := (b0 & 0x7E) >> 1
		b1 := payload[off+1]
		// nuh_temporal_id_plus1 = b1 & 0x07，H.265 §7.4.2 规定必须 ≥ 1。
		if b1&0x07 != 0 {
			switch hevcType {
			case 32, 33, 34, // VPS, SPS, PPS
				19, 20: // IDR_W_RADL, IDR_N_LP
				return streamTypeHEVC
			}
		}
	}

	// MPEG-4 Visual：visual_object_sequence / video_object 等起始码族。
	switch b0 {
	case 0xB0, 0xB3, 0xB5, 0xB6:
		return streamTypeMPEG4Visual
	}
	if b0 >= 0x20 && b0 <= 0x2F {
		// video_object_layer_start_codes 扩展区间。
		return streamTypeMPEG4Visual
	}

	// AVC：1 字节 NAL header（H.264 §7.3.1）。nal_unit_type = b0 & 0x1F。
	// 取值 0=unspecified 不判定，避免与 HEVC byte0 冲突误判。
	if nalType := b0 & 0x1F; nalType >= 1 && nalType <= 23 {
		return streamTypeAVC
	}

	return 0
}

// skipAnnexBStartCode 跳过 Annex B 起始码（00 00 00 01 或 00 00 01），
// 返回起始码长度 scLen 与其后首个字节的偏移 off；找不到起始码时 off=-1。
func skipAnnexBStartCode(b []byte) (scLen, off int) {
	if len(b) < 4 {
		return 0, -1
	}
	if b[0] == 0x00 && b[1] == 0x00 && b[2] == 0x00 && b[3] == 0x01 {
		return 4, 4
	}
	if b[0] == 0x00 && b[1] == 0x00 && b[2] == 0x01 {
		return 3, 3
	}
	return 0, -1
}

// encodePSM 按 ISO/IEC 13818-1 §2.5.4 Program Stream Map 布局生成完整 PSM。
//
// 布局（大端，共 18 字节）：
//
//	[0:4]   00 00 01 BC                            program_stream_map_start_code
//	[4:6]   program_stream_map_length = 12        自身与起始码不计，含 CRC_32
//	[6]     current_next_indicator(1)=1            = '1'|'11'|version(5)=1 → 0xE1
//	        + reserved(2)='11' + program_stream_map_version(5)=1
//	[7]     reserved(7)='1111111' + marker_bit(1)=1 → 0xFF
//	[8:10]  program_stream_info_length = 0         不携带描述符
//	[10:12] elementary_stream_map_length = 2       1 个条目 × 2 字节
//	[12:14] stream_type(8) + elementary_stream_id(8)
//	        = 探测值 + 0xE0（视频 PES stream_id）
//	[14:18] CRC_32（附录 A 算法）                   覆盖 body [6:14]（version 起至
//	        最后条目字节，共 8 字节），不含 length 自身与 CRC 字段
//
// 本期仅声明视频流（stream_type=探测值，stream_id=0xE0），音频条目留待后续
// 变更补充（PSM 只需保证"不误报"，缺失条目不影响透明性）。
func encodePSM(videoStreamType byte) []byte {
	psm := make([]byte, 0, 18)
	psm = append(psm, 0x00, 0x00, 0x01, 0xBC) // program_stream_map_start_code
	psm = append(psm, 0x00, 0x0C)             // program_stream_map_length = 12（标志 2 + info_len 2 + es_map_len 2 + 条目 2 + CRC 4）
	psm = append(psm, 0xE1)                   // current_next=1 | reserved='11' | version=1
	psm = append(psm, 0xFF)                   // reserved='1111111' | marker_bit=1
	psm = append(psm, 0x00, 0x00)             // program_stream_info_length = 0
	psm = append(psm, 0x00, 0x02)             // elementary_stream_map_length = 2
	psm = append(psm, videoStreamType, psmESVideoID)
	// CRC 覆盖范围按标准：从 program_stream_map_length 字段之后的第一个字节（body 首字节）起，
	// 至 elementary_stream_map 最后一个条目字节止，共 8 字节，不含 length 自身与 CRC 字段。
	body := psm[6 : 6+12-4] // [6:14] version..最后条目
	crc := crc32MPEG2(body)
	psm = append(psm, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
	return psm
}

// crc32MPEG2 计算 CRC-32/MPEG-2 校验值：多项式 0x04C11DB7，初始值
// 0xFFFFFFFF，MSB-first，无输入/输出反转、无异或输出
// （参考 ISO/IEC 13818-1 附录 A）。
//
// 已知校验向量："123456789" → 0x0376E6E7。
func crc32MPEG2(data []byte) uint32 {
	const (
		poly   = 0x04C11DB7
		init32 = 0xFFFFFFFF
	)
	crc := uint32(init32)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// Packetize returns one PSFrame wrapping the ES frame. The ES payload is
// taken by reference; the returned PSFrame Payload includes the PS header
// bytes so the caller does not need to prepend anything.
//
// PSM 注入：每流首次遇到可识别视频帧时，PSM 单元（00 00 01 BC … CRC32）
// 作为流级独立单元置于该帧 pack header 之前。接收端扫描逻辑只识别
// 00 00 01 BA 与 00 00 01 E0/0xC0，0xBC 天然透明（详见 ps_depacketizer.go
// 与 internal/app/streaming/flv.go ParsePS）。
func (p *PSPacketizer) Packetize(frame model.ESFrame) (model.PSFrame, error) {
	if err := frame.Validate(); err != nil {
		if p.agg != nil {
			p.agg.Record("ps-mux", err)
		}
		return model.PSFrame{}, err
	}

	// Pack header (12 bytes total, preceded by 00 00 01 BA).
	muxVal := p.muxRate / 8 / 50 // bits/s -> bytes/s -> 50-byte units
	packHeader := make([]byte, 12)
	packHeader[0] = 0x44                                    // '01' + SCR=0 + marker + 0
	binary.BigEndian.PutUint32(packHeader[1:5], 0x00010001) // SCR=0 with mandatory marker bits
	binary.BigEndian.PutUint16(packHeader[5:7], uint16(muxVal))

	// PES header (everything after PES_packet_length).
	//
	// MPEG-2 PES header layout (after PES_packet_length, 2 bytes):
	//   [2]  : '10' marker + flags (1 byte)
	//   [3]  : PTS_DTS_flags etc. (1 byte)
	//   [4]  : PES_header_data_length (1 byte)
	//   [5:] : optional fields (PES_header_data_length bytes)
	//
	// With PTS-only (no DTS/ESCR/etc.), optional fields = 5-byte PTS.
	// So PES header total = 3 fixed bytes + 5 PTS bytes = 8 bytes.
	// PES_packet_length = 8 + esLen (counts everything after itself).
	esLen := len(frame.Payload)
	const pesHeaderDataLen = 5 // PTS is 5 bytes; no DTS, no ESCR, etc.
	const pesFixedLen = 3      // marker byte + flags byte + header_data_length byte
	pesHeader := make([]byte, 2+pesFixedLen+pesHeaderDataLen)
	// PES_packet_length: counts bytes AFTER this field (header + ES payload).
	binary.BigEndian.PutUint16(pesHeader[0:2], uint16(pesFixedLen+pesHeaderDataLen+esLen))
	pesHeader[2] = 0x80             // '10' marker
	pesHeader[3] = 0x80             // PTS only (PTS_DTS_flags = '10')
	pesHeader[4] = pesHeaderDataLen // number of bytes of optional fields after this

	// Encode PTS in 90 kHz domain into 5-byte MPEG-2 format.
	pts := frame.PTS
	pesHeader[5] = 0x21 | byte((pts>>29)&0x0E)
	pesHeader[6] = byte((pts >> 22) & 0xFF)
	pesHeader[7] = ((byte((pts >> 15) & 0x7F)) << 1) | 0x01
	pesHeader[8] = byte((pts >> 7) & 0xFF)
	pesHeader[9] = (byte(pts&0x7F) << 1) | 0x01

	// Assemble full PS frame.
	//
	// PSM 注入（方案 a）：PSM 作为流级独立单元置于该帧 PS 包的最前端，
	// 位于 pack header 之前。ISO/IEC 13818-1 §2.5.4 允许 PSM 出现在流中
	// 任意 pack 之间；置于 pack 之前使 `00 00 01 BA` 之后的字节序列与
	// 旧版输出完全一致，PSDepacketizer（按 BA 定位帧）与 flv.ParsePS
	// （按 00 00 01 + E0/C0 扫描 PES）均天然透明。
	ps := make([]byte, 0, 4+12+4+len(pesHeader)+esLen)
	if psmBytes := p.maybeBuildPSM(frame); len(psmBytes) > 0 {
		ps = append(ps, psmBytes...)
	}
	ps = append(ps, 0x00, 0x00, 0x01, 0xBA) // pack start code
	ps = append(ps, packHeader...)
	ps = append(ps, 0x00, 0x00, 0x01, streamIDForKind(frame.Kind)) // video (0xE0) or audio (0xC0)
	ps = append(ps, pesHeader...)
	ps = append(ps, frame.Payload...)

	return model.PSFrame{Payload: ps, PTS: pts}, nil
}

// maybeBuildPSM 在首个可识别视频帧上惰性生成 PSM，非视频帧/已发送/无法
// 识别返回 nil。返回的字节序列是完整的 00 00 01 BC … CRC 单元。
func (p *PSPacketizer) maybeBuildPSM(frame model.ESFrame) []byte {
	if p.psm != psmPending {
		return nil
	}
	if frame.Kind == model.ESFrameAudio {
		// 音频帧不触发 PSM 探测（本期只声明视频）。Kind 为零值 "" 的帧与
		// streamIDForKind 的语义一致，视为视频。
		return nil
	}
	st := detectVideoStreamType(frame.Payload)
	if st == 0 {
		// 编码未知：宁缺毋错，永久不再尝试发 PSM。
		p.psm = psmUnsupported
		// 一次性 debug 日志：不可识别时提示一次，便于排错。
		// 复用包级 aggregatorLog 模式（与 error_aggregator 保持一致），
		// log 为 nil 时退化为 slog.Default()。
		psmLogOnce("psm: video stream type undetected, skip Program Stream Map",
			"es_len", len(frame.Payload))
		return nil
	}
	p.psm = psmSent
	psmLogOnce("psm: emit Program Stream Map",
		"stream_type", fmt.Sprintf("0x%02X", st))
	return encodePSM(st)
}

// psmLogOnce 输出一次性 debug 日志。复用 error_aggregator 的包级 logger
// 模式（aggregatorLog）保持一致性；nil 时回退到 slog.Default。
func psmLogOnce(msg string, args ...any) {
	if aggregatorLog == nil {
		slog.Default().Debug(msg, args...)
		return
	}
	aggregatorLog.Debug(msg, args...)
}

// Header returns the MPEG-2 PS system header prefix (00 00 01 BB + body).
// Callers prepend it before the first PS frame if the transport requires it.
func (p *PSPacketizer) Header() []byte {
	// Minimal system header: 14 bytes after 00 00 01 BB.
	sh := make([]byte, 18)
	sh[0] = 0x00
	sh[1] = 0x00
	sh[2] = 0x01
	sh[3] = 0xBB                            // system header start code
	binary.BigEndian.PutUint16(sh[4:6], 14) // header length = 14
	// Rate bound = 1 Mbps, 1 video stream, 1 audio stream
	sh[6] = 0x80
	sh[7] = 0x01
	sh[8] = 0x00
	sh[9] = 0x01 // 1 video stream bound
	sh[10] = 0xE0
	sh[11] = 0x01 // 1 audio stream bound
	sh[12] = 0xC0
	return sh
}

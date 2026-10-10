// Package streaming provides the HTTP-FLV streaming gateway for browser playback.
package streaming

import (
	"encoding/binary"
)

// FLV tag types.
const (
	flvTagTypeAudio  = 8
	flvTagTypeVideo  = 9
	flvTagTypeScript = 18
)

// FLV video frame types.
const (
	flvFrameKey       = 1 // keyframe (IDR)
	flvFrameInter     = 2 // inter frame
	flvFrameDispo     = 3 // disposable inter (deprecated)
	flvFrameGenKey    = 4 // generated keyframe
	flvFrameVideoInfo = 5
)

// FLV AVC/HEVC packet types.
const (
	flvAVCSequenceHeader = 0 // AVC sequence header (SPS/PPS)
	flvAVCNALU           = 1 // AVC NALU
	flvAVCEOS            = 2 // AVC end of sequence
	flvHEVCSequenceHeader = 0
	flvHEVCDecoderConfigRecord = 0
	flvHEVCNALU           = 1 // HEVC NALU
	flvHEVCEOS            = 2 // HEVC end of sequence
)

// FLV codec IDs (lower 4 bits of frame-type byte).
const (
	flvCodecAVC = 7
	flvCodecHEVC = 12 // HEVC in FLV
)

// VideoCodec distinguishes the video coding format so the muxer knows how to
// frame FLV tags and which sequence-header format to emit.
type VideoCodec int

const (
	CodecUnknown VideoCodec = iota
	CodecAVC               // H.264
	CodecHEVC              // H.265 / HEVC
)

// String returns a stable lowercase name for log/metric keys.
func (c VideoCodec) String() string {
	switch c {
	case CodecAVC:
		return "avc"
	case CodecHEVC:
		return "hevc"
	default:
		return "unknown"
	}
}

// FLVMuxer encodes PS frames (MPEG-2 Program Stream) into FLV tags for
// browser playback via flv.js. It is stateful per subscriber: one instance
// per session goroutine.
//
// The encoder handles the FLV-to-H.264 bridging:
//   - FLV header: pre-written once per session
//   - AVC sequence header: emitted on the first frame (SPS/PPS extracted from
//     the first video NALU)
//   - Video tags: one FLV tag per video NALU, timestamp from PTS/90000
//
// The PS frame layout (as emitted by PSPacketizer):
//
//	00 00 01 BA <pack header>      (12 bytes)
//	00 00 01 E0 <PES header>       (variable, min 19 bytes)
//	<video NALU payload>
//
// The PES header contains the PTS (90 kHz). We parse the PTS from the PES
// header to timestamp FLV tags accurately.
//
// The video NALU is H.264 (Annex B start codes 00 00 00 01 or 00 00 01).
// For FLV, we strip the start code and prepend the NALU length (4 bytes,
// big-endian) as required by the AVC format in FLV.
type FLVMuxer struct {
	// channelKey identifies this stream for logging/metrics.
	channelKey string

	// sentHeader tracks whether the FLV header has been sent (written to
	// the writer passed to EncodeHeader).
	sentHeader bool

	// sentSeqHeader tracks whether the AVC sequence header (SPS/PPS) has
	// been sent. It is sent on the first video NALU of an IDR access unit.
	sentSeqHeader bool

	// lastTimestamp is the FLV timestamp (ms) of the previous tag.
	lastTimestamp uint32

	// frameBuf accumulates bytes for one FLV tag before WriteTag is called.
	frameBuf []byte
}

// NewFLVMuxer creates a new FLV encoder for a given stream identifier.
func NewFLVMuxer(channelKey string) *FLVMuxer {
	return &FLVMuxer{channelKey: channelKey}
}

// FLVHeader is the standard FLV file header (9 bytes) + First PreviousTagSize (4 bytes).
// This is prepended to every HTTP-FLV stream.
var FLVHeader = []byte{
	// FLV header (9 bytes)
	0x46, 0x4C, 0x56,       // "FLV"
	0x01,                   // version 1
	0x01,                   // flags: 0x01 = video present (no audio in basic streaming)
	0x00, 0x00, 0x00, 0x09, // DataOffset = 9 (4 bytes, big-endian)
	// PreviousTagSize0 (must be 0)
	0x00, 0x00, 0x00, 0x00,
}

// ParsePS parses a PS frame produced by PSPacketizer and returns its
// H.264 NALU payload (stripped of Annex B start codes) and timestamp (ms).
//
// The PS frame layout:
//
//	00 00 01 BA <pack header (12 bytes)>
//	00 00 01 E0 <PES header> <video payload>
//
// The PES header format (after stream_id 0xE0, after PES_packet_length):
//
//	[2] PES_packet_length (not reliable)
//	[1] '10' marker + PES_scrambling_control(00) + PES_priority(0) + data_alignment(0) + copyright(0) + original_or_copy(0)
//	[1] '11' + PTS_DTS_flags(10 for PTS only) + ESCR_flag(0) + ES_rate_flag(0) + DSM_trick_mode(0) +
//	    additional_copy_info_flag(0) + PES_CRC_flag(0) + PES_extension_flag(0)
//	[1] PES_header_data_length (number of optional fields)
//	PTS: [5] in MPEG-2 timestamp format (33-bit, 3 reserved bits, marker bits)
//
// For PTS-only packets the optional fields are just the 5-byte PTS field.
func ParsePS(psPayload []byte) (nalus [][]byte, timestampMS uint32, ok bool) {
	if len(psPayload) < 20 {
		return nil, 0, false
	}

	// Find PES header start: scan for 00 00 01 E0 (video) or 00 00 01 C0
	// (audio) after the pack header. Pack header is 00 00 01 BA + 12 bytes,
	// so PES follows at offset 16 in the common case; we still scan because
	// some pack headers can be longer in future extensions.
	pi := 0
	for pi < len(psPayload)-4 {
		if psPayload[pi] == 0x00 && psPayload[pi+1] == 0x00 &&
			psPayload[pi+2] == 0x01 {
			b3 := psPayload[pi+3]
			if (b3 & 0xF0) == 0xE0 || (b3 & 0xE0) == 0xC0 {
				pi += 4
				break
			}
		}
		pi++
	}
	if pi >= len(psPayload) {
		return nil, 0, false
	}

	// PES header: skip PES_packet_length (2 bytes), then parse PES header structure.
	// 需要 5 字节：PES_packet_length(2) + flags(1) + flags(1) + PES_header_data_length(1)。
	if pi+5 > len(psPayload) {
		return nil, 0, false
	}
	pi += 2 // skip PES_packet_length (not reliable, use remaining bytes)

	// byte 0: '10' + flags
	if psPayload[pi]&0xC0 != 0x80 {
		return nil, 0, false
	}
	pi++

	// byte 1: PTS_DTS_flags + other flags
	ptsDtsFlags := (psPayload[pi] >> 6) & 0x03
	pi++

	// byte 2: PES_header_data_length
	headerLen := int(psPayload[pi])
	pi++

	if pi+headerLen > len(psPayload) {
		return nil, 0, false
	}

	var pts uint64
	if ptsDtsFlags >= 2 {
		// PTS is present. PTS format: '0011' + 3 reserved + 3 marker bits + 15 bits + 1 marker + 15 bits + 1 marker + 15 bits + 1 marker
		// Total 5 bytes.
		if pi+5 > len(psPayload) {
			return nil, 0, false
		}
		// PTS starts at bit 4 of byte 0 (after '0011' prefix).
		b0 := psPayload[pi]
		b1 := psPayload[pi+1]
		b2 := psPayload[pi+2]
		b3 := psPayload[pi+3]
		b4 := psPayload[pi+4]

		// Extract 33-bit PTS from 5 bytes:
		// byte 0: [0-3]=0011, [4-6]=PTS[32-30], [7]=marker
		// byte 1: [0-7]=PTS[29-22]
		// byte 2: [0]=PTS[21], [1-7]=PTS[20-15] with top bit from b2
		// byte 3: [0-6]=PTS[14-8], top bit from b3
		// byte 4: [0-6]=PTS[7-1], top bit from b4
		// Actually simpler: just shift bytes
		pts = uint64((uint64(b0)&0x0E)<<29 |
			uint64(b1)<<22 |
			uint64(b2&0xFE)<<14 |
			uint64(b3&0xFE)<<7 |
			uint64(b4&0xFE)>>1)
		pi += 5
	}

	// Video payload starts after PES header data.
	videoStart := pi
	if videoStart >= len(psPayload) {
		return nil, 0, false
	}

	// Extract NALUs from video payload.
	// Annex B start code: 00 00 01 or 00 00 00 01.
	for videoStart < len(psPayload) {
		// Find start code.
		if videoStart+3 < len(psPayload) &&
			psPayload[videoStart] == 0x00 &&
			psPayload[videoStart+1] == 0x00 {
			var scLen int
			if videoStart+4 < len(psPayload) && psPayload[videoStart+2] == 0x01 {
				scLen = 3
			} else if videoStart+5 < len(psPayload) &&
				psPayload[videoStart+2] == 0x00 &&
				psPayload[videoStart+3] == 0x01 {
				scLen = 4
			} else {
				videoStart++
				continue
			}

			naluStart := videoStart + scLen
			// Find next start code.
			nextStart := naluStart
			for nextStart+3 < len(psPayload) {
				if psPayload[nextStart] == 0x00 &&
					psPayload[nextStart+1] == 0x00 &&
					(psPayload[nextStart+2] == 0x01 ||
						(nextStart+4 < len(psPayload) &&
							psPayload[nextStart+2] == 0x00 &&
							psPayload[nextStart+3] == 0x01)) {
					break
				}
				nextStart++
			}

			naluType := psPayload[naluStart] & 0x1F
			// HEVC NALU type: upper 7 bits of byte[1] (encoded as (byte[1] & 0x7E) >> 1)
			hevcNaluType := byte(0)
			if videoStart+1 < naluStart+naluStart+1 && nextStart-naluStart >= 2 {
				hevcNaluType = (psPayload[naluStart+1] & 0x7E) >> 1
			}
			nal := make([]byte, 4+nextStart-naluStart)
			// FLV AVC/HEVC format: 4-byte NALU length (big-endian) + NALU data (no start code)
			binary.BigEndian.PutUint32(nal, uint32(nextStart-naluStart))
			copy(nal[4:], psPayload[naluStart:nextStart])

			// Keep video NALUs. H.264 types 1..23 (1=non-IDR, 5=IDR, 7=SPS, 8=PPS).
			// HEVC types 0..47 (32=VPS, 33=SPS, 34=PPS, 19/20=IDR, 1=non-IDR).
			isAVC := naluType >= 1 && naluType <= 23
			isHEVC := hevcNaluType <= 47 && (hevcNaluType == 0 || (hevcNaluType >= 1 && hevcNaluType <= 40) || hevcNaluType == 32 || hevcNaluType == 33 || hevcNaluType == 34)
			if isAVC || isHEVC {
				nalus = append(nalus, nal)
			}

			videoStart = nextStart
			continue
		}
		videoStart++
	}

	// Timestamp in milliseconds: PTS is in 90 kHz units.
	timestampMS = uint32(pts * 1000 / 90000)
	return nalus, timestampMS, true
}

// BuildAVCSequenceHeader builds the FLV AVC sequence header tag from
// SPS and PPS NALUs (already stripped of start codes).
func BuildAVCSequenceHeader(sps, pps []byte) []byte {
	// AVCDecoderConfigurationRecord:
	// - configurationVersion: 1
	// - AVCProfileIndication: from SPS[1]
	// - profileCompatibility: from SPS[3]
	// - AVCLevelIndication: from SPS[4]
	// - lengthSizeMinusOne: 3 (4 bytes NALU length)
	// - numOfSequenceParameterSets: 1
	// - sequenceParameterSetLength: len(sps)
	// - sequenceParameterSetNALUnit: sps
	// - numOfPictureParameterSets: 1
	// - pictureParameterSetLength: len(pps)
	// - pictureParameterSetNALUnit: pps

	// AVCDecoderConfigurationRecord layout (ISO/IEC 14496-15):
	//   configurationVersion      1 byte
	//   AVCProfileIndication      1 byte
	//   profileCompatibility      1 byte
	//   AVCLevelIndication        1 byte
	//   6-bit reserved + lengthSizeMinusOne (2 bits)  1 byte
	//   1-bit reserved + numOfSequenceParameterSets   1 byte
	//   SPS: 16-bit length + NALU data
	//   1-bit reserved + numOfPictureParameterSets    1 byte
	//   PPS: 16-bit length + NALU data
	recordLen := 5 + 1 + (2 + len(sps)) + 1 + (2 + len(pps))
	// 11 bytes tag header + body (1 frameType + 1 avcType + 3 compTime + recordLen) + 4 bytes PreviousTagSize
	dataSize := 1 + 1 + 3 + recordLen
	tag := make([]byte, 11+dataSize+4)
	tag[0] = flvTagTypeVideo // TagType = 9 (video)
	tag[1] = byte(dataSize >> 16)
	tag[2] = byte(dataSize >> 8)
	tag[3] = byte(dataSize)
	// Timestamp (3 bytes, big-endian) + 1 byte timestamp extended = 0
	tag[4] = 0
	tag[5] = 0
	tag[6] = 0
	tag[7] = 0
	// StreamID (3 bytes) = 0
	tag[8] = 0
	tag[9] = 0
	tag[10] = 0

	// Tag body:
	offset := 11
	// Video data: frameType(4)=5(sensor) | codecID(4)=7(AVC) => 0x17 = keyframe + AVC
	tag[offset] = 0x17
	offset++
	// AVC packet type: 0 = sequence header
	tag[offset] = flvAVCSequenceHeader
	offset++
	// CompositionTime offset (3 bytes) = 0
	tag[offset] = 0
	tag[offset+1] = 0
	tag[offset+2] = 0
	offset += 3

	// AVCDecoderConfigurationRecord
	tag[offset] = 1 // version
	offset++
	// ISO/IEC 14496-15: SPS bytes 1-3 are profile/level fields.
	// Guard against malformed SPS (needs at least 4 bytes total).
	if len(sps) >= 4 {
		tag[offset] = sps[1] // AVCProfileIndication
		offset++
		tag[offset] = sps[2] // profileCompatibility
		offset++
		tag[offset] = sps[3] // AVCLevelIndication
		offset++
	} else {
		// Fallback: zero out the fields so the record is well-formed.
		tag[offset], tag[offset+1], tag[offset+2] = 0, 0, 0
		offset += 3
	}
	tag[offset] = 0xFF // lengthSizeMinusOne = 3 (means 4 bytes NALU length)
	offset++

	// SPS
	tag[offset] = 0xE1 // numOfSequenceParameterSets = 1, reserved 11111
	offset++
	binary.BigEndian.PutUint16(tag[offset:], uint16(len(sps)))
	offset += 2
	copy(tag[offset:], sps)
	offset += len(sps)

	// PPS
	tag[offset] = 1 // numOfPictureParameterSets = 1
	offset++
	binary.BigEndian.PutUint16(tag[offset:], uint16(len(pps)))
	offset += 2
	copy(tag[offset:], pps)
	offset += len(pps)

	// PreviousTagSize (4 bytes, big-endian) — allocated in make([]byte, …+4)
	prevSize := uint32(dataSize + 11)
	binary.BigEndian.PutUint32(tag[offset:], prevSize)

	return tag
}

// BuildVideoTag builds one FLV video tag from an H.264 / HEVC NALU.
// nalu must be in length-prefixed format (4-byte length + data).
// isKey should be true for IDR frames.
// timestamp is in milliseconds.
// codec selects whether to write an AVC (7) or HEVC (12) tag.
func BuildVideoTag(nalu []byte, isKey bool, timestampMS uint32, codec VideoCodec) []byte {
	naluLen := len(nalu)
	dataSize := 1 + 1 + 3 + naluLen // frameType/codec + AVCType + compTime + NALU

	tag := make([]byte, 11+dataSize+4)
	// Tag header
	tag[0] = flvTagTypeVideo
	tag[1] = byte(dataSize >> 16)
	tag[2] = byte(dataSize >> 8)
	tag[3] = byte(dataSize)
	tag[4] = byte(timestampMS >> 16)
	tag[5] = byte(timestampMS >> 8)
	tag[6] = byte(timestampMS)
	tag[7] = byte(timestampMS >> 24) // timestamp extended
	tag[8] = 0
	tag[9] = 0
	tag[10] = 0

	offset := 11
	// Frame type + codec: keyframe(1)/inter(2) + AVC(7) => 0x17/0x27
	//                          keyframe(1)/inter(2) + HEVC(12) => 0x1C/0x2C
	if codec == CodecHEVC {
		if isKey {
			tag[offset] = 0x1C
		} else {
			tag[offset] = 0x2C
		}
	} else {
		if isKey {
			tag[offset] = 0x17
		} else {
			tag[offset] = 0x27
		}
	}
	offset++
	if codec == CodecHEVC {
		tag[offset] = flvHEVCNALU
	} else {
		tag[offset] = flvAVCNALU
	}
	offset++
	// CompositionTime offset (for B-frame compatibility)
	tag[offset] = 0
	tag[offset+1] = 0
	tag[offset+2] = 0
	offset += 3
	// NALU payload
	copy(tag[offset:], nalu)
	offset += naluLen

	// PreviousTagSize
	binary.BigEndian.PutUint32(tag[offset:], uint32(dataSize+11))

	return tag
}

// CodecFromFLVTagHeader extracts the codec from the first byte of an FLV
// video tag body (the byte that combines FrameType and CodecID). Returns
// CodecAVC for AVC, CodecHEVC for HEVC, CodecUnknown otherwise.
func CodecFromFLVTagHeader(b byte) VideoCodec {
	codec := VideoCodec(b & 0x0F)
	switch codec {
	case flvCodecAVC:
		return CodecAVC
	case flvCodecHEVC:
		return CodecHEVC
	default:
		return CodecUnknown
	}
}

// IsKeyframe returns true if the NALU type is IDR (5).
func IsKeyframe(naluType byte) bool {
	return naluType == 5
}

// NALUType returns the H.264 NALU type from a length-prefixed NALU.
func NALUType(nalu []byte) byte {
	if len(nalu) < 5 {
		return 0
	}
	return nalu[4] & 0x1F
}

// HEVCNALUType returns the HEVC NALU type from a length-prefixed NALU.
// HEVC uses byte[4] & 0x7E (upper 7 bits of second byte), shifted right 1.
func HEVCNALUType(nalu []byte) byte {
	if len(nalu) < 6 {
		return 0
	}
	return (nalu[4] & 0x7E) >> 1
}

// IsHEVCSPSPPS returns true for HEVC VPS(32)/SPS(33)/PPS(34) NALUs.
func IsHEVCSPSPPS(nalu []byte) bool {
	t := HEVCNALUType(nalu)
	return t == 32 || t == 33 || t == 34
}

// IsHEVCKeyframe returns true for HEVC IDR_W_RADL / IDR_N_LP (type 19/20).
func IsHEVCKeyframe(nalu []byte) bool {
	t := HEVCNALUType(nalu)
	return t == 19 || t == 20
}

// DetectCodec infers the VideoCodec from a list of NALUs by their length-prefixed
// header byte(s). It returns CodecUnknown if the list is empty.
func DetectCodec(nalus [][]byte) VideoCodec {
	for _, nalu := range nalus {
		if len(nalu) >= 5 {
			// H.264: byte[4] & 0x1F in 1..23
			if t := NALUType(nalu); t >= 1 && t <= 23 {
				return CodecAVC
			}
		}
		if len(nalu) >= 6 {
			// HEVC: byte[4] upper 7 bits shifted right 1, in 0..63
			if t := HEVCNALUType(nalu); t <= 63 {
				return CodecHEVC
			}
		}
	}
	return CodecUnknown
}

// ExtractSPSPPS extracts SPS and PPS NALUs from a slice of NALUs.
// SPS = NALU type 7, PPS = NALU type 8. The returned bytes have their
// 4-byte length prefix stripped (FLV AVC format stores NALUs without it).
func ExtractSPSPPS(nalus [][]byte) (sps, pps []byte) {
	for _, nalu := range nalus {
		t := NALUType(nalu)
		if t == 7 && sps == nil {
			sps = nalu[4:] // strip 4-byte length prefix
		} else if t == 8 && pps == nil {
			pps = nalu[4:]
		}
		if sps != nil && pps != nil {
			break
		}
	}
	return
}

// ExtractHEVCDecoderConfig extracts VPS, SPS and PPS NALUs from a slice
// of NALUs (HEVC NALU types 32/33/34). Returns nil fields if absent.
func ExtractHEVCDecoderConfig(nalus [][]byte) (vps, sps, pps []byte) {
	for _, nalu := range nalus {
		t := HEVCNALUType(nalu)
		switch t {
		case 32:
			if vps == nil {
				vps = nalu[4:]
			}
		case 33:
			if sps == nil {
				sps = nalu[4:]
			}
		case 34:
			if pps == nil {
				pps = nalu[4:]
			}
		}
		if vps != nil && sps != nil && pps != nil {
			break
		}
	}
	return
}

// BuildHEVCSequenceHeader builds the FLV HEVCDecoderConfigurationRecord
// (a.k.a. HEVC sequence header) from the three parameter set NALUs.
func BuildHEVCSequenceHeader(vps, sps, pps []byte) []byte {
	// HEVCDecoderConfigurationRecord layout (ISO/IEC 14496-15 + flv.js extension):
	//   configurationVersion         (8) = 1
	//   general_profile_space        (2) | general_tier_flag (1) | general_profile_idc (5)
	//   general_profile_compatibility_flags (32) — read from SPS[2..5]
	//   general_constraint_indicator_flags    (48)
	//   general_level_idc                      (8)
	//   min_spatial_segmentation_idc           (16, 0xF000 reserved)
	//   parallelismType                        (8, 0)
	//   chromaFormatIdc                        (8, reserved 0xFC | val<<2)
	//   bitDepthLumaMinus8                     (8, reserved 0xF8 | val)
	//   bitDepthChromaMinus8                   (8, reserved 0xF8 | val)
	//   avgFrameRate                           (16)
	//   constantFrameRate|numTemporalLayers|temporalIdNested|lengthSizeMinusOne (8)
	//   numOfArrays                           (8) = 3
	//   per array: array_completeness|nalu_type (8), numNalus (16), naluLength(16), nalu
	// 固定头 23 字节；每个 array 固定开销 5 字节（nalu_type 1 + numNalus 2 + naluLength 2）。
	recordLen := 23 + 5 + len(vps) + 5 + len(sps) + 5 + len(pps)

	dataSize := 1 + 1 + 3 + recordLen
	tag := make([]byte, 11+dataSize+4)

	tag[0] = flvTagTypeVideo
	tag[1] = byte(dataSize >> 16)
	tag[2] = byte(dataSize >> 8)
	tag[3] = byte(dataSize)
	tag[4], tag[5], tag[6], tag[7] = 0, 0, 0, 0
	tag[8], tag[9], tag[10] = 0, 0, 0

	offset := 11
	// Frame type: 1 (keyframe) | codecID: 12 (HEVC) = 0x1C
	tag[offset] = 0x1C
	offset++
	// AVCPacketType for HEVC: 0 = HEVCDecoderConfigurationRecord
	tag[offset] = flvHEVCDecoderConfigRecord
	offset++
	// CompositionTime: 0
	tag[offset], tag[offset+1], tag[offset+2] = 0, 0, 0
	offset += 3

	// HEVCDecoderConfigurationRecord
	tag[offset] = 1 // configurationVersion
	offset++
	// general_profile_space(0) | tier_flag(0) | general_profile_idc(sps[1] from HEVC SPS)
	// HEVC SPS has profile_idc at byte 2; pass through with 0/0 prefix
	if len(sps) >= 3 {
		tag[offset] = sps[1] & 0x1F
	} else {
		tag[offset] = 0
	}
	offset++
	// general_profile_compatibility_flags: bytes 4..7 of SPS, but only
	// first 4 bytes are reliably populated. Fill with 0 for safety.
	tag[offset], tag[offset+1], tag[offset+2], tag[offset+3] = 0, 0, 0, 0
	offset += 4
	// general_constraint_indicator_flags: 6 bytes
	tag[offset], tag[offset+1], tag[offset+2] = 0, 0, 0
	tag[offset+3], tag[offset+4], tag[offset+5] = 0, 0, 0
	offset += 6
	// general_level_idc: SPS[12] if available
	if len(sps) >= 13 {
		tag[offset] = sps[12]
	} else {
		tag[offset] = 0
	}
	offset++
	// min_spatial_segmentation_idc: 0xF000 (reserved upper bits)
	tag[offset] = 0xF0
	tag[offset+1] = 0x00
	offset += 2
	// parallelismType: 0
	tag[offset] = 0xFC
	offset++
	// chromaFormatIdc: 0xFC | (1<<1) = 0xFE (chroma_format_idc=1, 4:2:0)
	tag[offset] = 0xFD
	offset++
	// bitDepthLumaMinus8: 0xF8 | 0
	tag[offset] = 0xF8
	offset++
	// bitDepthChromaMinus8: 0xF8 | 0
	tag[offset] = 0xF8
	offset++
	// avgFrameRate: 0 (unknown)
	tag[offset] = 0
	tag[offset+1] = 0
	offset += 2
	// constantFrameRate(0) | numTemporalLayers(0) | temporalIdNested(0) | lengthSizeMinusOne(3)
	// 0000 0000 1111 = 0x0F
	tag[offset] = 0x0F
	offset++
	// numOfArrays = 3
	tag[offset] = 3
	offset++

	// VPS array
	tag[offset] = 32 // array_completeness(1) | reserved(1) | NAL_unit_type(6)
	offset++
	binary.BigEndian.PutUint16(tag[offset:], 1) // numNalus
	offset += 2
	binary.BigEndian.PutUint16(tag[offset:], uint16(len(vps)))
	offset += 2
	copy(tag[offset:], vps)
	offset += len(vps)

	// SPS array
	tag[offset] = 33
	offset++
	binary.BigEndian.PutUint16(tag[offset:], 1)
	offset += 2
	binary.BigEndian.PutUint16(tag[offset:], uint16(len(sps)))
	offset += 2
	copy(tag[offset:], sps)
	offset += len(sps)

	// PPS array
	tag[offset] = 34
	offset++
	binary.BigEndian.PutUint16(tag[offset:], 1)
	offset += 2
	binary.BigEndian.PutUint16(tag[offset:], uint16(len(pps)))
	offset += 2
	copy(tag[offset:], pps)
	offset += len(pps)

	// PreviousTagSize
	prevSize := uint32(dataSize + 11)
	binary.BigEndian.PutUint32(tag[offset:], prevSize)
	return tag
}

// FlushTag finalises and returns the accumulated FLV tag (preceded by
// its PreviousTagSize), resetting the internal buffer.
func (m *FLVMuxer) FlushTag() []byte {
	if len(m.frameBuf) == 0 {
		return nil
	}
	tag := m.frameBuf
	m.frameBuf = nil
	return tag
}

// Append appends raw bytes to the current tag body being built.
func (m *FLVMuxer) Append(p []byte) {
	m.frameBuf = append(m.frameBuf, p...)
}

// SetLastTimestamp records the FLV timestamp of the most recently written tag.
func (m *FLVMuxer) SetLastTimestamp(ts uint32) {
	m.lastTimestamp = ts
}

// LastTimestamp returns the FLV timestamp (ms) of the previous tag.
func (m *FLVMuxer) LastTimestamp() uint32 {
	return m.lastTimestamp
}

// Timestamp returns the current accumulated timestamp (or zero if not set).
func (m *FLVMuxer) Timestamp() uint32 { return m.lastTimestamp }

// Close closes the muxer and returns any finalisation data (e.g., an empty
// FLV tag to signal end of stream). Currently returns nil.
func (m *FLVMuxer) Close() error {
	m.sentHeader = false
	m.sentSeqHeader = false
	m.lastTimestamp = 0
	m.frameBuf = nil
	return nil
}

// String implements fmt.Stringer for logging.
func (m *FLVMuxer) String() string {
	return m.channelKey
}

// PTSFromTimestamp converts an FLV timestamp (ms) to a PTS (90 kHz units).
func PTSFromTimestamp(tsMS uint32) uint64 {
	return uint64(tsMS) * 90000 / 1000
}

// TimestampFromPTS converts a PTS (90 kHz units) to FLV timestamp (ms).
func TimestampFromPTS(pts uint64) uint32 {
	return uint32(pts * 1000 / 90000)
}

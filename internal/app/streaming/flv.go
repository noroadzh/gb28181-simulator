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
	flvFrameKey     = 1 // keyframe (IDR)
	flvFrameInter   = 2 // inter frame
	flvFrameDispo   = 3 // disposable inter (deprecated)
	flvFrameGenKey  = 4 // generated keyframe
	flvFrameVideoInfo = 5
)

// FLV AVC packet types.
const (
	flvAVCSequenceHeader = 0 // AVC sequence header (SPS/PPS)
	flvAVCNALU           = 1 // AVC NALU
	flvAVCEOS            = 2 // AVC end of sequence
)

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
	// FLV header
	0x46, 0x4C, 0x56, // "FLV"
	0x01,             // version 1
	0x01,             // flags: 0x01 = video present (no audio in basic streaming)
	0x00, 0x00, 0x00, // header size (big-endian, usually 9)
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

	// Find PES header start: scan for 00 00 01 E0 after the pack header.
	// Pack header is: 00 00 01 BA + 12 bytes. PES follows at offset 16.
	// Safer: scan for 00 00 01 Ex pattern.
	pi := 0
	for pi < len(psPayload)-4 {
		if psPayload[pi] == 0x00 && psPayload[pi+1] == 0x00 &&
			psPayload[pi+2] == 0x01 && (psPayload[pi+3]&0xF0) == 0xE0 {
			pi += 4
			break
		}
		pi++
	}
	if pi >= len(psPayload) {
		return nil, 0, false
	}

	// PES header: skip PES_packet_length (2 bytes), then parse PES header structure.
	if pi+3 > len(psPayload) {
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
			nal := make([]byte, 4+nextStart-naluStart)
			// FLV AVC format: 4-byte NALU length (big-endian) + NALU data (no start code)
			binary.BigEndian.PutUint32(nal, uint32(nextStart-naluStart))
			copy(nal[4:], psPayload[naluStart:nextStart])

			// Only keep video NALUs (5-23 are H.264 types; 7=SPS, 8=PPS, 5=IDR, 1=non-IDR).
			if naluType >= 1 && naluType <= 23 {
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

	recordLen := 5 + 2 + len(sps) + 2 + len(pps)
	tag := make([]byte, 11+recordLen) // 11 bytes tag header + body

	// Tag header (11 bytes):
	tag[0] = flvTagTypeVideo
	// DataSize (3 bytes, big-endian) = 1 (frame type/codec) + 1 (AVC packet type) + 6 (conf) + sps + pps
	dataSize := 1 + 1 + recordLen
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
	tag[offset] = sps[1] // AVCProfileIndication
	offset++
	tag[offset] = sps[2] // profileCompatibility
	offset++
	tag[offset] = sps[3] // AVCLevelIndication
	offset++
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

	// PreviousTagSize (4 bytes, big-endian)
	prevSize := uint32(dataSize + 11)
	binary.BigEndian.PutUint32(tag[offset:], prevSize)

	return tag
}

// BuildVideoTag builds one FLV video tag from an H.264 NALU.
// nalu must be in length-prefixed format (4-byte length + data).
// isKey should be true for IDR frames.
// timestamp is in milliseconds.
func BuildVideoTag(nalu []byte, isKey bool, timestampMS uint32) []byte {
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
	// Frame type + codec: keyframe(1)/inter(2) + AVC(7) => 0x17 or 0x27
	if isKey {
		tag[offset] = 0x17
	} else {
		tag[offset] = 0x27
	}
	offset++
	tag[offset] = flvAVCNALU // AVC NALU
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

// ExtractSPSPPS extracts SPS and PPS NALUs from a slice of NALUs.
// SPS = NALU type 7, PPS = NALU type 8.
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

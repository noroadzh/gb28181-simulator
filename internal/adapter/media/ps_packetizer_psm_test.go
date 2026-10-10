package media

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// findPsmStartCode returns the byte offset of `00 00 01 BC` in b, or -1.
func findPsmStartCode(b []byte) int {
	return bytes.Index(b, []byte{0x00, 0x00, 0x01, 0xBC})
}

// parsePSM decodes a raw PSM unit (starting at the 00 00 01 BC start code)
// per ISO/IEC 13818-1 §2.5.4, returning the elementary_stream_map entries and
// the embedded CRC_32.
func parsePSM(t *testing.T, psm []byte) (entries [][2]byte, crcGot uint32) {
	t.Helper()
	// [0:4] start code | [4:6] program_stream_map_length | body | CRC(4)
	if len(psm) < 4+2+12 {
		t.Fatalf("PSM too short: %d bytes", len(psm))
	}
	psmLen := int(binary.BigEndian.Uint16(psm[4:6]))
	crcStart := 6 + psmLen - 4 // CRC is included in psmLen
	if crcStart+4 > len(psm) {
		t.Fatalf("PSM length %d exceeds buffer %d", psmLen, len(psm))
	}
	// Body (after program_stream_map_length), per §2.5.4:
	//   [0]  current_next_indicator | reserved | program_stream_map_version
	//   [1]  reserved | marker_bit
	//   [2:4] program_stream_info_length
	//   [4:6] elementary_stream_map_length
	//   [6:6+esMapLen] N × (stream_type, elementary_stream_id)
	infoLen := int(binary.BigEndian.Uint16(psm[8:10]))
	esMapLen := int(binary.BigEndian.Uint16(psm[10:12]))
	if infoLen != 0 {
		t.Fatalf("expected no descriptors, program_stream_info_length = %d", infoLen)
	}
	for i := 0; i+1 < esMapLen; i += 2 {
		entries = append(entries, [2]byte{psm[12+i], psm[12+i+1]})
	}
	crcGot = binary.BigEndian.Uint32(psm[crcStart : crcStart+4])
	return entries, crcGot
}

// TestCRC32MPEG2KnownVector verifies CRC-32/MPEG-2 (ISO/IEC 13818-1 Annex A,
// poly 0x04C11DB7, init 0xFFFFFFFF) against the standard known vector.
func TestCRC32MPEG2KnownVector(t *testing.T) {
	if got := crc32MPEG2([]byte("123456789")); got != 0x0376E6E7 {
		t.Errorf("CRC-32/MPEG-2 for \"123456789\" = %08X, want 0376E6E7", got)
	}
}

// TestPSMMPEG4FirstFrameOnly asserts that a MPEG-4 Part 2 stream gets a PSM
// with stream_type=0x10 on the first video frame only, and that the PSM CRC
// self-verifies.
func TestPSMMPEG4FirstFrameOnly(t *testing.T) {
	p := NewPSPacketizer(nil)
	es1 := []byte{0x00, 0x00, 0x01, 0xB6, 0x12, 0x34, 0x56} // 3-byte start code + VOP
	es2 := []byte{0x00, 0x00, 0x01, 0xB6, 0xAB, 0xCD, 0xEF}

	ps1, err := p.Packetize(model.NewESFrame(es1, 90000))
	if err != nil {
		t.Fatalf("packetize frame 1: %v", err)
	}
	ps2, err := p.Packetize(model.NewESFrame(es2, 180000))
	if err != nil {
		t.Fatalf("packetize frame 2: %v", err)
	}

	// Frame 1 must contain a PSM unit with (0x10, 0xE0).
	idx := findPsmStartCode(ps1.Payload)
	if idx < 0 {
		t.Fatal("first video frame should contain a 00 00 01 BC PSM unit")
	}
	entries, crcGot := parsePSM(t, ps1.Payload[idx:])
	if len(entries) != 1 {
		t.Fatalf("PSM elementary_stream_map should have 1 entry, got %d", len(entries))
	}
	if entries[0][0] != 0x10 || entries[0][1] != 0xE0 {
		t.Errorf("PSM entry = (0x%02X, 0x%02X), want (0x10, 0xE0)", entries[0][0], entries[0][1])
	}
	// Self-verify CRC over body bytes from the first byte after
	// program_stream_map_length through the last map entry byte
	// (ISO/IEC 13818-1 §2.5.4: CRC begins with the first byte AFTER the
	// program_stream_map_length field — 与 FFmpeg mpeg.c 等主流实现一致).
	// Layout: [0:4] start code | [4:6] psm_length | body | [len-4:] CRC.
	psmLen := int(binary.BigEndian.Uint16(ps1.Payload[idx+4 : idx+6]))
	crcWant := crc32MPEG2(ps1.Payload[idx+6 : idx+6+psmLen-4])
	if crcWant != crcGot {
		t.Errorf("PSM CRC mismatch: embedded %08X, computed %08X", crcGot, crcWant)
	}

	// Frame 2 must NOT contain a PSM.
	if findPsmStartCode(ps2.Payload) >= 0 {
		t.Error("second video frame should not contain a PSM unit")
	}
}

// TestPSMH264FirstFrame asserts that a H.264 stream (SPS-led) gets a PSM with
// stream_type = 0x1B on the first frame only.
func TestPSMH264FirstFrame(t *testing.T) {
	p := NewPSPacketizer(nil)
	es := append([]byte{}, sampleES...) // SPS(0x67)+PPS(0x68)+IDR(0x65), 4-byte start codes
	ps1, err := p.Packetize(model.NewESFrame(es, 45000))
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	idx := findPsmStartCode(ps1.Payload)
	if idx < 0 {
		t.Fatal("H.264 first video frame should contain a PSM unit")
	}
	entries, _ := parsePSM(t, ps1.Payload[idx:])
	if len(entries) != 1 || entries[0][0] != 0x1B || entries[0][1] != 0xE0 {
		t.Errorf("PSM entry = %v, want [(0x1B, 0xE0)]", entries)
	}
}

// TestPSMHEVCFirstFrame asserts that a HEVC stream (VPS-led with byte1
// carrying nuh_temporal_id_plus1=1) gets a PSM with stream_type = 0x24.
func TestPSMHEVCFirstFrame(t *testing.T) {
	p := NewPSPacketizer(nil)
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x40, 0x01, 0xC0, 0x00} // VPS NAL unit
	ps, err := p.Packetize(model.NewESFrame(es, 9000))
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	idx := findPsmStartCode(ps.Payload)
	if idx < 0 {
		t.Fatal("HEVC first video frame should contain a PSM unit")
	}
	entries, _ := parsePSM(t, ps.Payload[idx:])
	if len(entries) != 1 || entries[0][0] != 0x24 || entries[0][1] != 0xE0 {
		t.Errorf("PSM entry = %v, want [(0x24, 0xE0)]", entries)
	}
}

// TestPSMUnknownCodecSkipped asserts the "宁缺毋错" fallback: an unrecognizable
// first video frame must not emit a PSM, and must stay PSM-less for the rest
// of the stream (psmUnsupported is sticky).
func TestPSMUnknownCodecSkipped(t *testing.T) {
	p := NewPSPacketizer(nil)
	es := []byte{0x00, 0x00, 0x01, 0x99, 0xAA} // no valid codec signature
	ps1, err := p.Packetize(model.NewESFrame(es, 90000))
	if err != nil {
		t.Fatalf("packetize 1: %v", err)
	}
	if findPsmStartCode(ps1.Payload) >= 0 {
		t.Error("unknown codec: first frame should not contain a PSM unit")
	}
	// Even a subsequent H.264 frame should not trigger a PSM (sticky unsupported).
	ps2, err := p.Packetize(model.NewESFrame(sampleES, 180000))
	if err != nil {
		t.Fatalf("packetize 2: %v", err)
	}
	if findPsmStartCode(ps2.Payload) >= 0 {
		t.Error("unknown codec is sticky: later H.264 frame should not gain a PSM")
	}
}

// TestPSMAudioFirstDoesNotTriggerPSM: only the first video frame triggers PSM.
func TestPSMAudioFirstDoesNotTriggerPSM(t *testing.T) {
	p := NewPSPacketizer(nil)
	audio := model.NewESFrame([]byte{0x01, 0x02, 0x03}, 90000)
	audio.Kind = model.ESFrameAudio
	psA, err := p.Packetize(audio)
	if err != nil {
		t.Fatalf("packetize audio: %v", err)
	}
	if findPsmStartCode(psA.Payload) >= 0 {
		t.Error("audio frame should not contain a PSM unit")
	}
	// The subsequent video frame is the first video frame → PSM emitted there.
	psV, err := p.Packetize(model.NewESFrame(sampleES, 180000))
	if err != nil {
		t.Fatalf("packetize video: %v", err)
	}
	if findPsmStartCode(psV.Payload) < 0 {
		t.Error("first video frame should contain a PSM unit even after an audio frame")
	}
}

// TestPSMTransparentToDepacketizer feeds the PSM-bearing PS output back into
// PSDepacketizer and asserts the ES frame sequence is byte-identical (frames,
// payloads, PTS) to the no-PSM baseline.
func TestPSMTransparentToDepacketizer(t *testing.T) {
	p := NewPSPacketizer(nil)
	es1 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x80, 0x0A, 0xAA, 0xBB} // H.264 SPS
	es2 := []byte{0x00, 0x00, 0x01, 0xB6, 0x11, 0x22, 0x33}                   // MPEG-4 VOP
	f1 := model.NewESFrame(es1, 90000)
	f2 := model.NewESFrame(es2, 180000)
	ps1, err := p.Packetize(f1)
	if err != nil {
		t.Fatalf("packetize f1: %v", err)
	}
	ps2, err := p.Packetize(f2)
	if err != nil {
		t.Fatalf("packetize f2: %v", err)
	}
	withPSM := append(append([]byte{}, ps1.Payload...), ps2.Payload...)

	// Strip the leading PSM unit to get the baseline "no-PSM" stream.
	// PSM total size = 4 (start code) + 2 (length) + psmLen (body + CRC).
	idx := findPsmStartCode(withPSM)
	if idx < 0 {
		t.Fatal("expected PSM in first frame")
	}
	psmLen := int(binary.BigEndian.Uint16(withPSM[idx+4 : idx+6]))
	psmTotal := 4 + 2 + psmLen
	noPSM := append([]byte{}, withPSM[:idx]...)
	noPSM = append(noPSM, withPSM[idx+psmTotal:]...)

	depWith := NewPSDepacketizer()
	framesWith, err := depWith.Write(withPSM)
	if err != nil {
		t.Fatalf("depacketize with PSM: %v", err)
	}
	depWithout := NewPSDepacketizer()
	framesWithout, err := depWithout.Write(noPSM)
	if err != nil {
		t.Fatalf("depacketize without PSM: %v", err)
	}

	if len(framesWith) != len(framesWithout) {
		t.Fatalf("frame count mismatch: with PSM = %d, without PSM = %d",
			len(framesWith), len(framesWithout))
	}
	for i := range framesWith {
		if framesWith[i].PTS != framesWithout[i].PTS {
			t.Errorf("frame %d PTS: with PSM = %d, without PSM = %d",
				i, framesWith[i].PTS, framesWithout[i].PTS)
		}
		if !bytes.Equal(framesWith[i].Payload, framesWithout[i].Payload) {
			t.Errorf("frame %d payload: with PSM = % x, without PSM = % x",
				i, framesWith[i].Payload, framesWithout[i].Payload)
		}
	}
	// Sanity: ES output must equal the source ES frames (round trip intact).
	if len(framesWith) != 2 || !bytes.Equal(framesWith[0].Payload, es1) || !bytes.Equal(framesWith[1].Payload, es2) {
		t.Errorf("round-tripped ES frames do not match source: %d frames", len(framesWith))
	}
}

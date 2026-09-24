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
	"bytes"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// A minimal H.264-style ES payload: a few NALUs with Annex B start codes.
var sampleES = []byte{
	0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x80, 0x0A, // SPS
	0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
	0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x80, 0x12, // IDR frame
	0x01, 0x02, 0x03, 0x04, 0x05,
}

func TestPSPacketizeRoundTrip(t *testing.T) {
	pktz := NewPSPacketizer()
	depktz := NewPSDepacketizer()

	frame := model.NewESFrame(sampleES, 270000)
	ps, err := pktz.Packetize(frame)
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	if err := ps.Validate(); err != nil {
		t.Fatalf("invalid PS frame: %v", err)
	}

	// Depacketize the PS bytes.
	frames, err := depktz.Write(ps.Payload)
	if err != nil {
		t.Fatalf("depacketize write: %v", err)
	}
	if err := depktz.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}

	got := frames[0]
	if !bytes.Equal(got.Payload, sampleES) {
		t.Fatalf("ES payload mismatch: got %x, want %x", got.Payload, sampleES)
	}
	if got.PTS != frame.PTS {
		t.Errorf("PTS = %d, want %d", got.PTS, frame.PTS)
	}
	if !got.HasPTS() {
		t.Error("ES frame should carry the source PTS")
	}
}

func TestPSDepacketizerMultipleFrames(t *testing.T) {
	pktz := NewPSPacketizer()
	depktz := NewPSDepacketizer()

	frame1 := model.NewESFrame(sampleES, 90000)
	frame2 := model.NewESFrame(append([]byte{0x01, 0x02}, sampleES...), 180000)

	ps1, err := pktz.Packetize(frame1)
	if err != nil {
		t.Fatalf("packetize 1: %v", err)
	}
	ps2, err := pktz.Packetize(frame2)
	if err != nil {
		t.Fatalf("packetize 2: %v", err)
	}

	// Feed both frames in one Write call (simulating a stream).
	frames, err := depktz.Write(append(ps1.Payload, ps2.Payload...))
	if err != nil {
		t.Fatalf("depacketize: %v", err)
	}
	if err := depktz.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}

	if !bytes.Equal(frames[0].Payload, sampleES) {
		t.Errorf("frame 0 payload mismatch")
	}
	if !bytes.Equal(frames[1].Payload, append([]byte{0x01, 0x02}, sampleES...)) {
		t.Errorf("frame 1 payload mismatch")
	}
	if frames[1].PTS != 180000 {
		t.Errorf("frame 1 PTS = %d, want 180000", frames[1].PTS)
	}
}

func TestRTPizerSinglePacket(t *testing.T) {
	rtp := NewRTPizer(0xABCDEF01, 1400)

	ps := model.PSFrame{Payload: []byte{0x01, 0x02, 0x03, 0x04, 0x05}, PTS: 123456}
	pkts, err := rtp.Packetize(ps)
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	if len(pkts) != 1 {
		t.Fatalf("got %d RTP packets, want 1", len(pkts))
	}

	p := pkts[0]
	if p.SSRC != 0xABCDEF01 {
		t.Errorf("SSRC = 0x%X, want 0xABCDEF01", p.SSRC)
	}
	if p.Sequence != 1 {
		t.Errorf("Sequence = %d, want 1", p.Sequence)
	}
	if !p.Marker {
		t.Error("marker bit should be set for single-packet frame")
	}
	if p.Timestamp != 123456 {
		t.Errorf("Timestamp = %d, want 123456", p.Timestamp)
	}
	if !bytes.Equal(p.Payload, ps.Payload) {
		t.Errorf("payload mismatch")
	}
}

func TestRTPizerFragmentsLargeFrame(t *testing.T) {
	rtp := NewRTPizer(0x12345678, 50)

	// A 200-byte PS frame with MTU 50 should produce multiple RTP packets.
	ps := model.PSFrame{Payload: bytes.Repeat([]byte{0xAB}, 200), PTS: 45000}
	pkts, err := rtp.Packetize(ps)
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	if len(pkts) < 2 {
		t.Fatalf("got %d packets, want >= 2 for fragmented frame", len(pkts))
	}

	// All but the last packet should have marker=false.
	for i := 0; i < len(pkts)-1; i++ {
		if pkts[i].Marker {
			t.Errorf("packet %d marker should be false", i)
		}
	}
	if !pkts[len(pkts)-1].Marker {
		t.Error("last packet marker should be true")
	}

	// Reassembled payload should equal the original.
	var reassembled []byte
	for _, p := range pkts {
		reassembled = append(reassembled, p.Payload...)
	}
	if !bytes.Equal(reassembled, ps.Payload) {
		t.Error("reassembled payload does not match original")
	}

	// Sequence numbers should be strictly increasing.
	seq := pkts[0].Sequence
	for i := 1; i < len(pkts); i++ {
		if pkts[i].Sequence != seq+uint16(i) {
			t.Errorf("sequence discontinuity at packet %d: %d -> %d", i, pkts[i-1].Sequence, pkts[i].Sequence)
		}
	}
}

func TestRTPizerSequenceMonotonic(t *testing.T) {
	rtp := NewRTPizer(1, 1400)
	for i := 0; i < 100; i++ {
		ps := model.PSFrame{Payload: []byte{byte(i)}, PTS: uint64(i)}
		pkts, err := rtp.Packetize(ps)
		if err != nil {
			t.Fatalf("packetize %d: %v", i, err)
		}
		if len(pkts) != 1 {
			t.Fatalf("iteration %d: expected 1 packet, got %d", i, len(pkts))
		}
		if pkts[0].Sequence != uint16(i+1) {
			t.Errorf("iteration %d: seq=%d, want %d", i, pkts[0].Sequence, i+1)
		}
	}
}

func TestRTPDeizerReassemblesFrame(t *testing.T) {
	deizer := NewRTPDeizer(0xABCDEF01)

	// Build a 200-byte PS payload and fragment it with RTPizer.
	rtp := NewRTPizer(0xABCDEF01, 50)
	ps := model.PSFrame{Payload: bytes.Repeat([]byte{0xCD}, 200), PTS: 78900}
	pkts, err := rtp.Packetize(ps)
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}

	// Feed packets out of order + duplicate to test reordering and dedup.
	pkts[0], pkts[1] = pkts[1], pkts[0] // swap first two
	pkts = append(pkts, pkts[0])        // duplicate the first one

	var gotPS model.PSFrame
	for _, p := range pkts {
		out, err := deizer.Write(p)
		if err != nil {
			t.Fatalf("deizer write: %v", err)
		}
		if out.Payload != nil {
			gotPS = out
		}
	}
	if err := deizer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if !bytes.Equal(gotPS.Payload, ps.Payload) {
		t.Error("reassembled payload does not match original")
	}
	if gotPS.PTS != ps.PTS {
		t.Errorf("PTS = %d, want %d", gotPS.PTS, ps.PTS)
	}
}

func TestRTPDeizerMarkerEndsFrame(t *testing.T) {
	deizer := NewRTPDeizer(0xDEADBEEF)

	rtp := NewRTPizer(0xDEADBEEF, 30)
	// 36 bytes > MTU(30)-12 = 18, so RTPizer produces exactly 2 packets.
	ps := model.PSFrame{Payload: bytes.Repeat([]byte{0x01, 0x02, 0x03}, 12), PTS: 5000}
	pkts, err := rtp.Packetize(ps)
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}

	// Feed first packet (no marker): should buffer.
	out, err := deizer.Write(pkts[0])
	if err != nil {
		t.Fatalf("write first: %v", err)
	}
	if out.Payload != nil {
		t.Error("first packet without marker should not produce a frame")
	}

	// Feed last packet (marker): should produce the frame.
	out, err = deizer.Write(pkts[len(pkts)-1])
	if err != nil {
		t.Fatalf("write last: %v", err)
	}
	if out.Payload == nil {
		t.Fatal("last packet with marker should produce a frame")
	}
	if !bytes.Equal(out.Payload, ps.Payload) {
		t.Error("payload mismatch after marker")
	}
}

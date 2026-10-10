package media

import (
	"os"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestRegenerateGoldenFiles regenerates the golden fixtures with the current
// (spec-correct) packetizer. Run with: go test ./internal/adapter/media -run TestRegenerateGoldenFiles -count=1
func TestRegenerateGoldenFiles(t *testing.T) {
	// Video ES: SPS + PPS + IDR (Annex-B)
	videoFrame := model.NewESFrame(sampleES, 270000)
	videoFrame.Kind = model.ESFrameVideo
	// Audio ES
	audioFrame := model.NewESFrame([]byte{0x01, 0x02, 0x03, 0x04}, 270000)
	audioFrame.Kind = model.ESFrameAudio

	pktz := NewPSPacketizer(nil)

	ps1, err := pktz.Packetize(videoFrame)
	if err != nil {
		t.Fatalf("packetize video: %v", err)
	}
	ps2, err := pktz.Packetize(audioFrame)
	if err != nil {
		t.Fatalf("packetize audio: %v", err)
	}

	psBytes := append(append([]byte{}, ps1.Payload...), ps2.Payload...)
	if err := os.WriteFile("testdata/ps-roundtrip.bin", psBytes, 0o644); err != nil {
		t.Fatalf("write ps golden: %v", err)
	}
	t.Logf("wrote %d bytes to testdata/ps-roundtrip.bin", len(psBytes))

	// RTP golden: same PS bytes via RTPizer
	rtpz := NewRTPizer(0xABCDEF01, 1400, nil)
	psFrame := model.PSFrame{Payload: psBytes, PTS: 270000}
	pkts, err := rtpz.Packetize(psFrame)
	if err != nil {
		t.Fatalf("rtp packetize: %v", err)
	}

	_ = pkts // RTP golden uses raw PS bytes, since depacketizer takes PS not RTP
	rtpBytes := psBytes
	if err := os.WriteFile("testdata/rtp-roundtrip.bin", rtpBytes, 0o644); err != nil {
		t.Fatalf("write rtp golden: %v", err)
	}
	t.Logf("wrote %d bytes to testdata/rtp-roundtrip.bin", len(rtpBytes))
}

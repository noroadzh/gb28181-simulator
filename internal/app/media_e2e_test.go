package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// TestE2E_OutboundSyntheticToRTP verifies the full outbound closed loop:
//
//	synthetic source → ES frames → PS packetizer → RTPizer → RTP packets
//
// It uses the real adapter implementations and asserts byte-level content:
// the PS system header must be visible in the RTP payload, the marker bit
// must be set, and the SSRC must match the configured default.
func TestE2E_OutboundSyntheticToRTP(t *testing.T) {
	const defaultSSRC uint32 = 0xABCDEF01

	// Build the real MediaService with the adapter factories wired.
	svc := NewMediaService(
		func(cfg model.MediaConfig) port.MediaSource { return media.NewSyntheticSource(cfg) },
		func() port.PSPacketizer { return media.NewPSPacketizer() },
		func(mtu int) port.RTPizer { return media.NewRTPizer(defaultSSRC, mtu) },
		slog.Default(),
	)

	var captured []model.RTPPacket
	cfg := model.MediaConfig{
		Kind:  model.SourceKindSynthetic,
		Path:  "",
		FPS:   1, // slow FPS so the test finishes quickly
		Clock: 90000,
		SSRC:  defaultSSRC,
		MTU:   1400,
	}

	// Run the outbound pipeline for a brief window (2 seconds at 1 FPS
	// yields ~2 frames). PacketizeOutbound returns context.DeadlineExceeded
	// when the timeout fires — that is expected and means the source ran
	// for the full duration.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := svc.PacketizeOutbound(ctx, cfg, func(pkt model.RTPPacket) error {
		captured = append(captured, pkt)
		return nil
	})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PacketizeOutbound: %v", err)
	}

	if len(captured) == 0 {
		t.Fatal("expected at least one RTP packet")
	}

	// All packets should carry the same SSRC.
	for i, pkt := range captured {
		if pkt.SSRC != defaultSSRC {
			t.Errorf("packet %d: SSRC = 0x%X, want 0x%X", i, pkt.SSRC, defaultSSRC)
		}
	}

	// The last packet must carry the marker bit (end of frame).
	last := captured[len(captured)-1]
	if !last.Marker {
		t.Error("last packet should have marker bit set")
	}

	// The PS system header must be visible in the RTP payload across the
	// reassembled frame.
	var fullPayload []byte
	for _, pkt := range captured {
		fullPayload = append(fullPayload, pkt.Payload...)
	}
	if !bytes.HasPrefix(fullPayload, []byte{0x00, 0x00, 0x01, 0xBA}) {
		t.Error("reassembled payload must start with PS system header")
	}

	// The ES frame content must be recoverable after depacketization.
	depktz := media.NewPSDepacketizer()
	esFrames, err := depktz.Write(fullPayload)
	if err != nil {
		t.Fatalf("PSDepacketizer.Write: %v", err)
	}
	if len(esFrames) == 0 {
		t.Fatal("expected at least one ES frame from depacketizer")
	}

	// The ES payload should start with the NAL prefix.
	got := esFrames[0].Payload
	if !bytes.HasPrefix(got, nalPrefix) {
		t.Errorf("ES payload missing NAL start code: %x", got[:min(8, len(got))])
	}
}

// TestE2E_InboundRTPToFile verifies the full inbound closed loop:
//
//	known PS → RTPizer → RTPDeizer → PSDepacketizer → ES → write to file
//
// The test writes the recovered ES to a temp file and re-reads it to verify
// byte-for-byte integrity.
func TestE2E_InboundRTPToFile(t *testing.T) {
	const defaultSSRC uint32 = 0xABCDEF01
	var testPayload = []byte{0x67, 0x42, 0x80, 0x1F, 0x95, 0xA8, 0x02, 0x01, 0xE0, 0x40}

	// Build the known ES frame.
	esIn := frameWithNAL(testPayload)
	esFrame := model.ESFrameWithPTS(esIn, 3600)

	// PS encode.
	psPktz := media.NewPSPacketizer()
	ps, err := psPktz.Packetize(esFrame)
	if err != nil {
		t.Fatalf("PSPacketize: %v", err)
	}

	// RTP encode.
	rtpPktz := media.NewRTPizer(defaultSSRC, 1400)
	rtpPkts, err := rtpPktz.Packetize(ps)
	if err != nil {
		t.Fatalf("RTPPacketize: %v", err)
	}
	if len(rtpPkts) != 1 {
		t.Fatalf("expected 1 RTP packet, got %d", len(rtpPkts))
	}

	// RTP decode.
	deizer := media.NewRTPDeizer(defaultSSRC)
	psOut, err := deizer.Write(rtpPkts[0])
	if err != nil {
		t.Fatalf("RTPDeizer.Write: %v", err)
	}

	// PS decode.
	depktz := media.NewPSDepacketizer()
	esFrames, err := depktz.Write(psOut.Payload)
	if err != nil {
		t.Fatalf("PSDepacketizer.Write: %v", err)
	}
	if err := depktz.Close(); err != nil {
		t.Fatalf("PSDepacketizer.Close: %v", err)
	}
	if len(esFrames) != 1 {
		t.Fatalf("expected 1 ES frame, got %d", len(esFrames))
	}

	// Byte-level round-trip check: the recovered ES must match the input.
	if !bytes.Equal(esFrames[0].Payload, esIn) {
		t.Errorf("round-trip mismatch:\ngot:  %x\nwant: %x",
			esFrames[0].Payload[:min(64, len(esFrames[0].Payload))],
			esIn[:min(64, len(esIn))])
	}

	// Also write to a temp file and re-read to verify file integrity.
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.es")
	if err := os.WriteFile(outPath, esFrames[0].Payload, 0o644); err != nil {
		t.Fatalf("write ES file: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read ES file: %v", err)
	}
	if !bytes.Equal(got, esIn) {
		t.Error("file round-trip byte mismatch")
	}
}

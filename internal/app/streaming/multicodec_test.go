package streaming

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// 内置小体积测试素材（go test 工作目录为包目录，testdata 在 adapter/media 下）。
const (
	mpeg4Asset = "../../adapter/media/testdata/mpeg4-part2.mp4"
	hevcAsset  = "../../adapter/media/testdata/hevc-sample.mp4"
)

// PacketizeES wraps an ES frame into a PS frame via the media adapter's
// packetizer, mimicking the production pipeline
// (MP4Demuxer → ESFrameReader → PSPacketizer → gateway ParsePS).
func PacketizeES(frame model.ESFrame) (model.PSFrame, error) {
	pktz := media.NewPSPacketizer(nil)
	return pktz.Packetize(frame)
}

// TestMulticodecPSRoundTrip verifies the full ES→PS pipeline for HEVC and
// MPEG-4 Part 2 video sources from built-in MP4 test assets (offline, no /tmp).
func TestMulticodecPSRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"HEVC", hevcAsset},
		{"MPEG4", mpeg4Asset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := os.Stat(tc.path); err != nil {
				t.Fatalf("built-in asset missing: %s: %v", tc.path, err)
			}

			f, err := os.Open(tc.path)
			if err != nil {
				t.Fatalf("Open(%s): %v", tc.path, err)
			}
			defer f.Close()

			demuxer, err := media.NewMP4Demuxer(f)
			if err != nil {
				t.Fatalf("NewMP4Demuxer rejected %s: %v", tc.name, err)
			}
			defer demuxer.Close()

			var framer port.ESFrameReader = demuxer

			// Read one ES frame and PS-packetize it
			frame, err := framer.ReadFrame(context.Background())
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if len(frame.Payload) == 0 {
				t.Fatal("empty ES frame")
			}

			ps, err := PacketizeES(frame)
			if err != nil {
				t.Fatalf("PacketizeES: %v", err)
			}
			if len(ps.Payload) == 0 {
				t.Fatal("empty PS frame")
			}

			t.Logf("%s: ES=%d bytes → PS=%d bytes", tc.name, len(frame.Payload), len(ps.Payload))
		})
	}
}

// TestParsePSHEVCSource verifies a real HEVC MP4 asset round-trips through
// ES→PS→ParsePS→DetectCodec and is identified as HEVC.
func TestParsePSHEVCSource(t *testing.T) {
	f, err := os.Open(hevcAsset)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	demuxer, err := media.NewMP4Demuxer(f)
	if err != nil {
		t.Fatalf("NewMP4Demuxer: %v", err)
	}
	defer demuxer.Close()

	var framer port.ESFrameReader = demuxer

	frame, err := framer.ReadFrame(context.Background())
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	ps, err := PacketizeES(frame)
	if err != nil {
		t.Fatalf("PacketizeES: %v", err)
	}

	nalus, _, ok := ParsePS(ps.Payload)
	if !ok {
		t.Fatalf("ParsePS rejected PS payload (len=%d)", len(ps.Payload))
	}
	if len(nalus) == 0 {
		t.Fatalf("ParsePS returned 0 NALUs from HEVC ES frame")
	}

	codec := DetectCodec(nalus)
	t.Logf("HEVC: %d NALUs, codec=%s", len(nalus), codec)
	if codec != CodecHEVC {
		t.Fatalf("DetectCodec returned %s, want CodecHEVC", codec)
	}
}

// TestParsePSMPEG4Source verifies a real MPEG-4 Part 2 MP4 asset round-trips
// through ES→PS→ParsePS→DetectCodec and is identified as MPEG-4 (no longer
// misdetected as H.264, no longer rejected by the demuxer).
func TestParsePSMPEG4Source(t *testing.T) {
	f, err := os.Open(mpeg4Asset)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	demuxer, err := media.NewMP4Demuxer(f)
	if err != nil {
		t.Fatalf("NewMP4Demuxer must accept mp4v, got: %v", err)
	}
	defer demuxer.Close()

	var framer port.ESFrameReader = demuxer

	// First video frame carries the VOS/VO/VOL config prefix.
	frame, err := framer.ReadFrame(context.Background())
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if len(frame.Payload) < 4 || frame.Payload[0] != 0x00 || frame.Payload[1] != 0x00 ||
		(frame.Payload[2] != 0x01 && !(frame.Payload[2] == 0x00 && frame.Payload[3] == 0x01)) {
		t.Fatalf("first frame does not start with an Annex-B start code: % x", frame.Payload[:min(8, len(frame.Payload))])
	}

	ps, err := PacketizeES(frame)
	if err != nil {
		t.Fatalf("PacketizeES must succeed for mp4v, got: %v", err)
	}

	nalus, _, ok := ParsePS(ps.Payload)
	if !ok {
		t.Fatalf("ParsePS rejected PS payload (len=%d)", len(ps.Payload))
	}
	if len(nalus) == 0 {
		t.Fatalf("ParsePS returned 0 NALUs from MPEG-4 ES frame")
	}

	codec := DetectCodec(nalus)
	t.Logf("MPEG-4: %d NALUs, codec=%s", len(nalus), codec)
	if codec != CodecMPEG4 {
		t.Fatalf("DetectCodec returned %s, want CodecMPEG4 (was misdetected as AVC before this change)", codec)
	}
}

// TestFLVHeaderCorrect verifies the FLVHeader constant has exactly 13 bytes
// (9-byte FLV header + 4-byte PreviousTagSize0) and DataOffset == 9.
func TestFLVHeaderCorrect(t *testing.T) {
	if len(FLVHeader) != 13 {
		t.Fatalf("FLVHeader length = %d, want 13", len(FLVHeader))
	}
	if !bytes.Equal(FLVHeader[0:3], []byte("FLV")) {
		t.Fatalf("FLV magic mismatch: % x", FLVHeader[0:3])
	}
	if FLVHeader[3] != 0x01 {
		t.Fatalf("version = %d, want 1", FLVHeader[3])
	}
	offset := uint32(FLVHeader[5])<<24 | uint32(FLVHeader[6])<<16 | uint32(FLVHeader[7])<<8 | uint32(FLVHeader[8])
	if offset != 9 {
		t.Fatalf("DataOffset = %d, want 9", offset)
	}
	prev := uint32(FLVHeader[9])<<24 | uint32(FLVHeader[10])<<16 | uint32(FLVHeader[11])<<8 | uint32(FLVHeader[12])
	if prev != 0 {
		t.Fatalf("PreviousTagSize0 = %d, want 0", prev)
	}
}

// TestHEVCSequenceHeaderNoPanic verifies BuildHEVCSequenceHeader does not
// panic with empty VPS/SPS/PPS inputs.
func TestHEVCSequenceHeaderNoPanic(t *testing.T) {
	tag := BuildHEVCSequenceHeader(nil, nil, nil)
	if tag == nil {
		t.Fatal("BuildHEVCSequenceHeader(nil,nil,nil) returned nil")
	}
	if len(tag) < 15 {
		t.Fatalf("tag too short: %d bytes", len(tag))
	}
	if tag[0] != flvTagTypeVideo {
		t.Fatalf("tag[0] = 0x%02x, want 0x%02x (flvTagTypeVideo)", tag[0], flvTagTypeVideo)
	}
}

// TestBuildAVCSequenceHeaderNoPanic verifies BuildAVCSequenceHeader produces a
// tag with TagType=9 and does not panic with empty SPS/PPS.
func TestBuildAVCSequenceHeaderNoPanic(t *testing.T) {
	tag := BuildAVCSequenceHeader(nil, nil)
	if tag == nil {
		t.Fatal("BuildAVCSequenceHeader(nil,nil) returned nil")
	}
	if tag[0] != flvTagTypeVideo {
		t.Fatalf("tag[0] = 0x%02x, want 0x%02x (flvTagTypeVideo) — AVC sequence header must have TagType=9", tag[0], flvTagTypeVideo)
	}
	// body[1] must be 0 (AVC sequence header)
	if tag[12] != flvAVCSequenceHeader {
		t.Fatalf("AVC packet type = %d, want %d", tag[12], flvAVCSequenceHeader)
	}
}

// TestMPEG4FilesExist is a smoke test that checks the built-in HEVC and
// MPEG-4 test assets are present on disk.
func TestMPEG4FilesExist(t *testing.T) {
	for _, path := range []string{hevcAsset, mpeg4Asset} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("built-in asset %s missing: %v", path, err)
			continue
		}
		t.Logf("asset %s: %d bytes", path, info.Size())
	}
}

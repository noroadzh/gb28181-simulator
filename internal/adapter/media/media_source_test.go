package media

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

func TestFileSourceReadsLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.bin")
	data := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := NewFileSource(model.MediaConfig{Kind: model.SourceKindFile, Path: path})
	rc, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %v, want %v", got, data)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSyntheticSourceProducesNALUnits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	src := NewSyntheticSource(model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25})
	rc, err := src.Open(ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	reader := port.NewStreamESReader(rc, model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25, Clock: 90000})

	frame, err := reader.Read(ctx)
	if err != nil {
		t.Fatalf("Read first frame: %v", err)
	}
	if len(frame.Payload) == 0 {
		t.Fatalf("first frame empty")
	}
	if !bytes.HasPrefix(frame.Payload, []byte{0x00, 0x00, 0x00, 0x01}) {
		t.Fatalf("first frame missing NAL start code: %x", frame.Payload)
	}

	frame, err = reader.Read(ctx)
	if err != nil {
		t.Fatalf("Read second frame: %v", err)
	}
	if len(frame.Payload) == 0 {
		t.Fatalf("second frame empty")
	}
	if !bytes.HasPrefix(frame.Payload, []byte{0x00, 0x00, 0x00, 0x01}) {
		t.Fatalf("second frame missing NAL start code: %x", frame.Payload)
	}

	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRTSPSourceOpenInvalidURL(t *testing.T) {
	src := NewRTSPSource(model.MediaConfig{Kind: model.SourceKindRTSP, Path: "rtsp://127.0.0.1:9/invalid"})
	_, err := src.Open(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable RTSP server")
	}
}

func TestHLSSourceOpenInvalidURL(t *testing.T) {
	src := NewHLSSource(model.MediaConfig{Kind: model.SourceKindHLS, Path: "http://127.0.0.1:9/playlist.m3u8"})
	_, err := src.Open(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable HLS server")
	}
}

func TestSyntheticSourceConfigPassthrough(t *testing.T) {
	cfg := model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 30, SSRC: 0xDEADBEEF}
	src := NewSyntheticSource(cfg)
	got := src.Config()
	if got.FPS != cfg.FPS || got.SSRC != cfg.SSRC || got.Kind != cfg.Kind {
		t.Fatalf("config mismatch: %+v", got)
	}
}

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
	defer func() { _ = rc.Close() }()

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
	defer func() { _ = rc.Close() }()

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

// TestFileSource_NoLoop_StopsAtEOF guards the default behaviour: an absent
// Loop flag reads the file once and returns io.EOF on the second read.
// We use a bounded loop instead of io.ReadAll because ReadAll discards
// the (0, io.EOF) terminal value.
func TestFileSource_NoLoop_StopsAtEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.bin")
	data := []byte{0x01, 0x02, 0x03}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	src := NewFileSource(model.MediaConfig{Kind: model.SourceKindFile, Path: path})
	rc, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	buf := make([]byte, 16)
	// First read: file is larger than buf, returns all data.
	n1, err1 := rc.Read(buf)
	if n1 != len(data) || err1 != nil {
		t.Fatalf("first read n=%d err=%v, want n=%d nil", n1, err1, len(data))
	}
	// Second read: file exhausted, must return (0, io.EOF).
	n2, err2 := rc.Read(buf)
	if n2 != 0 || err2 != io.EOF {
		t.Fatalf("second read n=%d err=%v, want n=0 io.EOF", n2, err2)
	}
}

// TestFileSource_Loop_RewindsOnEOF verifies that Loop=true causes the
// reader to restart from the beginning of the file. We request exactly
// 2× the file length using a buffer smaller than the file, so the first
// read fills the buffer and the second read must start a new pass from
// offset 0 — confirming the rewind. A third read must NOT return (0,EOF)
// immediately (which would mean the file was never rewound).
func TestFileSource_Loop_RewindsOnEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.bin")
	data := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	src := NewFileSource(model.MediaConfig{
		Kind: model.SourceKindFile, Path: path, Loop: true,
	})
	rc, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	// buf smaller than data so one pass needs at least two reads.
	buf := make([]byte, 3)
	// Pass 1: read 2×3 = 6 bytes = first 6 of data[0..5].
	var got []byte
	for i := 0; i < 2; i++ {
		n, err := rc.Read(buf)
		if err != nil {
			t.Fatalf("pass1 read %d: unexpected err %v", i, err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, data[:6]) {
		t.Fatalf("pass1: got %v want %v", got, data[:6])
	}
	// Pass 2: read 3 bytes → file returns (0,EOF) → rewind → (0,nil).
	// The next read starts the new pass from byte 0.
	got = nil
	n3, err3 := rc.Read(buf)
	if err3 != nil {
		t.Fatalf("pass2 first read: unexpected err %v", err3)
	}
	if n3 != 0 {
		t.Fatalf("pass2 first read: n=%d, want 0 (rewind happened)", n3)
	}
	// Pass 2, second read: must return data[0..3] again (rewound).
	n4, err4 := rc.Read(buf)
	if err4 != nil {
		t.Fatalf("pass2 second read: unexpected err %v", err4)
	}
	got = append(got, buf[:n4]...)
	if !bytes.Equal(got, data[:3]) {
		t.Fatalf("pass2: got %v want %v (rewind failed)", got, data[:3])
	}
	// Pass 3, first read: data[3..5] — confirming the loop continues.
	n5, err5 := rc.Read(buf)
	if n5 != 3 || err5 != nil {
		t.Fatalf("pass3 first read: got (%d,%v), want (3,nil)", n5, err5)
	}
	if !bytes.Equal(buf[:n5], data[3:6]) {
		t.Fatalf("pass3 data mismatch: got %v want %v", buf[:n5], data[3:6])
	}
}

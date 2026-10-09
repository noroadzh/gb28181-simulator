package model

import (
	"strings"
	"testing"
)

func TestESFrameValidate(t *testing.T) {
	t.Run("empty payload is rejected", func(t *testing.T) {
		f := ESFrame{Payload: []byte{}}
		err := f.Validate()
		if err == nil {
			t.Fatal("expected error for empty payload, got nil")
		}
		if !strings.Contains(err.Error(), "payload is empty") {
			t.Fatalf("error does not name the field: %v", err)
		}
	})

	t.Run("non-empty payload passes", func(t *testing.T) {
		f := NewESFrame([]byte{0x00, 0x00, 0x01, 0x65}, 900)
		err := f.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !f.HasPTS() {
			t.Error("NewESFrame must mark the frame as carrying a PTS")
		}
	})
}

func TestNewESFramePTS(t *testing.T) {
	f := NewESFrame([]byte{0x01}, 45000)
	if f.PTS != 45000 {
		t.Errorf("PTS = %d, want 45000", f.PTS)
	}
	if !f.HasPTS() {
		t.Error("HasPTS = false for a frame built with NewESFrame, want true")
	}
}

func TestPSFrameValidate(t *testing.T) {
	err := (PSFrame{Payload: []byte{}}).Validate()
	if err == nil {
		t.Error("empty PS payload must be an error, got nil")
	}
	err = (PSFrame{Payload: []byte{0x00, 0x00}}).Validate()
	if err != nil {
		t.Errorf("non-empty PS payload rejected: %v", err)
	}
}

func TestRTPPacketValidate(t *testing.T) {
	err := (RTPPacket{Payload: []byte{}}).Validate()
	if err == nil {
		t.Error("empty RTP payload must be an error, got nil")
	}
	err = (RTPPacket{Payload: []byte{0x01}}).Validate()
	if err != nil {
		t.Errorf("non-empty RTP payload rejected: %v", err)
	}
}

func TestMediaConfigValidate(t *testing.T) {
	t.Run("missing kind is an error", func(t *testing.T) {
		c := MediaConfig{Path: "x"}
		err := c.Validate()
		if err == nil {
			t.Error("expected error for empty kind, got nil")
		}
	})

	t.Run("missing path is an error", func(t *testing.T) {
		c := MediaConfig{Kind: SourceKindFile}
		err := c.Validate()
		if err == nil {
			t.Error("expected error for empty path, got nil")
		}
	})

	t.Run("defaults applied", func(t *testing.T) {
		c := MediaConfig{Kind: SourceKindFile, Path: "a.ps"}.Normalize()
		err := c.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.MTU != 1400 {
			t.Errorf("MTU = %d, want default 1400", c.MTU)
		}
		if c.FPS != 25 {
			t.Errorf("FPS = %d, want default 25", c.FPS)
		}
		if c.Clock != 90000 {
			t.Errorf("Clock = %d, want default 90000", c.Clock)
		}
	})

	t.Run("explicit values preserved", func(t *testing.T) {
		c := MediaConfig{Kind: SourceKindSynthetic, Path: "s", MTU: 1000, FPS: 30}
		err := c.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.MTU != 1000 || c.FPS != 30 {
			t.Errorf("explicit values not preserved: MTU=%d FPS=%d", c.MTU, c.FPS)
		}
	})
}

func TestSourceKindsAreStableStrings(t *testing.T) {
	if SourceKindFile != "file" || SourceKindRTSP != "rtsp" ||
		SourceKindHLS != "hls" || SourceKindSynthetic != "synthetic" {
		t.Error("source kind constants drifted from their configuration strings")
	}
}

func TestMediaConfigNormalizeLocalFileAlias(t *testing.T) {
	c := MediaConfig{Kind: "local_file", Path: "/data/v.mp4"}.Normalize()
	if c.Kind != SourceKindFile {
		t.Errorf("Normalize kind = %q, want %q", c.Kind, SourceKindFile)
	}
	if c.Path != "/data/v.mp4" {
		t.Errorf("Normalize path = %q, want unchanged", c.Path)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("normalized local_file should pass Validate: %v", err)
	}
}

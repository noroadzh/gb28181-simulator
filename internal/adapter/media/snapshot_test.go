package media

import (
	"bytes"
	"errors"
	"image/jpeg"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// The synthetic source already produces pixels, so a snapshot is a draw plus
// an encode — no decoder involved.
func TestSyntheticRenderer_RendersJPEG(t *testing.T) {
	data, mime, err := NewSyntheticRenderer().RenderFrame(model.MediaConfig{Kind: model.SourceKindSynthetic})
	if err != nil {
		t.Fatalf("RenderFrame: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("media type = %q, want image/jpeg", mime)
	}
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("body does not start with the JPEG SOI marker: % X", data[:min(4, len(data))])
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != snapshotWidth || b.Dy() != snapshotHeight {
		t.Errorf("frame = %dx%d, want %dx%d", b.Dx(), b.Dy(), snapshotWidth, snapshotHeight)
	}
}

// A video source cannot be turned into a still without a decoder; saying so
// beats returning a broken picture.
func TestSyntheticRenderer_RefusesVideo(t *testing.T) {
	for _, kind := range []model.MediaSourceKind{
		model.SourceKindFile, model.SourceKindRTSP, model.SourceKindHLS,
	} {
		_, _, err := NewSyntheticRenderer().RenderFrame(model.MediaConfig{Kind: kind, Path: "x"})
		if !errors.Is(err, port.ErrFrameRenderUnsupported) {
			t.Errorf("kind %q: err = %v, want ErrFrameRenderUnsupported", kind, err)
		}
	}
}

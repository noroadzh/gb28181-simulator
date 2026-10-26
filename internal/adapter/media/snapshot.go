package media

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// snapshotWidth/Height are the dimensions of a rendered still. The synthetic
// source has no configured size, so the snapshot uses the same 4:3 frame the
// generator produces for streaming.
const (
	snapshotWidth  = 640
	snapshotHeight = 480
)

// SyntheticRenderer turns a synthetic media source into a still JPEG.
//
// Only sources that already produce pixels qualify: drawing a frame is a pure
// image operation. Anything that would need a video decoder (H.264, H.265,
// MPEG-4) is refused rather than silently returning a broken picture — this
// build is pure Go and carries no decoder.
type SyntheticRenderer struct{}

// NewSyntheticRenderer builds the still-frame renderer.
func NewSyntheticRenderer() *SyntheticRenderer { return &SyntheticRenderer{} }

// RenderFrame implements port.FrameRenderer.
func (r *SyntheticRenderer) RenderFrame(cfg model.MediaConfig) ([]byte, string, error) {
	if cfg.Kind != model.SourceKindSynthetic {
		return nil, "", fmt.Errorf("%w: kind %q needs a video decoder", port.ErrFrameRenderUnsupported, cfg.Kind)
	}
	src := NewSyntheticSource(cfg)
	img := src.renderFrame(snapshotWidth, snapshotHeight, time.Now().Unix())

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, "", fmt.Errorf("media: encode snapshot jpeg: %w", err)
	}
	return buf.Bytes(), "image/jpeg", nil
}

// Ensure the adapter satisfies the port at compile time.
var _ port.FrameRenderer = (*SyntheticRenderer)(nil)

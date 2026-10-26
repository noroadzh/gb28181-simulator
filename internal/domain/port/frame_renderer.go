// Package port — still-frame rendering for snapshots.
package port

import (
	"errors"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ErrFrameRenderUnsupported marks a media source the renderer cannot turn into
// a still image without a video decoder.
var ErrFrameRenderUnsupported = errors.New("port: frame render unsupported")

// ErrSnapshotNoMedia marks a channel with no media source configured at all.
var ErrSnapshotNoMedia = errors.New("port: no media source configured")

// FrameRenderer renders one still frame for a media configuration.
//
// It exists so the app layer can serve a snapshot without importing a media
// adapter: the adapter decides which sources it can draw, the app decides what
// a missing capability means over HTTP.
type FrameRenderer interface {
	// RenderFrame returns the encoded image and its media type
	// (e.g. "image/jpeg"). It returns ErrFrameRenderUnsupported when the
	// source needs a decoder this build does not have.
	RenderFrame(cfg model.MediaConfig) ([]byte, string, error)
}

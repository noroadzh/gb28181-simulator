// Package playback — stub PlaybackPort adapter.
//
// This is a minimal placeholder; a real implementation would talk to a
// downstream device over SIP MESSAGE or a proprietary protocol.
package playback

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time check.
var _ port.PlaybackPort = (*PortAdapter)(nil)

// PortAdapter is the default adapter; it always returns ErrPlaybackUnsupported.
type PortAdapter struct{}

// NewPortAdapter builds the adapter.
func NewPortAdapter() *PortAdapter { return &PortAdapter{} }

// Play starts a playback session. scale is the playback rate (1.0 normal,
// 0 paused, negative reverse); the stub refuses every request, including
// ones with an illegal rate (NaN / ±Inf).
func (*PortAdapter) Play(ctx context.Context, deviceID, channelID, startTime, endTime string, scale float64) (string, error) {
	return "", port.ErrPlaybackUnsupported
}

// Stop terminates an active playback session.
func (*PortAdapter) Stop(ctx context.Context, sessionID string) error {
	return port.ErrPlaybackUnsupported
}

// Query returns the current playback state.
func (*PortAdapter) Query(ctx context.Context, sessionID string) (port.PlaybackState, error) {
	return port.PlaybackState{}, port.ErrPlaybackUnsupported
}

// SetScale changes the playback rate of an existing session. The stub
// refuses — there is no session to operate on.
func (*PortAdapter) SetScale(ctx context.Context, sessionID string, scale float64) error {
	return port.ErrPlaybackUnsupported
}

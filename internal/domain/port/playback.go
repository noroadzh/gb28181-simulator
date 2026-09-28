// Package port — Playback control.
package port

import (
	"context"
	"fmt"
)

// ErrPlaybackUnsupported is returned by a PlaybackPort adapter that cannot
// handle the request (e.g. a stub).
var ErrPlaybackUnsupported = fmt.Errorf("port: playback unsupported")

// PlaybackPort controls playback sessions with downstream devices.
type PlaybackPort interface {
	// Play starts a playback session with the given device/channel and time
	// range. scale is the playback rate: 1.0 normal, 0 paused, negative for
	// reverse; NaN and ±Inf are refused with ErrPlaybackUnsupported. The
	// returned session id identifies the playback for later Stop / Query.
	Play(ctx context.Context, deviceID, channelID, startTime, endTime string, scale float64) (string, error)

	// Stop terminates an active playback session.
	Stop(ctx context.Context, sessionID string) error

	// Query returns the current playback state for the session.
	Query(ctx context.Context, sessionID string) (PlaybackState, error)

	// SetScale changes the playback rate of an existing session. sessionID
	// names the session returned by Play; scale must not be NaN or ±Inf.
	// Returns ErrPlaybackUnsupported when the implementation does not support
	// in-session rate changes; the caller should treat that as 400 so the
	// client knows it was the request that was refused, not the dialog.
	SetScale(ctx context.Context, sessionID string, scale float64) error
}

// PlaybackState represents the current playback session state.
type PlaybackState struct {
	SessionID string
	DeviceID  string
	ChannelID string
	Status    string  // playing / paused / stopped / error
	Scale     float64 // active playback rate; 1.0 unless adjusted
}

var _ PlaybackPort = (*noopPlaybackPort)(nil)

type noopPlaybackPort struct{}

func (noopPlaybackPort) Play(ctx context.Context, deviceID, channelID, startTime, endTime string, scale float64) (string, error) {
	return "", nil
}
func (noopPlaybackPort) Stop(ctx context.Context, sessionID string) error { return nil }
func (noopPlaybackPort) Query(ctx context.Context, sessionID string) (PlaybackState, error) {
	return PlaybackState{}, nil
}
func (noopPlaybackPort) SetScale(ctx context.Context, sessionID string, scale float64) error {
	return ErrPlaybackUnsupported
}

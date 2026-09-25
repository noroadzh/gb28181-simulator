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
	// range. The returned session id identifies the playback for later Stop /
	// Query calls.
	Play(ctx context.Context, deviceID, channelID, startTime, endTime string) (string, error)

	// Stop terminates an active playback session.
	Stop(ctx context.Context, sessionID string) error

	// Query returns the current playback state for the session.
	Query(ctx context.Context, sessionID string) (PlaybackState, error)
}

// PlaybackState represents the current playback session state.
type PlaybackState struct {
	SessionID string
	DeviceID  string
	ChannelID string
	Status    string // playing / paused / stopped / error
}

var _ PlaybackPort = (*noopPlaybackPort)(nil)

type noopPlaybackPort struct{}

func (noopPlaybackPort) Play(ctx context.Context, deviceID, channelID, startTime, endTime string) (string, error) {
	return "", nil
}
func (noopPlaybackPort) Stop(ctx context.Context, sessionID string) error { return nil }
func (noopPlaybackPort) Query(ctx context.Context, sessionID string) (PlaybackState, error) {
	return PlaybackState{}, nil
}

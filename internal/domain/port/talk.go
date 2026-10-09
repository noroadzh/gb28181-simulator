// Package port — Talk (voice intercom) port.
//
// The TalkPort is the platform-large side of a GB28181 voice intercom dialog:
// the platform receives a SIP INVITE from a downstream device offering audio
// (PCMU/PCMA per the standard audio profile) and answers with 200 OK after
// picking a local RTP port. Audio is then streamed from the WebRTC/MediaStream
// captured by the Web UI over WebSocket-encoded PCMU frames into the device.
//
// The port is symmetric — Start/Stop apply regardless of which side initiated
// the dialog — and uses an opaque session ID that the Web layer carries back
// to correlate messages.
package port

import (
	"context"
	"fmt"
)

// ErrTalkUnsupported is returned by an adapter that cannot host a talk session
// (e.g. when the embedded device is not a camera with a microphone).
var ErrTalkUnsupported = fmt.Errorf("port: talk unsupported")

// TalkPort manages voice intercom sessions between the platform and a
// downstream device.
type TalkPort interface {
	// StartInvite issues a SIP INVITE to the device for the given channel.
	// The returned session id identifies the talk for later audio streaming,
	// Stop calls, and event correlation. audioCodec is the negotiated codec
	// (currently only "PCMU" / "PCMA" are accepted; the implementation
	// SHOULD refuse others with ErrTalkUnsupported).
	//
	// The returned rtpPort is the local UDP port chosen by the platform for
	// receiving the device's audio RTP packets; the device will be told this
	// port via the SDP c= line. A return value of 0 means no audio will be
	// delivered and the session is audio-receive only.
	StartInvite(ctx context.Context, deviceID, channelID, audioCodec string) (sessionID string, rtpPort int, err error)

	// AcceptBye sends a BYE for an active session started by either side.
	// Closing a non-existent session is a no-op and returns nil.
	Stop(ctx context.Context, sessionID string) error

	// SendAudio pushes one PCMU frame (8 kHz, mono, 160 samples = 20 ms) to
	// the device side of the session. Returns ErrTalkUnsupported if the
	// session was started with audio-receive-only, or model.ErrNotFound if
	// the session has already ended.
	SendAudio(ctx context.Context, sessionID string, pcm []byte) error

	// List returns the IDs of currently active talk sessions.
	List(ctx context.Context) []string
}

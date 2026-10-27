// Package port — live pull (a platform pulling a device's real-time stream).
package port

import (
	"errors"
	"fmt"
)

// ErrLiveUnsupported signals that this node cannot pull a stream at all —
// a device is the source side, and a platform without media wiring has
// nothing to pull with. HTTP callers map it to 501.
var ErrLiveUnsupported = errors.New("port: live pull unsupported")

// ErrLiveNoTarget signals that the platform has no single downstream device
// to pull from for the channel (none online, or several and the channel
// names no owner). HTTP callers map it to 404.
var ErrLiveNoTarget = errors.New("port: no downstream device to pull from")

// ErrLiveRefused signals that the device answered the INVITE with a final
// non-2xx, or with an answer that cannot carry media. HTTP callers map it to
// 502: the device exists, it just will not send this stream.
var ErrLiveRefused = errors.New("port: device refused the live stream")

// LiveSessionInfo is what a live start reports back.
type LiveSessionInfo struct {
	// SessionID identifies one pull for stop calls and logs.
	SessionID string
	// Transport is "udp" or "tcp".
	Transport string
	// LocalPort is the port the platform receives media on.
	LocalPort int
	// SSRC is the stream identifier the device declared, if any.
	SSRC uint32
}

// String renders a log-safe one-line summary.
func (i LiveSessionInfo) String() string {
	return fmt.Sprintf("live<%s %s:%d ssrc=%d>", i.SessionID, i.Transport, i.LocalPort, i.SSRC)
}

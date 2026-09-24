// Package model — WireEvent.
package model

import (
	"fmt"
	"net"
	"time"
)

// Direction tags a WireEvent as either transmit ("t") or receive ("r").
// Kept as a one-byte string rather than an enum so audit log lines stay
// compact and grep-friendly.
type Direction string

const (
	// DirTransmit is a packet the local process sent onto the wire.
	DirTransmit Direction = "t"
	// DirReceive is a packet the local process read from the wire.
	DirReceive Direction = "r"
)

// WireEvent is the immutable audit record of one observed SIP message on
// the wire (either direction). Adapters in the audit package construct
// it; downstream sinks (log, pcap, replay) consume it.
type WireEvent struct {
	at        time.Time
	direction Direction
	transport string // "udp" / "tcp" / "tls"
	local     net.Addr
	peer      net.Addr
	size      int
	preview   string // first 256 bytes, redacted of Authorization/Credentials
}

// NewWireEvent builds a WireEvent. preview MUST be at most 256 bytes;
// longer previews are truncated. transport MUST be non-empty.
func NewWireEvent(at time.Time, dir Direction, transport string, local, peer net.Addr, size int, preview string) (WireEvent, error) {
	if transport == "" {
		return WireEvent{}, fmt.Errorf("model: WireEvent.transport is empty")
	}
	if size < 0 {
		return WireEvent{}, fmt.Errorf("model: WireEvent.size %d negative", size)
	}
	if len(preview) > 256 {
		preview = preview[:256]
	}
	return WireEvent{
		at:        at,
		direction: dir,
		transport: transport,
		local:     local,
		peer:      peer,
		size:      size,
		preview:   preview,
	}, nil
}

// At returns the observation time.
func (w WireEvent) At() time.Time { return w.at }

// Direction returns transmit/receive.
func (w WireEvent) Direction() Direction { return w.direction }

// Transport returns "udp" / "tcp" / "tls".
func (w WireEvent) Transport() string { return w.transport }

// Local returns the local socket address; may be nil.
func (w WireEvent) Local() net.Addr { return w.local }

// Peer returns the remote socket address; may be nil.
func (w WireEvent) Peer() net.Addr { return w.peer }

// Size returns the wire-level byte count of the message.
func (w WireEvent) Size() int { return w.size }

// Preview returns the first ≤256 bytes, redacted of sensitive headers.
// Empty string is acceptable (some transports may opt out of previews).
func (w WireEvent) Preview() string { return w.preview }

// String returns a one-line log-friendly summary.
func (w WireEvent) String() string {
	peer := "<nil>"
	if w.peer != nil {
		peer = w.peer.String()
	}
	return fmt.Sprintf("audit[%s %s %dB peer=%s]", w.direction, w.transport, w.size, peer)
}

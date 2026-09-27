// Package port — CaptureStore: per-node capture ring buffer.
package port

import (
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// CaptureEvent is one SIP message buffered for capture or pcap export.
type CaptureEvent struct {
	// NodeID is the owning node identifier. Transport emits it from
	// siptransport when the socket is constructed with WithNodeID.
	NodeID string

	// Direction is "t" for transmit, "r" for receive, matching the audit
	// layer's Direction constants.
	Direction model.Direction

	// Local is the transport's local endpoint (host:port).
	Local string

	// Remote is the peer endpoint (host:port).
	Remote string

	// Transport is the scheme ("udp"/"tcp"/"tls").
	Transport string

	// Bytes is the raw serialised SIP message.
	Bytes []byte

	// At is when the event was captured.
	At time.Time
}

// CaptureStore exposes per-node capture buffers. One store owns all nodes;
// the implementation fans each node's events to its own ring. Queries never
// mutate the buffer.
type CaptureStore interface {
	// Append stores the event. The store MUST drop the event rather than
	// block when the node's ring is full; a silent drop is acceptable for
	// the "buffer full" case so the transport never blocks.
	Append(nodeID string, evt CaptureEvent)

	// Query returns up to limit events for nodeID ordered oldest-first
	// among the returned subset. The ring is not mutated.
	Query(nodeID string, limit int) []CaptureEvent

	// Subscribe returns a fan-out channel of live events for nodeID and
	// a cancel function. Events that would exceed the receiver channel
	// buffer are dropped for that receiver but still delivered to other
	// subscribers. Closing the returned channel is done by the store only
	// after cancel has been called; new subscribers after cancel see an
	// immediately closed channel until Subscribe is called again.
	Subscribe(nodeID string) (<-chan CaptureEvent, func())

	// PCAP writes a pcap file (LINKTYPE_ETHERNET) with all currently
	// buffered events for nodeID, oldest first. An empty buffer returns a
	// valid empty pcap (global header, no packets). The returned bytes can
	// be streamed directly with Content-Type application/vnd.tcpdump.pcap.
	PCAP(nodeID string) ([]byte, error)
}

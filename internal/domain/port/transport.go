// Package port — SIPTransport.
package port

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// SIPTransport is the outbound adapter for raw SIP signalling: send a
// pre-built request to an explicit destination, receive the next
// request/response together with the peer it arrived from, and close the
// transport cleanly on shutdown.
//
// The interface is intentionally transport-agnostic; concrete adapters in
// internal/adapter/siptransport and internal/adapter/sip fulfil it with
// UDP, TCP and TLS.
type SIPTransport interface {
	// Send transmits a request to dst ("host:port", optionally prefixed by
	// a transport scheme). The destination is explicit: adapters MUST NOT
	// infer it from msg.URI(). The adapter copies msg so callers may mutate
	// it after the call returns. An empty dst is an error. Returns a
	// transport-level error (network unreachable, peer closed, etc.);
	// SIP-level status codes are surfaced via Receive, not here.
	Send(ctx context.Context, msg model.Message, dst string) error

	// Receive blocks until a request or response arrives, the context is
	// cancelled, or the transport is closed. It returns the message and the
	// address it arrived from ("host:port"), which callers may pass
	// straight back to Send to answer. An adapter that cannot determine the
	// peer address MUST return an error rather than an empty address, so a
	// caller never replies to the wrong destination. Callers must tolerate
	// ctx errors and translate them to their own retry/backoff policy.
	Receive(ctx context.Context) (model.Message, string, error)

	// LocalAddr reports the address the transport is bound to, in the form
	// "host:port".  Outbound constructors (NOTIFY, MESSAGE from the platform
	// side, etc.) need it to fill the Via header without having to maintain a
	// parallel address field on the platform.
	LocalAddr() string

	// Close releases all underlying resources (sockets, goroutines).
	// Idempotent; calling it twice MUST NOT panic.
	Close() error
}

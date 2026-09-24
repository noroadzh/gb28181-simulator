// Package port — SIPTransport.
package port

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// SIPTransport is the outbound adapter for raw SIP signalling: send a
// pre-built request, receive the next request/response, and close the
// transport cleanly on shutdown.
//
// The interface is intentionally transport-agnostic; concrete adapters in
// internal/adapter/siptransport and internal/adapter/sip fulfil it with
// UDP, TCP and TLS.
type SIPTransport interface {
	// Send transmits a request. The adapter resolves the destination from
	// msg.URI() and copies msg so callers may mutate it after the call
	// returns. Returns a transport-level error (network unreachable, peer
	// closed, etc.); SIP-level status codes are surfaced via Receive, not
	// here.
	Send(ctx context.Context, msg model.Message) error

	// Receive blocks until a request or response arrives, the context is
	// cancelled, or the transport is closed. Callers must tolerate ctx
	// errors and translate them to their own retry/backoff policy.
	Receive(ctx context.Context) (model.Message, error)

	// Close releases all underlying resources (sockets, goroutines).
	// Idempotent; calling it twice MUST NOT panic.
	Close() error
}
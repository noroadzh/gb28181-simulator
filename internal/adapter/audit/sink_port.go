package audit

import (
	"net"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// SinkAdapter adapts the legacy audit.Emitter (Emit(WireEvent) without error)
// to the port.AuditSink contract (Emit(model.WireEvent) error).
//
// Legacy WireEvent uses string endpoints and raw bytes; model.WireEvent
// exposes them via getters and carries a transport label and a preview
// string. The adapter preserves direction, local/remote addresses (as
// strings), raw bytes (approximated via preview), and timestamp.
// Errors from the underlying Emit are swallowed and surfaced as nil because
// the legacy contract never propagates failures.
type SinkAdapter struct {
	inner Emitter
}

// NewSinkAdapter wraps an Emitter to satisfy port.AuditSink. A nil Emitter is
// treated as a no-op so the adapter is always safe to call.
func NewSinkAdapter(e Emitter) *SinkAdapter {
	if e == nil {
		e = NopEmitter{}
	}
	return &SinkAdapter{inner: e}
}

// Emit forwards evt to the underlying emitter. It never returns an error;
// see the type comment for the rationale.
func (a *SinkAdapter) Emit(evt model.WireEvent) error {
	var local, remote net.Addr
	if evt.Local() != nil {
		local = evt.Local()
	}
	if evt.Peer() != nil {
		remote = evt.Peer()
	}
	a.inner.Emit(WireEvent{
		Direction: string(evt.Direction()),
		Local:     netAddrString(local),
		Remote:    netAddrString(remote),
		Bytes:     []byte(evt.Preview()),
		Timestamp: evt.At(),
	})
	return nil
}

// netAddrString formats a net.Addr as host:port or "" when nil.
func netAddrString(a net.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}

// Compile-time assertion that *SinkAdapter satisfies port.AuditSink.
var _ port.AuditSink = (*SinkAdapter)(nil)

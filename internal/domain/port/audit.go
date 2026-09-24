// Package port — AuditSink / WireEvent re-export.
package port

import "github.com/your-org/gb28181-simulator/internal/domain/model"

// AuditSink consumes immutable WireEvent values produced by the transport
// layer. Adapters (log/pcap/replay) implement it; the transport only knows
// the interface.
type AuditSink interface {
	// Emit handles one event. Implementations MUST be safe for concurrent
	// use. A sink that returns an error must not stop the transport; the
	// error is logged at debug level by the caller.
	Emit(evt model.WireEvent) error
}

// Compile-time marker so adapters that import port get model.WireEvent
// visible without an extra import line in the consumer file.
var _ = model.WireEvent{}
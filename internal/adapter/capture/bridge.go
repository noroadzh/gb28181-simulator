// Package capture — bridge adapts the process-global audit emitter into a
// CaptureStore by converting each emitted audit.WireEvent into a
// port.CaptureEvent. The conversion drops nothing; fields that the transport
// does not populate (for example Remote on the receive path) are left empty
// rather than synthesised.
package capture

import (
	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// AuditBridge returns an audit.Emitter that appends every WireEvent into the
// supplied CaptureStore. The store must outlive the emitter.
func AuditBridge(store port.CaptureStore) audit.Emitter {
	return audit.EmitterFunc(func(e audit.WireEvent) {
		store.Append(e.NodeID, port.CaptureEvent{
			NodeID:    e.NodeID,
			Direction: model.Direction(e.Direction),
			Local:     e.Local,
			Remote:    e.Remote,
			Bytes:     e.Bytes,
			At:        e.Timestamp,
		})
	})
}

// The store must outlive the emitter.

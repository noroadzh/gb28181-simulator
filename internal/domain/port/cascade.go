// Package port — cascade.
package port

import (
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// CascadeHandler is the boundary through which outbound SIP messages are
// offered to the cascade subsystem before they hit the wire. Implementations
// MUST be safe for concurrent use.
type CascadeHandler interface {
	// Forward receives a message destined for dstDeviceID and returns the
	// next deviceID the message should be routed to, and any SIP headers
	// that must be added before sending. An empty nextDeviceID means
	// "send to the original dstDeviceID unchanged" — the handler did not
	// find a closer hop.
	//
	// When the handler returns true in the second result, the caller MUST
	// NOT continue normal processing because the handler has already
	// forwarded the message to a fallback path (e.g. upstream to
	// CascadeParent).
	Forward(fromNodeID model.NodeID, msg model.Message, dstDeviceID string) (nextDeviceID string, additionalHeaders []model.Header, handled bool, err error)
}

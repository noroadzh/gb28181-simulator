// Package port — control-plane sentinels (PTZ, record, snapshot).
//
// These errors are returned by NodeService / HTTP handlers when the request
// cannot be served — either because the node is not the expected kind, or
// because the requested control is not implemented in this build. The
// errors carry the node+channel identifiers and a free-form Reason via
// fmt.Errorf("%w", ErrPTZUnsupported{...}) so HTTP handlers can render a
// precise 4xx/5xx body.
package port

import (
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ErrPTZUnsupported signals that the platform cannot send a PTZ command to
// the device — typically because the node is not a device, or the device
// does not expose PTZ.
type ErrPTZUnsupported struct {
	NodeID    model.NodeID
	ChannelID string
	Reason    string
}

func (e ErrPTZUnsupported) Error() string {
	return "port: ptz unsupported for node " + e.NodeID.String() +
		" channel " + e.ChannelID + ": " + e.Reason
}

// ErrRecordUnsupported signals that the device does not have a record
// catalog or the platform cannot list records for the given channel.
type ErrRecordUnsupported struct {
	NodeID    model.NodeID
	ChannelID string
	Reason    string
}

func (e ErrRecordUnsupported) Error() string {
	return "port: record unsupported for node " + e.NodeID.String() +
		" channel " + e.ChannelID + ": " + e.Reason
}

// ErrSnapshotUnsupported signals that the platform cannot capture a
// snapshot for the given channel.
type ErrSnapshotUnsupported struct {
	NodeID    model.NodeID
	ChannelID string
	Reason    string
}

func (e ErrSnapshotUnsupported) Error() string {
	return "port: snapshot unsupported for node " + e.NodeID.String() +
		" channel " + e.ChannelID + ": " + e.Reason
}

// PTZUnsupported is a convenience wrapper that returns the typed error as
// a regular error value, so call sites can `return ptzUnsupported(...)`.
func PTZUnsupported(n model.NodeID, ch, reason string) error {
	return ErrPTZUnsupported{NodeID: n, ChannelID: ch, Reason: reason}
}

func RecordUnsupported(n model.NodeID, ch, reason string) error {
	return ErrRecordUnsupported{NodeID: n, ChannelID: ch, Reason: reason}
}

func SnapshotUnsupported(n model.NodeID, ch, reason string) error {
	return ErrSnapshotUnsupported{NodeID: n, ChannelID: ch, Reason: reason}
}

// Ensure unused import is referenced (fmt used by callers; we keep the
// blank reference to keep this file self-contained when helpers move).
var _ = fmt.Sprintf

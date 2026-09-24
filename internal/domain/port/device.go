// Package port — DownstreamRegistry.
package port

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// DownstreamRegistry is a platform node's online device table: the devices
// that registered with it and have not left.
//
// The table is partitioned by node: one platform's devices are invisible to
// another, and clearing one node's table must not touch its neighbours.
// Implementations MUST be safe for concurrent use — the serving goroutine
// writes while HTTP readers read.
type DownstreamRegistry interface {
	// Upsert records dev under nodeID, replacing any row for the same
	// device id. Re-registering a device updates it rather than
	// duplicating it.
	Upsert(ctx context.Context, nodeID model.NodeID, dev model.DownstreamDevice) error

	// Remove drops the row for deviceID under nodeID. Removing an absent
	// device is a no-op, not an error: unregistering twice is harmless.
	Remove(ctx context.Context, nodeID model.NodeID, deviceID string) error

	// Lookup returns the row for deviceID under nodeID. The second result
	// is false when there is none.
	Lookup(ctx context.Context, nodeID model.NodeID, deviceID string) (model.DownstreamDevice, bool)

	// List returns every row under nodeID ordered by device id, so callers
	// (HTTP, tests) get a stable sequence without sorting themselves.
	List(ctx context.Context, nodeID model.NodeID) []model.DownstreamDevice

	// Clear drops every row under nodeID. It is what stops a node that
	// was stopped from reporting devices it can no longer reach.
	Clear(ctx context.Context, nodeID model.NodeID) error
}

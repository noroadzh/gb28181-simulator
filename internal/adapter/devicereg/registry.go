// Package devicereg — the in-memory online device table.
//
// A platform-large node keeps one table of the downstreams that registered
// with it. This adapter is that table for the whole process: partitioned by
// node, guarded by a RWMutex, and deliberately small — the serving
// goroutine writes, HTTP readers read, and nothing here knows a thing about
// SIP.
package devicereg

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Registry is the process-wide online device table.
//
// The zero value is NOT usable; construct one with New.
type Registry struct {
	mu     sync.RWMutex
	byNode map[string]map[string]model.DownstreamDevice
}

// New returns an empty table.
func New() *Registry {
	return &Registry{byNode: make(map[string]map[string]model.DownstreamDevice)}
}

// Compile-time check that the table satisfies the domain port.
var _ port.DownstreamRegistry = (*Registry)(nil)

// Upsert records dev under nodeID, replacing any row for the same device
// id: a device that re-registers refreshes its row instead of appearing
// twice.
func (r *Registry) Upsert(ctx context.Context, nodeID model.NodeID, dev model.DownstreamDevice) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("devicereg: upsert: %w", err)
	}
	if !dev.HasDevice() {
		return fmt.Errorf("devicereg: upsert: empty device for node %s", nodeID)
	}
	key := nodeID.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byNode[key] == nil {
		r.byNode[key] = make(map[string]model.DownstreamDevice)
	}
	r.byNode[key][dev.DeviceID()] = dev
	return nil
}

// Remove drops the row for deviceID under nodeID. Removing a device that
// is not there is a no-op: unregistering twice is harmless, and the caller
// has nothing to learn from being told off.
func (r *Registry) Remove(ctx context.Context, nodeID model.NodeID, deviceID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("devicereg: remove: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := r.byNode[nodeID.String()]
	if rows == nil {
		return nil
	}
	delete(rows, deviceID)
	// Do not keep an empty map around: a node with no devices is
	// indistinguishable from one that never served.
	if len(rows) == 0 {
		delete(r.byNode, nodeID.String())
	}
	return nil
}

// Lookup returns the row for deviceID under nodeID.
func (r *Registry) Lookup(ctx context.Context, nodeID model.NodeID, deviceID string) (model.DownstreamDevice, bool) {
	if err := ctx.Err(); err != nil {
		return model.DownstreamDevice{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	dev, ok := r.byNode[nodeID.String()][deviceID]
	return dev, ok
}

// List returns every row under nodeID ordered by device id, so HTTP
// responses and test assertions are stable without sorting of their own.
func (r *Registry) List(ctx context.Context, nodeID model.NodeID) []model.DownstreamDevice {
	if err := ctx.Err(); err != nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := r.byNode[nodeID.String()]
	out := make([]model.DownstreamDevice, 0, len(rows))
	for _, dev := range rows {
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID() < out[j].DeviceID() })
	return out
}

// Clear drops every row under nodeID. Stopping a platform must not leave
// behind devices it can no longer reach.
func (r *Registry) Clear(ctx context.Context, nodeID model.NodeID) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("devicereg: clear: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byNode, nodeID.String())
	return nil
}

// Count returns how many devices a node currently has. It exists for tests
// and for the odd operational log line, not for the domain.
func (r *Registry) Count(nodeID model.NodeID) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byNode[nodeID.String()])
}

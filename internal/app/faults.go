// Package app — in-memory fault store: runtime misbehaviour profiles per
// node plus the per-node counters the HTTP API and the scenario engine
// (#15) assert on (design D3/D8).
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time check: the in-memory store satisfies the port contract.
var _ port.FaultStore = (*FaultStoreAdapter)(nil)

// nodeGetter is the narrow slice of port.NodeRegistry the fault store
// needs: existence validation only (interface segregation — tests fake one
// method instead of the whole registry).
type nodeGetter interface {
	Get(ctx context.Context, id model.NodeID) (model.Node, bool)
}

// FaultStoreAdapter is the process-wide fault state. Profiles are runtime
// state — a restart clears them — and counters live next to the profile so
// that Clear resets both together (port.FaultStore contract).
type FaultStoreAdapter struct {
	// registry validates node existence: arming faults on an unknown node
	// is a typo, not a feature, so Install rejects it.
	registry nodeGetter

	log *slog.Logger

	mu       sync.RWMutex
	profiles map[model.NodeID]model.FaultProfile
	counters map[model.NodeID]map[model.FaultAction]uint64
}

// NewFaultStore builds an empty fault store bound to a node-existence
// checker. Pass the app's node registry in production. The logger is
// decorated with the "fault_store" subsystem tag so MultiHandler can apply
// per-module overrides without forcing every caller to thread attrs.
func NewFaultStore(registry nodeGetter, log *slog.Logger) *FaultStoreAdapter {
	if log == nil {
		log = slog.Default()
	}
	return &FaultStoreAdapter{
		registry: registry,
		log:      log.With("component", "internal/app", "subsystem", "fault_store"),
		profiles: make(map[model.NodeID]model.FaultProfile),
		counters: make(map[model.NodeID]map[model.FaultAction]uint64),
	}
}

// Install validates and replaces the profile for nodeID. An unknown node or
// an invalid profile is an error; a valid replace resets that node's
// counters because the old profile's numbers belong to the old behaviour.
func (s *FaultStoreAdapter) Install(ctx context.Context, nodeID model.NodeID, profile model.FaultProfile) error {
	if err := profile.Validate(); err != nil {
		s.log.Debug("fault install rejected: invalid profile",
			"node_id", nodeID,
			"err", err)
		return fmt.Errorf("app: install fault on %s: %w", nodeID, err)
	}
	if _, ok := s.registry.Get(ctx, nodeID); !ok {
		s.log.Debug("fault install rejected: unknown node",
			"node_id", nodeID)
		return fmt.Errorf("app: install fault on %s: %w", nodeID, model.ErrUnknownNode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[nodeID] = profile
	delete(s.counters, nodeID)
	s.log.Debug("fault installed",
		"node_id", nodeID,
		"profile", profile)
	return nil
}

// Clear removes the profile and the counters; clearing a node without a
// profile is a no-op (nil), mirroring the port contract.
func (s *FaultStoreAdapter) Clear(_ context.Context, nodeID model.NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.profiles, nodeID)
	delete(s.counters, nodeID)
	s.log.Debug("fault cleared", "node_id", nodeID)
	return nil
}

// Get returns the installed profile and whether one exists. A node with no
// profile yields the zero profile — the "behave normally" profile.
func (s *FaultStoreAdapter) Get(_ context.Context, nodeID model.NodeID) (model.FaultProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.profiles[nodeID]
	if !ok {
		return model.FaultProfile{}, false
	}
	return p, true
}

// Record bumps the per-node counter for an action that actually fired. The
// acceptor gate calls this on every canned/delay/drop/blackhole outcome.
// The hot-loop counter is not logged per call; callers emit aggregate
// snapshots via the API when they need to inspect it.
func (s *FaultStoreAdapter) Record(nodeID model.NodeID, action model.FaultAction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counters[nodeID] == nil {
		s.counters[nodeID] = make(map[model.FaultAction]uint64)
	}
	s.counters[nodeID][action]++
}

// FaultCounters returns a copy of the node's action counters. A node with
// no faults recorded yields an empty map, never nil-prefixed surprises.
func (s *FaultStoreAdapter) FaultCounters(nodeID model.NodeID) map[model.FaultAction]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[model.FaultAction]uint64, len(s.counters[nodeID]))
	for k, v := range s.counters[nodeID] {
		out[k] = v
	}
	return out
}

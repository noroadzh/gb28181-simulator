// Package port — FaultStore: runtime fault-injection state for nodes.
package port

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// FaultStore holds the fault profile installed per node. It is runtime
// state, not persistent configuration: a restart clears it. Implementations
// MUST be safe for concurrent use — the acceptor reads profiles on every
// inbound request while the HTTP API installs/clears them.
type FaultStore interface {
	// Install replaces (or installs) the profile for nodeID. An unknown
	// node returns an error so a typo cannot arm faults on nothing.
	Install(ctx context.Context, nodeID model.NodeID, profile model.FaultProfile) error

	// Clear removes the profile; clearing a node without one is a no-op
	// returning nil. Counters are reset together with the profile.
	Clear(ctx context.Context, nodeID model.NodeID) error

	// Get returns the installed profile and whether one exists. A node
	// with no profile yields (FaultProfile{}, false) — the zero profile is
	// the "behave normally" profile.
	Get(ctx context.Context, nodeID model.NodeID) (model.FaultProfile, bool)
}

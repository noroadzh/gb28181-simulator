// Package nodereg is the in-process node registry: a port.NodeRegistry
// implementation plus a port.NodeLifecycle implementation that binds and
// releases each node's signalling listener.
//
// Design D3: one listener per node, a map guarded by sync.RWMutex, and
// address uniqueness checked at registration time so two nodes can never
// silently share (or steal) a port.
package nodereg

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/adapter/cascade"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// Compile-time assertions: Registry satisfies the domain registry port and
// the status-advance port the app layer depends on.
var (
	_ port.NodeRegistry = (*Registry)(nil)
	_ port.NodeAdvancer = (*Registry)(nil)
)

// entry holds one registered node. The pointer indirection lets the
// lifecycle mutate the status in place under the registry lock without
// invalidating anything a caller already holds (model.Node is an immutable
// value, so readers can never observe a half-updated node).
type entry struct {
	node model.Node
}

// Registry is the process-wide catalogue of nodes. It is safe for
// concurrent use. Node identity is unique, and so is the signalling
// address: registering a second node on an address another node already
// holds is an error that names the incumbent (design D3).
//
// An optional cascade handler is refreshed whenever the node set changes so
// forwarding decisions always reflect the live parent/children graph.
type Registry struct {
	mu sync.RWMutex
	// nodes is keyed by the 20-digit node id.
	nodes map[string]*entry
	// addrIndex maps a signalling address to the id holding it, so the
	// uniqueness check is O(1) and cannot drift out of sync with nodes.
	addrIndex map[string]string
	// logger carries node_id on every line this registry emits, so two
	// nodes' records are always separable (spec: each node owns
	// independent log fields keyed by its node id).
	logger *slog.Logger
	// cascade receives topology refresh calls whenever nodes join or
	// leave. Nil means cascade is disabled.
	cascade *cascade.Handler
}

// New returns an empty registry that logs through the process logger.
func New() *Registry {
	return NewWithLogger(logging.L())
}

// NewWithLogger returns an empty registry that logs through l. A nil l falls
// back to the process logger, so a zero-value Registry is still usable.
func NewWithLogger(l *slog.Logger) *Registry {
	return &Registry{
		nodes:     make(map[string]*entry),
		addrIndex: make(map[string]string),
		logger:    l,
	}
}

// WithCascadeHandler attaches the cascade handler whose topology view this
// registry keeps in sync. Passing nil detaches. Every join and leave of a
// node rebuilds the handler's view, so forwarding decisions always reflect
// the live node set.
func (r *Registry) WithCascadeHandler(h *cascade.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cascade = h
	r.refreshCascadeTopologyLocked()
}

// refreshCascadeTopologyLocked rebuilds the cascade handler's topology from
// the profiles currently registered. Caller must hold r.mu. A nil handler
// means cascade is disabled and the call is a no-op.
func (r *Registry) refreshCascadeTopologyLocked() {
	if r.cascade == nil {
		return
	}
	profiles := make([]model.NodeProfile, 0, len(r.nodes))
	for _, e := range r.nodes {
		profiles = append(profiles, e.node.Profile())
	}
	r.cascade.WithTopology(cascade.NewTopologyMap(profiles))
}

// validateCascadeAcyclicLocked rejects a profile whose cascade relationships
// would close a loop through the nodes already registered: a self parent, a
// self child, or a parent chain that leads back to the new node. A parent
// that is not registered locally cannot close a loop and is allowed —
// cascades routinely point at platforms outside this process.
func (r *Registry) validateCascadeAcyclicLocked(profile model.NodeProfile) error {
	id := profile.ID().String()
	if parent := profile.CascadeParent(); parent != "" {
		if parent == id {
			return fmt.Errorf("nodereg: cascade parent of node %s is itself", id)
		}
		visited := map[string]struct{}{id: {}}
		for current := parent; current != ""; {
			if _, seen := visited[current]; seen {
				return fmt.Errorf(
					"nodereg: cascade cycle detected registering node %s: parent chain revisits %s",
					id, current)
			}
			visited[current] = struct{}{}
			ent, ok := r.nodes[current]
			if !ok {
				break // parent lives outside this process; the chain ends here
			}
			current = ent.node.Profile().CascadeParent()
		}
	}
	for _, child := range profile.CascadeChildren() {
		if child == id {
			return fmt.Errorf("nodereg: cascade child of node %s is itself", id)
		}
	}
	return nil
}

// log returns the registry's logger, never nil.
func (r *Registry) log() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return logging.L()
}

// Register adds a node built from profile and returns it in StatusIdle. It
// fails when the id is already registered, when the signalling address is
// already claimed by another node, or when the profile's cascade
// relationships would close a loop through the registered nodes.
func (r *Registry) Register(_ context.Context, profile model.NodeProfile) (model.Node, error) {
	id := profile.ID().String()
	if id == "" {
		return model.Node{}, fmt.Errorf("nodereg: empty node id")
	}
	node := model.NewNode(profile)

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.nodes[id]; ok {
		r.log().Warn("node register rejected: duplicate id", "node_id", id,
			"reason", "already registered", "current_status", existing.node.Status())
		return model.Node{}, fmt.Errorf("nodereg: node %s is already registered (status %s)",
			id, existing.node.Status())
	}
	if incumbent, ok := r.addrIndex[profile.Addr()]; ok {
		r.log().Warn("node register rejected: address claimed", "node_id", id,
			"reason", "address in use", "addr", profile.Addr(), "held_by", incumbent)
		return model.Node{}, fmt.Errorf(
			"nodereg: address %s is already claimed by node %s; node %s cannot bind it",
			profile.Addr(), incumbent, id)
	}
	if err := r.validateCascadeAcyclicLocked(profile); err != nil {
		r.log().Warn("node register rejected: cascade cycle", "node_id", id, "error", err.Error())
		return model.Node{}, err
	}
	r.nodes[id] = &entry{node: node}
	r.addrIndex[profile.Addr()] = id
	r.refreshCascadeTopologyLocked()
	r.log().Info("node registered", "node_id", id,
		"kind", profile.ID().Kind(), "addr", profile.Addr())
	return node, nil
}

// RecordRegistration stores the outcome of a registration on the node.
// It deliberately does not touch the status: recording data must not look
// like a lifecycle step, and the caller decides when to advance.
func (r *Registry) RecordRegistration(_ context.Context, id model.NodeID, result model.RegistrationResult) (model.Node, error) {
	key := id.String()

	r.mu.Lock()
	defer r.mu.Unlock()
	ent, ok := r.nodes[key]
	if !ok {
		return model.Node{}, fmt.Errorf("nodereg: unknown node %s", id)
	}
	updated, err := ent.node.WithRegistrationResult(result)
	if err != nil {
		return model.Node{}, fmt.Errorf("nodereg: record registration for node %s: %w", id, err)
	}
	ent.node = updated
	r.log().Info("node registration recorded", "node_id", key,
		"server", result.Server(), "expires", result.GrantedExpiry())
	return updated, nil
}

// MutateProfile replaces the node's profile with the result of fn applied to
// the current profile. fn runs inside the registry's write lock, so the
// read-modify-write is atomic against other mutators. A fn that refuses the
// mutation (error) leaves the stored profile untouched.
func (r *Registry) MutateProfile(_ context.Context, id model.NodeID,
	fn func(model.NodeProfile) (model.NodeProfile, error),
) (model.Node, error) {
	key := id.String()
	if fn == nil {
		return model.Node{}, fmt.Errorf("nodereg: nil profile mutator")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ent, ok := r.nodes[key]
	if !ok {
		return model.Node{}, fmt.Errorf("nodereg: unknown node %s", key)
	}
	np, err := fn(ent.node.Profile())
	if err != nil {
		return model.Node{}, fmt.Errorf("nodereg: mutate %s: %w", key, err)
	}
	updated := ent.node.WithProfile(np)
	ent.node = updated
	return updated, nil
}

// Unregister removes the node and releases its signalling address. Other
// nodes are untouched.
func (r *Registry) Unregister(_ context.Context, id model.NodeID) error {
	key := id.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.nodes[key]
	if !ok {
		return fmt.Errorf("nodereg: unknown node %s", key)
	}
	delete(r.nodes, key)
	delete(r.addrIndex, e.node.Profile().Addr())
	r.refreshCascadeTopologyLocked()
	r.log().Info("node unregistered", "node_id", key,
		"addr", e.node.Profile().Addr())
	return nil
}

// Get looks a node up by id.
func (r *Registry) Get(_ context.Context, id model.NodeID) (model.Node, bool) {
	key := id.String()
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.nodes[key]
	if !ok {
		return model.Node{}, false
	}
	return e.node, true
}

// List returns every registered node; the order is unspecified.
func (r *Registry) List(_ context.Context) []model.Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Node, 0, len(r.nodes))
	for _, e := range r.nodes {
		out = append(out, e.node)
	}
	return out
}

// Advance moves a node to status to, enforcing the legal-transition table.
// It is not part of port.NodeRegistry: it is the write path the lifecycle
// uses, and it goes through model's transition table so an illegal jump is
// still refused with model.ErrIllegalTransition. On an illegal transition
// the returned node is the unchanged current one.
func (r *Registry) Advance(_ context.Context, id model.NodeID, to model.Status) (model.Node, error) {
	key := id.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.nodes[key]
	if !ok {
		return model.Node{}, fmt.Errorf("nodereg: unknown node %s", key)
	}
	from := e.node.Status()
	next, err := e.node.WithStatus(to)
	if err != nil {
		r.log().Warn("node transition rejected", "node_id", key,
			"from", from, "to", to, "error", err)
		return e.node, err
	}
	e.node = next
	r.log().Info("node status changed", "node_id", key, "from", from, "to", to)
	return next, nil
}

// AddrOwner reports which node id holds addr, and whether any does. It is
// used by the lifecycle to describe conflicts without reaching into the
// registry's internals.
func (r *Registry) AddrOwner(addr string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.addrIndex[addr]
	return id, ok
}

// Package cascade implements the X-RoutePath / X-PreferredPath forwarding
// logic for GB/T 28181 §6.
//
// Design: the handler is a pure function over (route, topology); it owns no
// sockets and no goroutines, so it composes cleanly with the per-node
// transports owned by the nodereg lifecycle (design D3).
package cascade

import (
	"fmt"
	"strings"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time assertion: Handler satisfies the domain cascade port.
var _ port.CascadeHandler = (*Handler)(nil)

// Topology is the read-only view of the cascade graph the handler needs:
// for each node id, its parent and children. Implementations must be safe
// for concurrent reads; the default TopologyMap snapshot taken at
// construction time satisfies this trivially.
type Topology interface {
	// Parent returns the upstream deviceID of node, or "" when node has
	// no upstream configured.
	Parent(node string) string
	// Children returns the downstream deviceIDs of node in routing
	// order. The returned slice must not be mutated by the caller.
	Children(node string) []string
}

// TopologyMap is a snapshot of Topology built from a set of NodeProfiles.
// It is immutable after construction, so it is trivially safe for
// concurrent use by any number of Handler instances.
type TopologyMap struct {
	parent   map[string]string
	children map[string][]string
}

// NewTopologyMap builds a snapshot from profiles. Profiles without cascade
// settings are still indexed (as leaves with no parent), so lookups never
// have to check existence.
func NewTopologyMap(profiles []model.NodeProfile) *TopologyMap {
	t := &TopologyMap{
		parent:   make(map[string]string, len(profiles)),
		children: make(map[string][]string, len(profiles)),
	}
	for _, p := range profiles {
		id := p.ID().String()
		t.parent[id] = p.CascadeParent()
		t.children[id] = p.CascadeChildren()
	}
	return t
}

// Parent implements Topology.
func (t *TopologyMap) Parent(node string) string { return t.parent[node] }

// Children implements Topology.
func (t *TopologyMap) Children(node string) []string { return t.children[node] }

// Handler routes outbound messages through the cascade graph. It is safe
// for concurrent use by any number of goroutines.
type Handler struct {
	mu   sync.RWMutex
	topo Topology
}

// New returns a Handler operating on topo. A nil topo is accepted (every
// lookup falls back to "no parent, no children"), which lets a caller wire
// the handler before the topology is known.
func New(topo Topology) *Handler {
	return &Handler{topo: topo}
}

// WithTopology returns the handler after replacing its topology view. This
// is the write path the node registry uses when the cascade graph changes
// (a node joins or leaves); under a running system the registry serialises
// these calls, so a plain mutex swap is sufficient.
func (h *Handler) WithTopology(topo Topology) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.topo = topo
}

// topo returns the current topology view, never a nil Topology.
func (h *Handler) topoView() Topology {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.topo
}

// Forward implements port.CascadeHandler. It picks the next hop and the
// headers that must be attached; it never touches the network.
//
// Algorithm:
//  1. Parse the cascade headers the message already carries.
//  2. Loop detection: if the destination is already on RoutePath, return
//     an error naming the cycle — forwarding it again would livelock.
//  3. If the destination is a direct child, we are the next hop; append
//     ourselves to the route and add headers.
//  4. Otherwise, if we have a parent, route upstream: append ourselves
//     and let the parent pick up the message next.
//  5. Otherwise the caller sends to the original dstDeviceID unchanged.
func (h *Handler) Forward(fromNodeID model.NodeID, msg model.Message, dstDeviceID string) (string, []model.Header, bool, error) {
	dstDeviceID = strings.TrimSpace(dstDeviceID)
	if dstDeviceID == "" {
		return "", nil, false, fmt.Errorf("cascade: empty destination device id")
	}
	self := fromNodeID.String()

	route, err := model.RouteFromHeaders(msg.Headers())
	if err != nil {
		return "", nil, false, fmt.Errorf("cascade: parse headers on %s: %w", msg, err)
	}
	route.FromDeviceID = self

	if route.ContainsRoute(self) {
		return "", nil, false, fmt.Errorf(
			"cascade: loop detected: node %s is already on route %s",
			self, model.FormatRoutePath(route.RoutePath))
	}

	topo := h.topoView()
	var next string
	// Keep the route state after the PreferredPath entry has been consumed.
	routeAfterPop := route
	if topo != nil {
		// N-1 PreferredPath: honour the sender's preferred first hop when it
		// is a direct neighbour we know about (child or parent).
		popped, preferred := route.PopPreferred()
		if preferred != "" {
			for _, child := range topo.Children(self) {
				if child == preferred {
					next = preferred
					break
				}
			}
			if next == "" && topo.Parent(self) == preferred {
				next = preferred
			}
		}
		if next != "" {
			routeAfterPop = popped
		}
		// Child check (used when PreferredPath is absent or did not match).
		if next == "" {
			for _, child := range topo.Children(self) {
				if child == dstDeviceID {
					next = child
					break
				}
			}
		}
		if next == "" {
			if parent := topo.Parent(self); parent != "" {
				next = parent
			}
		}
	}

	if next == "" {
		// No cascade knowledge applies: send unchanged, no headers.
		return dstDeviceID, nil, false, nil
	}

	return next, routeAfterPop.AppendRoute(self).Headers(), false, nil
}

// String renders a log-safe one-line summary.
func (h *Handler) String() string {
	return "cascade.Handler"
}

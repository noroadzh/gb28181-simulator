// Node-configuration CRUD: the paths that write a node definition to the
// persistence store and the live registry together, in that order.
//
// Everything here treats the store as the source of truth (design D2):
// a definition that reached memory but not the store would vanish on the
// next boot, so every happy path ends with a store write, and every store
// failure rolls the memory side back before the error is returned. The
// rollback is best-effort and both failures are logged when it fails too —
// at that point memory holds a definition the store does not know about,
// which only a restart (memory rebuilds from the store) cleans up.
//
// Mutating paths refuse nodes that are not quiescent — idle, offline, or
// faulted. Editing a live node's address or cascade while its transports
// and heartbeats run against the old definition would leave the two sides
// disagreeing about where the node is; stop the node first.
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/platform/events"
)

// ErrNodeNotStoppable marks a create/update/delete refused because the
// node is not in a quiescent status. It is a sentinel so the HTTP layer
// can map it to 409 without string matching.
var ErrNodeNotStoppable = errors.New("app: node must be stopped before this operation")

// ErrCascadeCycle marks a cascade change that would close a loop — a node
// becoming its own parent, child, or an ancestor of itself.
var ErrCascadeCycle = errors.New("app: cascade change would close a loop")

// ErrCascadeSelfParent marks a cascade edit that would make a node its own
// parent. It is distinct from ErrCascadeCycle so the HTTP layer can answer
// the 400 the API contract names for it, rather than the 409 a loop earns.
var ErrCascadeSelfParent = errors.New("app: node cannot be its own parent")

// ErrCascadeInvalidID marks a cascade edit naming something that is not a
// GB/T 28181 node id. It is a caller mistake, not a server fault.
var ErrCascadeInvalidID = errors.New("app: cascade id is not a valid node id")

// ErrCascadeParentNotFound marks a cascade edit whose parent is not a node
// this process knows.
var ErrCascadeParentNotFound = errors.New("app: cascade parent not found")

// ErrCascadeChildNotFound marks a cascade edit naming a child this process
// does not know.
var ErrCascadeChildNotFound = errors.New("app: cascade child not found")

// nodeQuiescent reports whether a node sits in a status from which its
// definition may be edited or deleted: nothing of it is running, so there
// is no live state to disagree with the new definition.
func nodeQuiescent(st model.Status) bool {
	switch st {
	case model.StatusIdle, model.StatusOffline, model.StatusFault:
		return true
	}
	return false
}

// publishNodeEvent emits a definition event when an events hub is wired.
// Hub-less services (unit tests, the legacy seed path) stay silent.
func (s *NodeService) publishNodeEvent(typ, id string) {
	if s.events == nil {
		return
	}
	s.events.Publish(events.Event{Type: typ, ID: id, At: s.clock.Now()})
}

// GetStoredNode returns the persisted definition of one node; the second
// result is false when neither the store nor the registry knows the id.
// A service without a store (unit tests, the YAML seed path) has nothing
// persisted to return, so it falls back to the registry's live profile —
// same shape, same origin as ListStoredNodes.
func (s *NodeService) GetStoredNode(ctx context.Context, id model.NodeID) (model.StoredNode, bool) {
	if s.nodeStore != nil {
		sn, err := s.nodeStore.GetNode(ctx, id)
		if errors.Is(err, port.ErrNodeNotFound) {
			return model.StoredNode{}, false
		}
		if err != nil {
			// The NodeView contract has no error channel, so a storage
			// failure is reported the only way it can be — as absent —
			// but never silently: the log is what tells a 404 caused by
			// a database fault apart from a genuinely missing node.
			s.log.Error("app: read node from store", "id", id.String(), "err", err)
			return model.StoredNode{}, false
		}
		return sn, true
	}
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return model.StoredNode{}, false
	}
	return storedFromProfile(node.Profile()), true
}

// storedFromProfile shapes a live profile as the StoredNode the store
// would hold — the fallback view behind GetStoredNode/ListStoredNodes
// when no store is wired. CatalogPollIntervalMs is not recoverable here:
// the profile does not carry it (the acceptor reads it from its own
// option), so the fallback view reports it as zero.
func storedFromProfile(p model.NodeProfile) model.StoredNode {
	sn := model.StoredNode{
		ID:              p.ID(),
		Kind:            p.Kind(),
		Domain:          p.Domain(),
		Addr:            p.Addr(),
		Vendor:          p.Vendor(),
		CascadeParent:   p.CascadeParent(),
		CascadeChildren: slices.Clone(p.CascadeChildren()),
	}
	if reg, ok := p.Registration(); ok {
		sn.Registration = reg
		sn.HasRegistration = true
	}
	if serving, ok := p.PlatformServing(); ok {
		sn.Platform = serving
		sn.HasPlatform = true
	}
	return sn
}

// CreateNode registers sn in memory and persists it, in that order: a
// definition must be structurally valid before it earns a store row, and
// a store failure must not leave a half-created node behind, so the
// memory side is rolled back when the store refuses the row.
//
// The node comes back in StatusIdle, like Create. Starting it is the
// caller's next step.
func (s *NodeService) CreateNode(ctx context.Context, sn model.StoredNode) (model.Node, error) {
	prof, err := sn.Profile()
	if err != nil {
		return model.Node{}, fmt.Errorf("app: create node %s: %w", sn.ID, err)
	}
	node, err := s.registry.Register(ctx, prof)
	if err != nil {
		return model.Node{}, fmt.Errorf("app: create node %s: %w", sn.ID, err)
	}
	if s.nodeStore != nil {
		if err := s.nodeStore.CreateNode(ctx, sn); err != nil {
			if rb := s.registry.Unregister(ctx, node.ID()); rb != nil {
				s.log.Error("create node: persist failed and rollback failed too; restart will clean up",
					"node_id", node.ID().String(),
					"db_error", err.Error(), "rollback_error", rb.Error())
			}
			return model.Node{}, fmt.Errorf("app: create node %s: persist: %w", sn.ID, err)
		}
	}
	s.stamp(node.ID())
	s.publishNodeEvent(events.TypeNodeCreated, node.ID().String())
	return node, nil
}

// UpdateNode replaces a stored definition and the live profile behind it.
// The whole profile is swapped — address, cascade, registration, serving
// — because a partial edit could pair a new address with an old
// registration, a combination the user never asked for.
func (s *NodeService) UpdateNode(ctx context.Context, sn model.StoredNode) error {
	node, ok := s.registry.Get(ctx, sn.ID)
	if !ok {
		return fmt.Errorf("app: update node %s: %w", sn.ID, port.ErrNodeNotFound)
	}
	if !nodeQuiescent(node.Status()) {
		return fmt.Errorf("app: update node %s: %w (status %s)", sn.ID, ErrNodeNotStoppable, node.Status())
	}
	newProf, err := sn.Profile()
	if err != nil {
		return fmt.Errorf("app: update node %s: %w", sn.ID, err)
	}
	oldProf := node.Profile()
	if _, err := s.registry.MutateProfile(ctx, sn.ID, func(model.NodeProfile) (model.NodeProfile, error) {
		return newProf, nil
	}); err != nil {
		return fmt.Errorf("app: update node %s: %w", sn.ID, err)
	}
	if s.nodeStore != nil {
		if err := s.nodeStore.UpdateNode(ctx, sn.ID, sn); err != nil {
			if _, rb := s.registry.MutateProfile(ctx, sn.ID, func(model.NodeProfile) (model.NodeProfile, error) {
				return oldProf, nil
			}); rb != nil {
				s.log.Error("update node: persist failed and rollback failed too; restart will clean up",
					"node_id", sn.ID.String(),
					"db_error", err.Error(), "rollback_error", rb.Error())
			}
			return fmt.Errorf("app: update node %s: persist: %w", sn.ID, err)
		}
	}
	s.stamp(sn.ID)
	s.publishNodeEvent(events.TypeNodeUpdated, sn.ID.String())
	return nil
}

// DeleteNode removes a quiescent node from memory and store. The memory
// side goes first so a store failure can put the node back; the restored
// node comes back in StatusIdle, which is the closest memory can rebuild
// to the offline/fault state it was deleted in — the difference is logged
// when the rollback actually runs.
func (s *NodeService) DeleteNode(ctx context.Context, id model.NodeID) error {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return fmt.Errorf("app: delete node %s: %w", id, port.ErrNodeNotFound)
	}
	if !nodeQuiescent(node.Status()) {
		return fmt.Errorf("app: delete node %s: %w (status %s)", id, ErrNodeNotStoppable, node.Status())
	}
	if err := s.registry.Unregister(ctx, id); err != nil {
		return fmt.Errorf("app: delete node %s: %w", id, err)
	}
	if s.nodeStore != nil {
		if err := s.nodeStore.DeleteNode(ctx, id); err != nil {
			if _, rb := s.registry.Register(ctx, node.Profile()); rb != nil {
				s.log.Error("delete node: persist failed and rollback failed too; restart will clean up",
					"node_id", id.String(),
					"db_error", err.Error(), "rollback_error", rb.Error())
			}
			return fmt.Errorf("app: delete node %s: persist: %w", id, err)
		}
	}
	s.publishNodeEvent(events.TypeNodeDeleted, id.String())
	return nil
}

// peerEdit is one neighbour's children-list change an UpdateCascade
// implies: the new parent (when hosted locally) gains the node among its
// children, the old one loses it.
type peerEdit struct {
	id    model.NodeID
	after []string // post-edit children list of the neighbour
}

// UpdateCascade edits one node's cascade links: its parent, its children
// list, and the bookkeeping the change implies on its neighbours — the
// new parent (when hosted locally) gains the node among its children, the
// old one loses it. Neighbours this process does not host are skipped:
// cascades routinely point at platforms outside this process, and an
// absent profile is not an error, only nothing to edit.
//
// A change that would make the node its own parent or put one of its
// ancestors above it is refused before anything is written.
//
// Like the other paths, memory is edited first and the store second, with
// a full rollback when the store fails. Rollback restores each touched
// profile to the snapshot taken just before its mutation; when a concurrent
// edit wins the race between the snapshot and the mutation, the rollback
// is best-effort and logged, same as the create/update/delete paths.
func (s *NodeService) UpdateCascade(ctx context.Context, id model.NodeID, parent string, children []string) error {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return fmt.Errorf("app: update cascade %s: %w", id, port.ErrNodeNotFound)
	}
	if !nodeQuiescent(node.Status()) {
		return fmt.Errorf("app: update cascade %s: %w (status %s)", id, ErrNodeNotStoppable, node.Status())
	}

	parent = strings.TrimSpace(parent)
	children = normalizeCascadeChildren(children, id.String())
	if err := s.validateCascadeEdges(ctx, id, parent, children); err != nil {
		return err
	}
	if err := cascadeWouldCycle(ctx, s.registry, id.String(), parent); err != nil {
		return err
	}

	old := node.Profile()
	var peers []peerEdit
	if parent != old.CascadeParent() {
		// The old parent forgets the node; the new one learns it.
		if pid, perr := model.ParseNodeID(old.CascadeParent()); perr == nil {
			if pe, ok := s.editPeer(ctx, pid, id.String(), false); ok {
				peers = append(peers, pe)
			}
		}
		if pid, perr := model.ParseNodeID(parent); perr == nil {
			if pe, ok := s.editPeer(ctx, pid, id.String(), true); ok {
				peers = append(peers, pe)
			}
		}
	}

	// Ordered edit list: the node itself, then every neighbour. Each
	// entry carries the profile it should be restored to on rollback —
	// the snapshot taken just before its mutation.
	type edit struct {
		id     model.NodeID
		before model.NodeProfile
	}
	applied := make([]edit, 0, 1+len(peers))
	undo := func() {
		for _, e := range slices.Backward(applied) {
			if _, err := s.registry.MutateProfile(ctx, e.id, func(model.NodeProfile) (model.NodeProfile, error) {
				return e.before, nil
			}); err != nil {
				s.log.Error("cascade rollback step failed; restart will clean up",
					"node_id", e.id.String(), "error", err.Error())
			}
		}
	}

	do := func(target model.NodeID, np model.NodeProfile) error {
		cur, ok := s.registry.Get(ctx, target)
		if !ok {
			return fmt.Errorf("node %s disappeared mid-edit", target)
		}
		before := cur.Profile()
		if _, err := s.registry.MutateProfile(ctx, target, func(model.NodeProfile) (model.NodeProfile, error) {
			return np, nil
		}); err != nil {
			return err
		}
		applied = append(applied, edit{target, before})
		return nil
	}

	// WithCascadeParent refuses "" and WithCascadeChildren treats [] as a
	// no-op, so the clear cases go through the With*Cleared twins — that is
	// what makes "detach from cascade" expressible.
	selfAfter := old
	if parent == "" {
		selfAfter = selfAfter.WithCascadeParentCleared()
	} else {
		var perr error
		if selfAfter, perr = selfAfter.WithCascadeParent(parent); perr != nil {
			return fmt.Errorf("app: update cascade %s: %w", id, perr)
		}
	}
	if len(children) == 0 {
		selfAfter = selfAfter.WithCascadeChildrenCleared()
	} else {
		var cerr error
		if selfAfter, cerr = selfAfter.WithCascadeChildren(children); cerr != nil {
			return fmt.Errorf("app: update cascade %s: %w", id, cerr)
		}
	}
	if err := do(id, selfAfter); err != nil {
		undo()
		return fmt.Errorf("app: update cascade %s: %w", id, err)
	}
	for _, pe := range peers {
		np := mustPeerProfile(ctx, s.registry, pe.id)
		if len(pe.after) == 0 {
			np = np.WithCascadeChildrenCleared()
		} else {
			var nerr error
			if np, nerr = np.WithCascadeChildren(pe.after); nerr != nil {
				undo()
				return fmt.Errorf("app: update cascade %s: neighbour %s: %w", id, pe.id, nerr)
			}
		}
		if err := do(pe.id, np); err != nil {
			undo()
			return fmt.Errorf("app: update cascade %s: neighbour %s: %w", id, pe.id, err)
		}
	}

	if s.nodeStore != nil {
		if err := s.persistCascade(ctx, id, parent, children, peers); err != nil {
			undo()
			return fmt.Errorf("app: update cascade %s: persist: %w", id, err)
		}
	}
	s.stamp(id)
	s.publishNodeEvent(events.TypeNodeUpdated, id.String())
	return nil
}

// persistCascade writes the cascade edit to the store: the node's own row
// and, for each locally hosted neighbour, its children list. The peers'
// parent columns do not move — UpdateCascade edits ownership on the
// parent side only.
func (s *NodeService) persistCascade(ctx context.Context, id model.NodeID, parent string, children []string, peers []peerEdit) error {
	if err := s.nodeStore.UpdateCascade(ctx, id, parent, children); err != nil {
		return err
	}
	for _, pe := range peers {
		p, ok := s.registry.Get(ctx, pe.id)
		if !ok {
			continue
		}
		prof := p.Profile()
		if err := s.nodeStore.UpdateCascade(ctx, pe.id, prof.CascadeParent(), pe.after); err != nil {
			return err
		}
	}
	return nil
}

// editPeer snapshots a neighbour's children list and computes the list
// with the node added to or removed from it. It reports false when the
// neighbour is not hosted here or is the node itself.
func (s *NodeService) editPeer(ctx context.Context, peer model.NodeID, self string, add bool) (peerEdit, bool) {
	p, ok := s.registry.Get(ctx, peer)
	if !ok || peer.String() == self {
		return peerEdit{}, false
	}
	before := slices.Clone(p.Profile().CascadeChildren())
	after := make([]string, 0, len(before)+1)
	for _, c := range before {
		if c == self && !add {
			continue
		}
		after = append(after, c)
	}
	if add && !slices.Contains(after, self) {
		after = append(after, self)
	}
	return peerEdit{id: peer, after: after}, true
}

// mustPeerProfile reads a neighbour's current profile for an in-flight
// cascade edit. The neighbour was read moments ago, so the lookup cannot
// normally fail; a failure means a concurrent delete won the race, and the
// edit list is stale — the caller's rollback has already run.
func mustPeerProfile(ctx context.Context, r port.NodeRegistry, id model.NodeID) model.NodeProfile {
	p, ok := r.Get(ctx, id)
	if !ok {
		return model.NodeProfile{}
	}
	return p.Profile()
}

// cascadeWouldCycle walks the parent chain that would exist after the
// edit: starting from the new parent and following parents, arriving back
// at the node means the edit closes a loop. Chains of nodes this process
// does not host end the walk — their shape is unknowable here, and the
// runtime loop detection (482 Loop Detected) is the safety net for the
// configurations that only a running topology can reveal.
func cascadeWouldCycle(ctx context.Context, r port.NodeRegistry, self, parent string) error {
	if parent == "" {
		return nil
	}
	if parent == self {
		return fmt.Errorf("app: cascade: node %s cannot be its own parent: %w", self, ErrCascadeCycle)
	}
	seen := map[string]bool{self: true}
	cur := parent
	for cur != "" {
		if seen[cur] {
			if cur == self {
				return fmt.Errorf("app: cascade: parent chain of %s revisits it: %w", self, ErrCascadeCycle)
			}
			return nil
		}
		seen[cur] = true
		pid, err := model.ParseNodeID(cur)
		if err != nil {
			return nil
		}
		n, ok := r.Get(ctx, pid)
		if !ok {
			return nil
		}
		cur = n.Profile().CascadeParent()
	}
	return nil
}

// validateCascadeEdges rejects a cascade edit that names an id which is not
// a node id at all, or one this process does not host. The graph is
// in-process (the cascade handler resolves hops from the registry), so an
// edge to an unknown id would be stored and then route nowhere — better to
// refuse it at the boundary than to accept a definition that cannot work.
func (s *NodeService) validateCascadeEdges(ctx context.Context, id model.NodeID, parent string, children []string) error {
	if parent != "" {
		if parent == id.String() {
			return fmt.Errorf("app: update cascade %s: %w", id, ErrCascadeSelfParent)
		}
		pid, err := model.ParseNodeID(parent)
		if err != nil {
			return fmt.Errorf("app: update cascade %s: parent %q: %w: %w", id, parent, ErrCascadeInvalidID, err)
		}
		if _, ok := s.registry.Get(ctx, pid); !ok {
			return fmt.Errorf("app: update cascade %s: parent %s: %w", id, pid, ErrCascadeParentNotFound)
		}
	}
	for _, child := range children {
		cid, err := model.ParseNodeID(child)
		if err != nil {
			return fmt.Errorf("app: update cascade %s: child %q: %w: %w", id, child, ErrCascadeInvalidID, err)
		}
		if _, ok := s.registry.Get(ctx, cid); !ok {
			return fmt.Errorf("app: update cascade %s: child %s: %w", id, cid, ErrCascadeChildNotFound)
		}
	}
	return nil
}

// normalizeCascadeChildren trims, de-duplicates, drops empties and the
// node's own id, and keeps the caller's order — the order is the routing
// order the cascade handler relies on.
func normalizeCascadeChildren(children []string, self string) []string {
	out := make([]string, 0, len(children))
	for _, c := range children {
		c = strings.TrimSpace(c)
		if c == "" || c == self || slices.Contains(out, c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// LoadFromStore rebuilds the in-memory node set from the persisted
// definitions. It is the boot path behind "the store is the source of
// truth": a process that restarts comes back with exactly the nodes the
// store holds, no YAML involved.
//
// One rotten row must not take down the boot, so per-node failures are
// logged with their node id and skipped — the row stays in the store for
// inspection, and the rest of the fleet comes up. Nodes already live (a
// Create that raced the boot) are left alone.
//
// Definitions are registered in parallel, bounded, because a large fleet
// registers faster than one at a time and the registry locks per call
// anyway; ordering does not matter since cascade links are resolved at
// forwarding time, not at registration time.
func (s *NodeService) LoadFromStore(ctx context.Context) error {
	if s.nodeStore == nil {
		return nil
	}
	nodes, err := s.nodeStore.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("app: load nodes from store: %w", err)
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	for _, sn := range nodes {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if _, ok := s.registry.Get(ctx, sn.ID); ok {
				return
			}
			prof, err := sn.Profile()
			if err != nil {
				s.log.Warn("skip persisted node that cannot be rebuilt",
					"node_id", sn.ID.String(), "error", err.Error())
				return
			}
			if _, err := s.registry.Register(ctx, prof); err != nil {
				s.log.Warn("skip persisted node that cannot register",
					"node_id", sn.ID.String(), "error", err.Error())
				return
			}
			if err := s.RestorePersistedState(ctx, sn.ID); err != nil {
				s.log.Warn("node rebuilt but persisted state reload failed",
					"node_id", sn.ID.String(), "error", err.Error())
			}
		})
	}
	wg.Wait()
	return nil
}

// Package app — NodeService.
package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// TransportFactory binds a signalling listener on addr and returns the
// transport. The app layer decides how listeners are built; this package
// never imports an adapter, so the concrete construction stays in the
// composition root (cmd).
type TransportFactory func(addr string) (port.SIPTransport, error)

// NodeService orchestrates the node use cases. It depends only on domain
// ports — never on internal/adapter/... (design D6).
//
// Note on AuditSink: design D6 lists it among the dependencies, but the
// domain's only audit event, model.WireEvent, describes wire-level
// transmission (its Direction is transmit/receive and Transport is
// mandatory), so it cannot carry a node lifecycle change. Design Q2
// confirms node lifecycle events are out of scope for this change and are
// deferred to Change 13, so AuditSink is deliberately not injected yet.
type NodeService struct {
	registry  port.NodeRegistry
	lifecycle port.NodeLifecycle
	advancer  port.NodeAdvancer
	factory   TransportFactory
	clock     port.Clock
	registrar *Registrar

	mu        sync.Mutex
	changedAt map[string]time.Time
}

// NewNodeService builds a NodeService over domain ports. Every argument is
// required except clock, which defaults to a real-time clock when nil.
func NewNodeService(
	registry port.NodeRegistry,
	lifecycle port.NodeLifecycle,
	advancer port.NodeAdvancer,
	factory TransportFactory,
	clock port.Clock,
) (*NodeService, error) {
	if registry == nil {
		return nil, fmt.Errorf("app: NodeService requires a NodeRegistry")
	}
	if lifecycle == nil {
		return nil, fmt.Errorf("app: NodeService requires a NodeLifecycle")
	}
	if advancer == nil {
		return nil, fmt.Errorf("app: NodeService requires a NodeAdvancer")
	}
	if factory == nil {
		return nil, fmt.Errorf("app: NodeService requires a TransportFactory")
	}
	if clock == nil {
		clock = realClock{}
	}
	return &NodeService{
		registry:  registry,
		lifecycle: lifecycle,
		advancer:  advancer,
		factory:   factory,
		clock:     clock,
		changedAt: make(map[string]time.Time),
	}, nil
}

// WithRegistrar attaches the device registration use case to an existing
// service. It is separate from NewNodeService so the constructor keeps its
// shape for callers that do not register nodes; a node that wants to
// register without one fails its start instead of silently staying
// unregistered.
func (s *NodeService) WithRegistrar(r *Registrar) (*NodeService, error) {
	if r == nil {
		return nil, fmt.Errorf("app: NodeService requires a non-nil Registrar")
	}
	s.registrar = r
	return s, nil
}

// Create registers a node from profile and returns it in StatusIdle.
func (s *NodeService) Create(ctx context.Context, profile model.NodeProfile) (model.Node, error) {
	node, err := s.registry.Register(ctx, profile)
	if err != nil {
		return model.Node{}, fmt.Errorf("app: create node: %w", err)
	}
	s.stamp(node.ID())
	return node, nil
}

// Start binds the node's listener and advances it to StatusRegistering.
// A device node that carries a registration then completes its
// registration before Start returns: success leaves it StatusOnline, a
// failure StatusFault with the listener released (design D1). A node
// without a registration, or one that is not a device, stays at
// StatusRegistering exactly as before — the other identities arrive in
// later changes.
func (s *NodeService) Start(ctx context.Context, id model.NodeID) error {
	if err := s.lifecycle.Start(ctx, id); err != nil {
		return fmt.Errorf("app: start node %s: %w", id, err)
	}
	s.stamp(id)

	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return fmt.Errorf("app: start node %s: node disappeared after binding", id)
	}
	reg, wants := node.Registration()
	if !wants || node.ID().Kind() != model.NodeKindDevice {
		return nil
	}
	return s.register(ctx, id, node, reg)
}

// register runs one registration transaction: it records what the platform
// granted and moves the node registered → online, or faults it — releasing
// the port — and returns the cause.
func (s *NodeService) register(
	ctx context.Context,
	id model.NodeID,
	node model.Node,
	reg model.Registration,
) error {
	if s.registrar == nil {
		return s.fault(ctx, id, fmt.Errorf(
			"app: node %s is configured to register but the service has no registrar", id))
	}
	tr := s.lifecycle.Transport(id)
	if tr == nil {
		return s.fault(ctx, id, fmt.Errorf("app: no listener bound for node %s", id))
	}
	result, err := s.registrar.Register(ctx, tr, node, reg)
	if err != nil {
		return s.fault(ctx, id, fmt.Errorf("app: register node %s: %w", id, err))
	}
	if _, err := s.registry.RecordRegistration(ctx, id, result); err != nil {
		return s.fault(ctx, id, fmt.Errorf("app: record registration of node %s: %w", id, err))
	}
	// Only now does the node count as registered, and then online.
	if err := s.MarkRegistered(ctx, id); err != nil {
		return s.fault(ctx, id, err)
	}
	if err := s.MarkOnline(ctx, id); err != nil {
		return s.fault(ctx, id, err)
	}
	s.stamp(id)
	return nil
}

// fault moves the node to StatusFault — releasing its listener — and
// returns cause. If faulting itself fails (the node was stopped in the
// meantime) both failures are reported, so neither is swallowed.
func (s *NodeService) fault(ctx context.Context, id model.NodeID, cause error) error {
	s.stamp(id)
	if err := s.lifecycle.Fail(ctx, id, cause); err != nil {
		return fmt.Errorf("%v (and faulting node %s failed: %w)", cause, id, err)
	}
	return cause
}

// Stop releases the node's listener and advances it to StatusOffline.
func (s *NodeService) Stop(ctx context.Context, id model.NodeID) error {
	if err := s.lifecycle.Stop(ctx, id); err != nil {
		return fmt.Errorf("app: stop node %s: %w", id, err)
	}
	s.stamp(id)
	return nil
}

// MarkRegistered advances a node to StatusRegistered. Start calls it once a
// registration completes; it stays exported for the other identities and
// for tests.
func (s *NodeService) MarkRegistered(ctx context.Context, id model.NodeID) error {
	return s.advance(ctx, id, model.StatusRegistered)
}

// MarkOnline advances a node to StatusOnline. It exists for the identity
// implementations and tests; Start never calls it.
func (s *NodeService) MarkOnline(ctx context.Context, id model.NodeID) error {
	return s.advance(ctx, id, model.StatusOnline)
}

func (s *NodeService) advance(ctx context.Context, id model.NodeID, to model.Status) error {
	if _, err := s.advancer.Advance(ctx, id, to); err != nil {
		return fmt.Errorf("app: advance node %s to %s: %w", id, to, err)
	}
	s.stamp(id)
	return nil
}

// List returns every registered node.
func (s *NodeService) List(ctx context.Context) []model.Node {
	return s.registry.List(ctx)
}

// Get looks a node up by id.
func (s *NodeService) Get(ctx context.Context, id model.NodeID) (model.Node, bool) {
	return s.registry.Get(ctx, id)
}

// Status returns the node's current status.
func (s *NodeService) Status(ctx context.Context, id model.NodeID) (model.Status, error) {
	st, err := s.lifecycle.Status(ctx, id)
	if err != nil {
		return model.StatusIdle, fmt.Errorf("app: status of node %s: %w", id, err)
	}
	return st, nil
}

// ChangedAt reports when this service last changed the node's status. It is
// driven by the injected Clock so tests can assert timestamps without
// sleeping.
func (s *NodeService) ChangedAt(id model.NodeID) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.changedAt[id.String()]
	return at, ok
}

// TransportFactory exposes the factory so the composition root can reuse the
// same one it injected, keeping a single source of truth for how listeners
// are built.
func (s *NodeService) TransportFactory() TransportFactory { return s.factory }

func (s *NodeService) stamp(id model.NodeID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changedAt[id.String()] = s.clock.Now()
}

// realClock is the default Clock; it reads the system time. It keeps this
// package free of any platform dependency.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

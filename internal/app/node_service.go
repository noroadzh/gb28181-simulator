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
	keeper    *Keeper
	acceptor  *Acceptor

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

// WithKeeper attaches the keepalive and renewal use case. Without one a
// registered node still comes online — its registration simply is not held
// open, which is what tests and the other identities want.
func (s *NodeService) WithKeeper(k *Keeper) (*NodeService, error) {
	if k == nil {
		return nil, fmt.Errorf("app: NodeService requires a non-nil Keeper")
	}
	s.keeper = k
	return s, nil
}

// WithAcceptor attaches the platform-large use case: accepting downstream
// registrations. Without one a platform node still starts — it simply does
// not serve, which is what the other identities and the tests want.
func (s *NodeService) WithAcceptor(a *Acceptor) (*NodeService, error) {
	if a == nil {
		return nil, fmt.Errorf("app: NodeService requires a non-nil Acceptor")
	}
	s.acceptor = a
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
	if wants && node.ID().Kind() == model.NodeKindDevice {
		return s.register(ctx, id, node, reg)
	}
	// A platform does not register with anyone: it accepts registrations
	// instead, which is its own way of coming online.
	if node.ID().Kind() == model.NodeKindPlatformLarge {
		return s.serve(ctx, id, node)
	}
	return nil
}

// serve starts accepting downstream registrations on a platform-large node
// and brings it online — "online" meaning the platform is serving. A node
// that cannot serve is faulted, so a half-started platform never keeps its
// port.
func (s *NodeService) serve(ctx context.Context, id model.NodeID, node model.Node) error {
	if s.acceptor == nil {
		// No acceptor means no serving: the node stays where the
		// lifecycle left it rather than pretending to be online.
		return nil
	}
	tr := s.lifecycle.Transport(id)
	if tr == nil {
		return s.fault(ctx, id, fmt.Errorf("app: no listener bound for node %s", id))
	}
	serving, ok := node.PlatformServing()
	if !ok {
		// An undeclared `platform:` section is not a reason to refuse:
		// the node serves in its own domain with the default window.
		var err error
		if serving, err = model.DefaultPlatformServing(node.Profile().Domain()); err != nil {
			return s.fault(ctx, id, fmt.Errorf("app: serving defaults for node %s: %w", id, err))
		}
	}
	if err := s.acceptor.Serve(id, tr, serving.Realm(), serving.Policy()); err != nil {
		return s.fault(ctx, id, fmt.Errorf("app: start serving on node %s: %w", id, err))
	}
	// The state machine has no `registering → online` edge: a platform is
	// first "registered" (bound and ready), then serving.
	if err := s.MarkRegistered(ctx, id); err != nil {
		s.stopServing(id)
		return s.fault(ctx, id, err)
	}
	if err := s.MarkOnline(ctx, id); err != nil {
		s.stopServing(id)
		return s.fault(ctx, id, err)
	}
	s.stamp(id)
	return nil
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
	// From here on the node is held open: heartbeats go out and the
	// registration is renewed before it lapses. The keeper runs on its own
	// context, so it outlives this call.
	if s.keeper != nil {
		if err := s.keeper.Start(id, tr, reg, result); err != nil {
			return s.fault(ctx, id, fmt.Errorf("app: start keepalive for node %s: %w", id, err))
		}
	}
	s.stamp(id)
	return nil
}

// Unregister asks the platform to forget the node: it sends a REGISTER with
// `Expires: 0` and, only once the platform agrees, stops the node's
// background work and releases its listener (`online → offline`).
//
// A failed unregistration leaves the node online — its registration is still
// valid — and reports the stage it failed at, so the caller can retry.
func (s *NodeService) Unregister(ctx context.Context, id model.NodeID) error {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return fmt.Errorf("app: unregister node %s: unknown node", id)
	}
	switch node.Status() {
	case model.StatusOnline, model.StatusRegistered:
	default:
		return fmt.Errorf("app: unregister node %s: %w (status %s)",
			id, model.ErrIllegalTransition, node.Status())
	}
	reg, wants := node.Registration()
	if !wants {
		return fmt.Errorf("app: unregister node %s: %w", id, ErrNotRegistered)
	}
	if s.registrar == nil {
		return fmt.Errorf("app: unregister node %s: service has no registrar", id)
	}
	tr := s.lifecycle.Transport(id)
	if tr == nil {
		return fmt.Errorf("app: unregister node %s: no listener bound", id)
	}
	if _, err := s.registrar.Unregister(ctx, tr, node, reg); err != nil {
		// The registration still stands: report and let the caller
		// decide, rather than dropping a live node into fault.
		return fmt.Errorf("app: unregister node %s: %w", id, err)
	}
	s.stopKeeping(id)
	if err := s.lifecycle.Stop(ctx, id); err != nil {
		return fmt.Errorf("app: unregister node %s: %w", id, err)
	}
	s.stamp(id)
	return nil
}

// stopKeeping ends a node's background work before its listener is
// released, so no heartbeat is aimed at a closed transport.
func (s *NodeService) stopKeeping(id model.NodeID) {
	if s.keeper != nil {
		s.keeper.Stop(id)
	}
	s.stopServing(id)
}

// stopServing ends a platform node's serving goroutine and forgets the
// devices it accepted. It runs before the listener is released, so no
// answer is ever aimed at a closed transport.
func (s *NodeService) stopServing(id model.NodeID) {
	if s.acceptor != nil {
		s.acceptor.Stop(id)
	}
}

// fault moves the node to StatusFault — releasing its listener — and
// returns cause. If faulting itself fails (the node was stopped in the
// meantime) both failures are reported, so neither is swallowed.
func (s *NodeService) fault(ctx context.Context, id model.NodeID, cause error) error {
	s.stamp(id)
	s.stopKeeping(id)
	if err := s.lifecycle.Fail(ctx, id, cause); err != nil {
		return fmt.Errorf("%v (and faulting node %s failed: %w)", cause, id, err)
	}
	return cause
}

// Stop releases the node's listener and advances it to StatusOffline.
// Background work is stopped first, so the last heartbeat cannot race the
// release.
func (s *NodeService) Stop(ctx context.Context, id model.NodeID) error {
	s.stopKeeping(id)
	if err := s.lifecycle.Stop(ctx, id); err != nil {
		return fmt.Errorf("app: stop node %s: %w", id, err)
	}
	s.stamp(id)
	return nil
}

// Devices returns the online device table of a platform-large node,
// ordered by device id. It is a read-only view: an unknown node is an
// error, an unknown platform simply has no devices.
func (s *NodeService) Devices(ctx context.Context, id model.NodeID) ([]model.DownstreamDevice, error) {
	if _, ok := s.registry.Get(ctx, id); !ok {
		return nil, fmt.Errorf("app: devices of node %s: %w", id, model.ErrUnknownNode)
	}
	if s.acceptor == nil {
		return nil, nil
	}
	return s.acceptor.Devices(ctx, id), nil
}

// Device returns one row of a platform-large node's online device table.
func (s *NodeService) Device(ctx context.Context, id model.NodeID, deviceID string) (model.DownstreamDevice, error) {
	if _, ok := s.registry.Get(ctx, id); !ok {
		return model.DownstreamDevice{}, fmt.Errorf("app: device %s of node %s: %w", deviceID, id, model.ErrUnknownNode)
	}
	if s.acceptor == nil {
		return model.DownstreamDevice{}, model.ErrUnknownDevice
	}
	dev, ok := s.acceptor.Device(ctx, id, deviceID)
	if !ok {
		return model.DownstreamDevice{}, model.ErrUnknownDevice
	}
	return dev, nil
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

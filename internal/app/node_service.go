// Package app — NodeService.
package app

import (
	"context"
	"fmt"
	"log/slog"
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
	log       *slog.Logger
	registrar *Registrar
	keeper    *Keeper
	acceptor  *Acceptor

	// splits holds the message splitter of every node that is both halves
	// of a cascade. A platform-small serves and registers over one
	// listener, so something has to sort what arrives; a node that has no
	// splitter reads its own socket directly.
	splits *splitters

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
		log:       slog.Default(),
		splits:    newSplitters(),
		changedAt: make(map[string]time.Time),
	}, nil
}

// WithLogger replaces the logger the service reports through: which half of
// a two-halved node failed, and what could not be undone on the way out. It
// defaults to slog.Default(), so the composition root is the only caller
// that needs it.
func (s *NodeService) WithLogger(l *slog.Logger) (*NodeService, error) {
	if l == nil {
		return nil, fmt.Errorf("app: NodeService requires a non-nil logger")
	}
	s.log = l
	return s, nil
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

// Start binds the node's listener and advances it to StatusRegistering,
// then lets the node's identity decide what else it must do to come
// online:
//
//   - a device with a registration completes it — success leaves the node
//     StatusOnline, a failure StatusFault with the listener released
//     (design D1);
//   - a platform-large accepts registrations, which is its own way of
//     coming online;
//   - a platform-small does both: it serves first, then registers with its
//     upstream when one is declared. Either half failing faults the node
//     and unwinds the other.
//
// A node with nothing to do for its identity stays at StatusRegistering.
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
	switch node.ID().Kind() {
	case model.NodeKindDevice:
		if wants {
			return s.register(ctx, id, node, reg, s.lifecycle.Transport(id))
		}
		return nil
	case model.NodeKindPlatformLarge:
		// A platform-large does not register with anyone: it accepts
		// registrations instead.
		return s.serve(ctx, id, node, s.lifecycle.Transport(id))
	case model.NodeKindPlatformSmall:
		return s.startPlatformSmall(ctx, id, node, reg, wants, s.lifecycle.Transport(id))
	}
	return nil
}

// startPlatformSmall establishes both halves of a platform-small node —
// the link in the middle of a cascade, which is a UAS to its downstreams
// and a UAC to its upstream.
//
// Serving comes first because it is the half that hardly ever fails; the
// registration is the half that meets an unreachable or refusing peer, so
// putting it second makes its failure path "unwind the serving" and nothing
// more. A failed half faults the node and the other half is unwound: one
// node has one state, so a half-started platform is not a state we can
// report. The error names the half that failed.
//
// Both halves run over the node's one listener. They are given different
// views of it — the serving half reads what arrives unasked, the upstream
// half reads the answers to what it sent — because the socket gives each
// datagram to whichever half reads first, and the serving half, which reads
// all the time, would otherwise swallow the answers and the registration
// would never finish.
func (s *NodeService) startPlatformSmall(
	ctx context.Context,
	id model.NodeID,
	node model.Node,
	reg model.Registration,
	wants bool,
	tr port.SIPTransport,
) error {
	if tr == nil {
		return s.fault(ctx, id, fmt.Errorf("app: no listener bound for node %s", id))
	}
	split := s.splits.forNode(id, tr)
	if err := s.serve(ctx, id, node, split.Serving()); err != nil {
		s.log.Error("platform-small cannot serve its downstreams",
			"node_id", id.String(), "error", err.Error())
		return fmt.Errorf("app: platform-small node %s, serving half: %w", id, err)
	}
	if !wants {
		// No upstream declared: the node is a platform to its own
		// downstreams and registers with nobody. Serving alone is a
		// complete start.
		return nil
	}
	if err := s.register(ctx, id, node, reg, split.Upstream()); err != nil {
		s.log.Error("platform-small cannot register with its upstream, unwinding the serving half",
			"node_id", id.String(), "error", err.Error())
		// register faults the node, which ends the serving too, but
		// the serving half owns a goroutine and a device table of its
		// own: end it here as well, so the reason a faulted node is
		// not serving does not depend on how fault unwinds.
		s.stopServing(id)
		return fmt.Errorf("app: platform-small node %s, upstream half: %w", id, err)
	}
	return nil
}

// serve starts accepting downstream registrations on a platform node and
// brings it online — "online" meaning the platform is serving. A node
// that cannot serve is faulted, so a half-started platform never keeps its
// port.
func (s *NodeService) serve(
	ctx context.Context,
	id model.NodeID,
	node model.Node,
	tr port.SIPTransport,
) error {
	if s.acceptor == nil {
		// No acceptor means no serving: the node stays where the
		// lifecycle left it rather than pretending to be online.
		return nil
	}
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
	if err := s.advanceOnline(ctx, id); err != nil {
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
	tr port.SIPTransport,
) error {
	if s.registrar == nil {
		return s.fault(ctx, id, fmt.Errorf(
			"app: node %s is configured to register but the service has no registrar", id))
	}
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
	// Only now does the node count as registered, and then online — unless
	// another half of this node already brought it there.
	if err := s.advanceOnline(ctx, id); err != nil {
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
	tr := s.upstreamSocket(id)
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
// released, so no heartbeat is aimed at a closed transport. A node that
// sorted its socket between two halves stops sorting it too: nothing is left
// reading a listener that is about to be given back.
func (s *NodeService) stopKeeping(id model.NodeID) {
	if s.keeper != nil {
		s.keeper.Stop(id)
	}
	s.stopServing(id)
	s.splits.end(id)
}

// upstreamSocket is the socket the node speaks to its upstream through: the
// upstream half's view of a shared listener when the node has one, the
// listener itself otherwise. Reading the socket directly while a splitter is
// reading it would race the splitter for the answers.
func (s *NodeService) upstreamSocket(id model.NodeID) port.SIPTransport {
	tr := s.lifecycle.Transport(id)
	if tr == nil {
		return nil
	}
	if split, ok := s.splits.lookup(id); ok {
		return split.Upstream()
	}
	return tr
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
//
// A platform-small says goodbye to its upstream before that: it is the one
// identity that both serves and is registered somewhere, so stopping it has
// to end both halves. A goodbye that does not get through does not keep the
// node running — it is logged, and the stop goes on.
func (s *NodeService) Stop(ctx context.Context, id model.NodeID) error {
	s.unregisterUpstream(ctx, id)
	s.stopKeeping(id)
	if err := s.lifecycle.Stop(ctx, id); err != nil {
		return fmt.Errorf("app: stop node %s: %w", id, err)
	}
	s.stamp(id)
	return nil
}

// unregisterUpstream sends the upstream platform a REGISTER with
// `Expires: 0` when the node has an upstream to say goodbye to, and does
// nothing otherwise: a device is unregistered through Unregister, which
// reports failure to its caller, and a platform-large has no upstream.
//
// Nothing here can fail the stop. The node is going down either way; what
// is left behind is a platform that still thinks the node is registered
// until its own lifetime lapses, which is why it is worth a warning.
func (s *NodeService) unregisterUpstream(ctx context.Context, id model.NodeID) {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return
	}
	if node.ID().Kind() != model.NodeKindPlatformSmall {
		return
	}
	reg, wants := node.Registration()
	if !wants {
		return
	}
	switch node.Status() {
	case model.StatusOnline, model.StatusRegistered:
	default:
		// A node that never got registered has nothing to withdraw.
		return
	}
	if s.registrar == nil {
		return
	}
	tr := s.upstreamSocket(id)
	if tr == nil {
		return
	}
	if _, err := s.registrar.Unregister(ctx, tr, node, reg); err != nil {
		s.log.Warn("platform-small stopped without saying goodbye to its upstream",
			"node_id", id.String(), "error", err.Error())
	}
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

// MarkOnline advances a node to StatusOnline. The identity implementations
// reach it through advanceOnline, which skips it when the node is already
// there; it stays exported for tests.
func (s *NodeService) MarkOnline(ctx context.Context, id model.NodeID) error {
	return s.advance(ctx, id, model.StatusOnline)
}

// advanceOnline brings a node online once, whichever half of it got there
// first. The state machine has no `online → online` edge and no
// `online → registered` one either, so a second half that advances the same
// node would fail on a transition that has already been made: serving
// brings a platform-small online, and the upstream registration that
// follows must be able to record its result without asking for that edge
// again. Advancing is therefore idempotent — whatever is still missing is
// done, whatever is already true is skipped — and the conversion table
// itself is left alone.
func (s *NodeService) advanceOnline(ctx context.Context, id model.NodeID) error {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return fmt.Errorf("app: advance node %s to %s: %w", id, model.StatusOnline, model.ErrUnknownNode)
	}
	switch node.Status() {
	case model.StatusOnline:
		return nil
	case model.StatusRegistered:
		return s.MarkOnline(ctx, id)
	default:
		// The state machine has no `registering → online` edge: a
		// node is first "registered" (bound and ready), then online.
		if err := s.MarkRegistered(ctx, id); err != nil {
			return err
		}
		return s.MarkOnline(ctx, id)
	}
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

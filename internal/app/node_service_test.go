package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// The fakes below are hand-rolled rather than generated: they double as
// documentation of how the ports are expected to be used, and they keep this
// test free of any adapter import (task 6.5).

type fakeCatalogue struct {
	mu      sync.Mutex
	nodes   map[string]model.Node
	addrIdx map[string]string
}

func newFakeCatalogue() *fakeCatalogue {
	return &fakeCatalogue{nodes: map[string]model.Node{}, addrIdx: map[string]string{}}
}

func (c *fakeCatalogue) Register(_ context.Context, p model.NodeProfile) (model.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := p.ID().String()
	if _, ok := c.nodes[id]; ok {
		return model.Node{}, fmt.Errorf("node %s already registered", id)
	}
	if other, ok := c.addrIdx[p.Addr()]; ok {
		return model.Node{}, fmt.Errorf("address %s claimed by %s", p.Addr(), other)
	}
	n := model.NewNode(p)
	c.nodes[id] = n
	c.addrIdx[p.Addr()] = id
	return n, nil
}

func (c *fakeCatalogue) Unregister(_ context.Context, id model.NodeID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	delete(c.nodes, id.String())
	delete(c.addrIdx, n.Profile().Addr())
	return nil
}

func (c *fakeCatalogue) Get(_ context.Context, id model.NodeID) (model.Node, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[id.String()]
	return n, ok
}

func (c *fakeCatalogue) List(_ context.Context) []model.Node {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.Node, 0, len(c.nodes))
	for _, n := range c.nodes {
		out = append(out, n)
	}
	return out
}

func (c *fakeCatalogue) MutateProfile(_ context.Context, id model.NodeID,
	fn func(model.NodeProfile) (model.NodeProfile, error)) (model.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[id.String()]
	if !ok {
		return model.Node{}, fmt.Errorf("unknown node %s", id)
	}
	p, err := fn(n.Profile())
	if err != nil {
		return model.Node{}, err
	}
	next := n.WithProfile(p)
	c.nodes[id.String()] = next
	return next, nil
}

func (c *fakeCatalogue) Advance(_ context.Context, id model.NodeID, to model.Status) (model.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[id.String()]
	if !ok {
		return model.Node{}, fmt.Errorf("unknown node %s", id)
	}
	next, err := n.WithStatus(to)
	if err != nil {
		return n, err
	}
	c.nodes[id.String()] = next
	return next, nil
}

// RecordRegistration stores a registration outcome on the node, exactly
// like the real registry: the status is untouched.
func (c *fakeCatalogue) RecordRegistration(
	_ context.Context, id model.NodeID, result model.RegistrationResult,
) (model.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[id.String()]
	if !ok {
		return model.Node{}, fmt.Errorf("unknown node %s", id)
	}
	next, err := n.WithRegistrationResult(result)
	if err != nil {
		return n, err
	}
	c.nodes[id.String()] = next
	return next, nil
}

// fakeLifecycle models listener binding. It reads node existence from the
// catalogue, exactly like the real lifecycle reads the registry. fail makes
// the bind of an id fail, which is how a port collision is simulated.
type fakeLifecycle struct {
	cat    *fakeCatalogue
	mu     sync.Mutex
	status map[string]model.Status
	bound  map[string]bool
	fail   map[string]bool
	starts int

	// transport is handed to whoever asks for the node's listener; nil
	// means "nothing bound". faults counts Fail calls and lastFault keeps
	// the reason, so tests can assert a failed start was faulted.
	transport port.SIPTransport
	faults    int
	lastFault error
}

func newFakeLifecycle(cat *fakeCatalogue) *fakeLifecycle {
	return &fakeLifecycle{
		cat:    cat,
		status: map[string]model.Status{},
		bound:  map[string]bool{},
		fail:   map[string]bool{},
	}
}

// Start mirrors the real lifecycle: it advances the node through the
// catalogue (the registry is the single source of truth for status) and only
// then records the listener as bound.
func (l *fakeLifecycle) Start(ctx context.Context, id model.NodeID) error {
	if _, ok := l.cat.Get(ctx, id); !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	key := id.String()
	l.mu.Lock()
	if l.fail[key] {
		l.mu.Unlock()
		return fmt.Errorf("bind %s: address already in use", key)
	}
	if l.bound[key] {
		l.mu.Unlock()
		return fmt.Errorf("node %s already bound", key)
	}
	l.mu.Unlock()

	// Mirrors the real lifecycle: a faulted node is reset before starting.
	if node, ok := l.cat.Get(ctx, id); ok && node.Status() == model.StatusFault {
		if _, err := l.cat.Advance(ctx, id, model.StatusIdle); err != nil {
			return err
		}
	}
	if _, err := l.cat.Advance(ctx, id, model.StatusRegistering); err != nil {
		return err
	}
	l.mu.Lock()
	l.bound[key] = true
	l.status[key] = model.StatusRegistering
	l.starts++
	l.mu.Unlock()
	return nil
}

func (l *fakeLifecycle) Stop(ctx context.Context, id model.NodeID) error {
	if _, ok := l.cat.Get(ctx, id); !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	if _, err := l.cat.Advance(ctx, id, model.StatusOffline); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.bound, id.String())
	l.status[id.String()] = model.StatusOffline
	return nil
}

func (l *fakeLifecycle) Status(ctx context.Context, id model.NodeID) (model.Status, error) {
	node, ok := l.cat.Get(ctx, id)
	if !ok {
		return model.StatusIdle, fmt.Errorf("unknown node %s", id)
	}
	return node.Status(), nil
}

// Transport returns the listener of a bound node, mirroring the real
// lifecycle: an unbound node has none.
func (l *fakeLifecycle) Transport(id model.NodeID) port.SIPTransport {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.bound[id.String()] {
		return nil
	}
	return l.transport
}

// Fail advances the node to fault and releases its listener, recording why
// so a test can assert the cause reached the lifecycle.
func (l *fakeLifecycle) Fail(ctx context.Context, id model.NodeID, reason error) error {
	if _, ok := l.cat.Get(ctx, id); !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	if _, err := l.cat.Advance(ctx, id, model.StatusFault); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.bound, id.String())
	l.status[id.String()] = model.StatusFault
	l.faults++
	l.lastFault = reason
	return nil
}

func (l *fakeLifecycle) isBound(id model.NodeID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bound[id.String()]
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func testProfile(t *testing.T, id, addr string) model.NodeProfile {
	t.Helper()
	p, err := model.NewNodeProfile(id, addr, "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile(%q,%q): %v", id, addr, err)
	}
	return p
}

// mustID was removed because it was unused; re-add if a future test needs it.

func newTestService(t *testing.T) (*NodeService, *fakeCatalogue, *fakeLifecycle, *fakeClock) {
	t.Helper()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	clock := &fakeClock{now: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}
	svc, err := NewNodeService(cat, lc, cat, func(addr string, _ model.NodeID) (port.SIPTransport, error) {
		return nil, errors.New("no transport in tests")
	}, clock)
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	return svc, cat, lc, clock
}

// TestNodeService_CreateStartStopList covers the four use-case methods
// required by task 6.2 and asserts a fresh node is idle.
func TestNodeService_CreateStartStopList(t *testing.T) {
	ctx := context.Background()
	svc, _, lc, _ := newTestService(t)

	node, err := svc.Create(ctx, testProfile(t, "34020000011310000001", "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if node.Status() != model.StatusIdle {
		t.Errorf("fresh node status = %v, want idle", node.Status())
	}
	if got := len(svc.List(ctx)); got != 1 {
		t.Fatalf("List() len = %d, want 1", got)
	}

	if err := svc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// A device with no upstream registration is already fully started
	// once the listener is bound (design D2): there is no register
	// transaction to wait for, so it advances straight to online.
	if st, _ := svc.Status(ctx, node.ID()); st != model.StatusOnline {
		t.Errorf("status after Start = %v, want online", st)
	}
	if !lc.isBound(node.ID()) {
		t.Error("listener was not bound by Start")
	}

	if err := svc.Stop(ctx, node.ID()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if st, _ := svc.Status(ctx, node.ID()); st != model.StatusOffline {
		t.Errorf("status after Stop = %v, want offline", st)
	}
	if lc.isBound(node.ID()) {
		t.Error("listener still bound after Stop; the port must be released")
	}
	if _, ok := svc.Get(ctx, node.ID()); !ok {
		t.Error("Get lost the node after Stop")
	}
}

// TestNodeService_StartFailureReleasesPort asserts a failed start returns an
// error that names the cause, leaves no half-started node, and lets the same
// address be bound on a retry (task 6.3).
func TestNodeService_StartFailureReleasesPort(t *testing.T) {
	ctx := context.Background()
	svc, _, lc, _ := newTestService(t)

	node, err := svc.Create(ctx, testProfile(t, "34020000011310000001", "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	lc.fail[node.ID().String()] = true

	err = svc.Start(ctx, node.ID())
	if err == nil {
		t.Fatal("Start succeeded despite bind failure")
	}
	if lc.isBound(node.ID()) {
		t.Error("a failed start left the port bound")
	}
	if st, _ := svc.Status(ctx, node.ID()); st != model.StatusIdle {
		t.Errorf("status after failed start = %v, want idle", st)
	}

	// The address is free again: clearing the failure lets Start succeed.
	lc.fail[node.ID().String()] = false
	if err := svc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("retry Start: %v", err)
	}
	if st, _ := svc.Status(ctx, node.ID()); st != model.StatusOnline {
		t.Errorf("status after retry = %v, want online", st)
	}
}

// TestNodeService_StartAdvancesUnregisteredDevice asserts the design-D2
// behaviour: a device with no upstream registration advances straight to
// online on Start. After a stop the explicit Mark methods remain the way a
// caller moves a node through registered back to online.
func TestNodeService_StartAdvancesUnregisteredDevice(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)

	node, err := svc.Create(ctx, testProfile(t, "34020000011310000001", "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st, _ := svc.Status(ctx, node.ID()); st != model.StatusOnline {
		t.Errorf("status after Start = %v, want online (D2 advance)", st)
	}

	// After a stop the only way back to online for an unregistered
	// device is Start again — it goes idle→online directly (D2).
	if err := svc.Stop(ctx, node.ID()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if st, _ := svc.Status(ctx, node.ID()); st != model.StatusOffline {
		t.Fatalf("status after Stop = %v, want offline", st)
	}
	if err := svc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start after stop: %v", err)
	}
	if st, _ := svc.Status(ctx, node.ID()); st != model.StatusOnline {
		t.Errorf("status after second Start = %v, want online", st)
	}

	// An illegal jump is surfaced, not swallowed.
	if err := svc.MarkRegistered(ctx, node.ID()); !errors.Is(err, model.ErrIllegalTransition) {
		t.Errorf("Online -> Registered error = %v, want ErrIllegalTransition", err)
	}
}

// TestNodeService_ChangedAtUsesInjectedClock asserts the service timestamps
// status changes through the injected Clock rather than reading the system
// clock directly.
func TestNodeService_ChangedAtUsesInjectedClock(t *testing.T) {
	ctx := context.Background()
	svc, _, _, clock := newTestService(t)

	node, err := svc.Create(ctx, testProfile(t, "34020000011310000001", "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	at, ok := svc.ChangedAt(node.ID())
	if !ok {
		t.Fatal("ChangedAt returned false after Create")
	}
	if !at.Equal(clock.now) {
		t.Errorf("ChangedAt = %v, want %v (the injected clock)", at, clock.now)
	}

	clock.now = clock.now.Add(time.Hour)
	if err := svc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if at, _ = svc.ChangedAt(node.ID()); !at.Equal(clock.now) {
		t.Errorf("ChangedAt after Start = %v, want %v", at, clock.now)
	}
}

// TestNewNodeService_RequiresPorts asserts the composition root cannot
// accidentally build a service with a missing dependency.
func TestNewNodeService_RequiresPorts(t *testing.T) {
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	factory := func(addr string, _ model.NodeID) (port.SIPTransport, error) { return nil, nil }
	clock := &fakeClock{}
	if _, err := NewNodeService(nil, lc, cat, factory, clock); err == nil {
		t.Error("nil registry accepted")
	}
	if _, err := NewNodeService(cat, nil, cat, factory, clock); err == nil {
		t.Error("nil lifecycle accepted")
	}
	if _, err := NewNodeService(cat, lc, nil, factory, clock); err == nil {
		t.Error("nil advancer accepted")
	}
	if _, err := NewNodeService(cat, lc, cat, nil, clock); err == nil {
		t.Error("nil factory accepted")
	}
	// A nil clock is allowed and defaults to real time.
	svc, err := NewNodeService(cat, lc, cat, factory, nil)
	if err != nil {
		t.Fatalf("nil clock rejected: %v", err)
	}
	if svc.clock == nil {
		t.Error("nil clock did not default to a real clock")
	}
	if svc.TransportFactory() == nil {
		t.Error("TransportFactory() is nil")
	}
}

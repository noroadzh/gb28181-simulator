package nodereg_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Two legal 20-digit ids (type code 131 -> device) used across the tests.
const (
	idA = "34020000011310000001"
	idB = "34020000011310000002"
)

func mustProfile(t *testing.T, id, addr string) model.NodeProfile {
	t.Helper()
	p, err := model.NewNodeProfile(id, addr, "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile(%q,%q): %v", id, addr, err)
	}
	return p
}

// fakeTransport is a listener stand-in; it records whether it was closed so
// tests can assert a failed start released the port again.
type fakeTransport struct {
	addr    string
	closed  bool
	factory *fakeFactory
}

func (f *fakeTransport) Send(context.Context, model.Message, string) error { return nil }
func (f *fakeTransport) Receive(context.Context) (model.Message, string, error) {
	return model.Message{}, "", errors.New("no message")
}

// Close releases the address, exactly like closing a real socket frees the
// port: otherwise a test could never rebind it after a stop or a fault.
func (f *fakeTransport) Close() error {
	f.closed = true
	if f.factory != nil {
		f.factory.release(f.addr)
	}
	return nil
}

// fakeFactory binds addresses and can be told to refuse specific ones,
// which is how a port collision is simulated.
type fakeFactory struct {
	mu       sync.Mutex
	bound    map[string]bool
	refuse   map[string]bool
	created  []*fakeTransport
	failNext bool
}

func newFakeFactory() *fakeFactory {
	return &fakeFactory{bound: map[string]bool{}, refuse: map[string]bool{}}
}

func (f *fakeFactory) bind(addr string) (port.SIPTransport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext || f.refuse[addr] || f.bound[addr] {
		return nil, fmt.Errorf("address %s already in use", addr)
	}
	f.bound[addr] = true
	tr := &fakeTransport{addr: addr, factory: f}
	f.created = append(f.created, tr)
	return tr, nil
}

// release marks the address free again after its listener was closed.
func (f *fakeFactory) release(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.bound, addr)
}

func (f *fakeFactory) last() *fakeTransport {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.created) == 0 {
		return nil
	}
	return f.created[len(f.created)-1]
}

func (f *fakeFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

// TestRegistry_RegisterListGetUnregister covers the basic catalogue
// operations and asserts a fresh node starts in StatusIdle.
func TestRegistry_RegisterListGetUnregister(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if node.Status() != model.StatusIdle {
		t.Errorf("fresh node status = %v, want idle", node.Status())
	}
	if got := len(reg.List(ctx)); got != 1 {
		t.Fatalf("List() len = %d, want 1", got)
	}
	got, ok := reg.Get(ctx, node.ID())
	if !ok {
		t.Fatal("Get returned false for a registered node")
	}
	if got.ID().String() != idA {
		t.Errorf("Get id = %q, want %q", got.ID().String(), idA)
	}
	if _, ok := reg.Get(ctx, mustParseID(t, idB)); ok {
		t.Error("Get returned true for an unregistered node")
	}
	if err := reg.Unregister(ctx, node.ID()); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if got := len(reg.List(ctx)); got != 0 {
		t.Errorf("List() len after Unregister = %d, want 0", got)
	}
	if err := reg.Unregister(ctx, node.ID()); err == nil {
		t.Error("Unregister of an unknown id succeeded, want error")
	}
}

// TestRegistry_DuplicateIDRejected asserts re-registering an id fails and
// leaves the incumbent untouched (task 5.2).
func TestRegistry_DuplicateIDRejected(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	first, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	// Same id, different address: still rejected on identity.
	if _, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5999")); err == nil {
		t.Fatal("duplicate id registration succeeded, want error")
	}
	if got := len(reg.List(ctx)); got != 1 {
		t.Errorf("List() len = %d, want 1 (no overwrite)", got)
	}
	still, _ := reg.Get(ctx, first.ID())
	if still.Profile().Addr() != "127.0.0.1:5060" {
		t.Errorf("incumbent addr = %q, want 127.0.0.1:5060 (must not be overwritten)",
			still.Profile().Addr())
	}
}

// TestRegistry_DuplicateAddrRejected asserts two nodes cannot claim the same
// signalling address, and that the error names the node already holding it
// (task 5.4).
func TestRegistry_DuplicateAddrRejected(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	if _, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060")); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := reg.Register(ctx, mustProfile(t, idB, "127.0.0.1:5060"))
	if err == nil {
		t.Fatal("duplicate address registration succeeded, want error")
	}
	// The error must identify the incumbent so an operator can act on it.
	if !strings.Contains(err.Error(), idA) {
		t.Errorf("error = %q, want it to name the incumbent node %s", err.Error(), idA)
	}
	if !strings.Contains(err.Error(), "127.0.0.1:5060") {
		t.Errorf("error = %q, want it to name the conflicting address", err.Error())
	}
}

// TestRegistry_ConcurrentAccess hammers the registry from many goroutines;
// run with -race it must report no data race, no duplicate id and no lost
// update (task 5.3).
func TestRegistry_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()

	const n = 32
	var wg sync.WaitGroup
	// Concurrent registrations of distinct ids/addresses.
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("34020000011310%06d", i)
			addr := fmt.Sprintf("127.0.0.1:%d", 15000+i)
			if _, err := reg.Register(ctx, mustProfile(t, id, addr)); err != nil {
				errCh <- err
			}
		}(i)
	}
	// Concurrent readers while writers run.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.List(ctx)
			_, _ = reg.Get(ctx, mustParseID(t, idA))
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatalf("concurrent Register: %v", err)
	default:
	}

	if got := len(reg.List(ctx)); got != n {
		t.Fatalf("List() len = %d, want %d", got, n)
	}

	// Concurrent unregister of everything; exactly one goroutine per id
	// must succeed.
	var okCount int32
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := mustParseID(t, fmt.Sprintf("34020000011310%06d", i))
			if err := reg.Unregister(ctx, id); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	mu.Lock()
	got := okCount
	mu.Unlock()
	if got != n {
		t.Errorf("successful Unregister count = %d, want %d", got, n)
	}
	if rest := len(reg.List(ctx)); rest != 0 {
		t.Errorf("List() len after unregister = %d, want 0", rest)
	}
}

// TestLifecycle_StartAdvancesToRegistering asserts Start binds exactly one
// listener and stops at Registering rather than jumping to Online
// (design D9, task 6.2).
func TestLifecycle_StartAdvancesToRegistering(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	factory := newFakeFactory()
	lc := nodereg.NewLifecycle(reg, factory.bind)

	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := lc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, err := lc.Status(ctx, node.ID())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != model.StatusRegistering {
		t.Errorf("status after Start = %v, want registering", st)
	}
	if factory.count() != 1 {
		t.Errorf("factory bound %d listeners, want 1", factory.count())
	}
	if lc.Transport(node.ID()) == nil {
		t.Error("Transport() is nil after a successful Start")
	}

	// Starting again is an illegal transition (registering -> registering).
	if err := lc.Start(ctx, node.ID()); !errors.Is(err, model.ErrIllegalTransition) {
		t.Errorf("second Start error = %v, want ErrIllegalTransition", err)
	}

	// Stop releases the listener and moves to offline.
	if err := lc.Stop(ctx, node.ID()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusOffline {
		t.Errorf("status after Stop = %v, want offline", st)
	}
	if tr := factory.last(); tr == nil || !tr.closed {
		t.Error("listener was not closed on Stop")
	}
	if lc.Transport(node.ID()) != nil {
		t.Error("Transport() still set after Stop")
	}
}

// TestLifecycle_StartBindFailureRollsBack asserts a bind failure returns an
// error naming the address, releases nothing it never bound, and rolls the
// node back to Idle on a first start (task 6.3).
func TestLifecycle_StartBindFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	factory := newFakeFactory()
	factory.failNext = true
	lc := nodereg.NewLifecycle(reg, factory.bind)

	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	err = lc.Start(ctx, node.ID())
	if err == nil {
		t.Fatal("Start succeeded despite bind failure")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:5060") {
		t.Errorf("error = %q, want it to name the address", err.Error())
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusIdle {
		t.Errorf("status after failed first start = %v, want idle", st)
	}
	// The address must be free again: a retry with a working factory binds.
	factory.failNext = false
	if err := lc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("retry Start after failure: %v", err)
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusRegistering {
		t.Errorf("status after retry = %v, want registering", st)
	}
}

// TestLifecycle_RestartFailureGoesToFault asserts a restart that cannot
// rebind is recorded as a fault rather than silently going idle.
func TestLifecycle_RestartFailureGoesToFault(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	factory := newFakeFactory()
	lc := nodereg.NewLifecycle(reg, factory.bind)

	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := lc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := lc.Stop(ctx, node.ID()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Offline -> Registering is legal, but the bind now fails.
	factory.failNext = true
	if err := lc.Start(ctx, node.ID()); err == nil {
		t.Fatal("restart succeeded despite bind failure")
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusFault {
		t.Errorf("status after failed restart = %v, want fault", st)
	}
}

// TestLifecycle_UnknownNode asserts operating on an unregistered id errors
// instead of panicking.
func TestLifecycle_UnknownNode(t *testing.T) {
	ctx := context.Background()
	lc := nodereg.NewLifecycle(nodereg.New(), newFakeFactory().bind)
	unknown := mustParseID(t, idB)
	if err := lc.Start(ctx, unknown); err == nil {
		t.Error("Start of unknown node succeeded")
	}
	if err := lc.Stop(ctx, unknown); err == nil {
		t.Error("Stop of unknown node succeeded")
	}
	if _, err := lc.Status(ctx, unknown); err == nil {
		t.Error("Status of unknown node succeeded")
	}
}

// TestLifecycle_TwoNodesIndependent asserts stopping one node leaves the
// other running with its own listener (spec: multi-node isolation).
func TestLifecycle_TwoNodesIndependent(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	factory := newFakeFactory()
	lc := nodereg.NewLifecycle(reg, factory.bind)

	a, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register A: %v", err)
	}
	b, err := reg.Register(ctx, mustProfile(t, idB, "127.0.0.1:5061"))
	if err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := lc.Start(ctx, a.ID()); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	if err := lc.Start(ctx, b.ID()); err != nil {
		t.Fatalf("Start B: %v", err)
	}
	if err := lc.Stop(ctx, a.ID()); err != nil {
		t.Fatalf("Stop A: %v", err)
	}
	if st, _ := lc.Status(ctx, b.ID()); st != model.StatusRegistering {
		t.Errorf("B status after stopping A = %v, want registering", st)
	}
	if lc.Transport(b.ID()) == nil {
		t.Error("B lost its listener when A was stopped")
	}
}

func mustParseID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

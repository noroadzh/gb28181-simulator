package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Two platform-small ids, so a test can put two of them in one process.
const (
	testPlatformSmall        = "34020000002160000001"
	testPlatformSmallFailing = "34020000002160000002"
)

// nodeSockets hands a node its own socket: the default one, unless a test
// has given that node one of its own. Two nodes in one process must not
// share a scripted queue, or one would answer what the other asked.
type nodeSockets struct {
	*fakeLifecycle
	mu      sync.Mutex
	perNode map[string]port.SIPTransport
}

func (l *nodeSockets) Transport(id model.NodeID) port.SIPTransport {
	l.mu.Lock()
	defer l.mu.Unlock()
	if tr, ok := l.perNode[id.String()]; ok {
		return tr
	}
	return l.fakeLifecycle.transport
}

// smallFixture wires a NodeService with a real Acceptor and, when the caller
// attaches one, a real Registrar, so "a platform-small serves and registers"
// is exercised without a socket. Both halves share upstream as the node's
// one listener — which is what happens on the wire, and what makes the
// sorting of arriving messages part of the behaviour under test.
func smallFixture(t *testing.T, upstream port.SIPTransport) (
	*NodeService, *fakeCatalogue, *fakeLifecycle, *fakeDevices, *Acceptor, *nodeSockets,
) {
	t.Helper()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	lc.transport = upstream
	sockets := &nodeSockets{fakeLifecycle: lc, perNode: map[string]port.SIPTransport{}}
	svc, err := NewNodeService(cat, sockets, cat, func(string) (port.SIPTransport, error) {
		return upstream, nil
	}, &fakeClock{now: time.Now()})
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	devices := newFakeDevices()
	acceptor, err := NewAcceptor(context.Background(), newSyncClock(time.Now()), &fakeChallenger{},
		&fakeAuthenticator{}, newFakeCredentials(), devices, newFakeMANSCDP(), nil, discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	if _, err := svc.WithAcceptor(acceptor); err != nil {
		t.Fatalf("WithAcceptor: %v", err)
	}
	t.Cleanup(func() {
		_ = acceptor.Close()
		// Every node still running takes its background work with it,
		// so no test leaves a reader behind on a scripted socket.
		for _, n := range cat.List(context.Background()) {
			_ = svc.Stop(context.Background(), n.ID())
		}
	})
	return svc, cat, lc, devices, acceptor, sockets
}

func smallProfile(t *testing.T, id string, serving bool, reg *model.Registration) model.NodeProfile {
	t.Helper()
	return smallProfileAt(t, id, "127.0.0.1:15062", serving, reg)
}

// smallProfileAt is smallProfile for a node that needs an address of its
// own: two nodes in one process cannot claim the same listener.
func smallProfileAt(
	t *testing.T,
	id, addr string,
	serving bool,
	reg *model.Registration,
) model.NodeProfile {
	t.Helper()
	profile := testProfile(t, id, addr)
	if serving {
		sec, err := model.DefaultPlatformServing("3402000000")
		if err != nil {
			t.Fatalf("DefaultPlatformServing: %v", err)
		}
		if profile, err = profile.WithPlatformServing(sec); err != nil {
			t.Fatalf("WithPlatformServing: %v", err)
		}
	}
	if reg != nil {
		var err error
		if profile, err = profile.WithRegistration(*reg); err != nil {
			t.Fatalf("WithRegistration: %v", err)
		}
	}
	return profile
}

// A platform-small with only a serving section is a platform that registers
// with nobody: it serves, comes online, and sends nothing upstream.
func TestNodeService_StartPlatformSmallServesOnly(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{}
	svc, cat, _, _, acceptor, _ := smallFixture(t, upstream)

	id := mustParse(t, testPlatformSmall)
	if _, err := svc.Create(context.Background(),
		smallProfile(t, testPlatformSmall, true, nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online", got)
	}
	if acceptor.Serving() != 1 {
		t.Errorf("Serving() = %d, want 1", acceptor.Serving())
	}
	if got := len(upstream.messages()); got != 0 {
		t.Errorf("a platform-small with no upstream sent %d messages, want 0", got)
	}
}

// Serving is inherent to being a platform: an omitted `platform:` section
// means "serve with the defaults", not "do not serve".
func TestNodeService_StartPlatformSmallServesWithDefaults(t *testing.T) {
	t.Parallel()
	svc, cat, _, _, acceptor, _ := smallFixture(t, &scriptedTransport{})

	id := mustParse(t, testPlatformSmall)
	if _, err := svc.Create(context.Background(),
		smallProfile(t, testPlatformSmall, false, nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if acceptor.Serving() != 1 {
		t.Errorf("Serving() = %d, want 1 even with no `platform:` section", acceptor.Serving())
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online", got)
	}
}

// Both halves: the node serves first and then registers with its upstream,
// over one listener it does not share with anyone. The node has one state
// machine, so the second half must not fail on the transition the serving
// half already made.
func TestNodeService_StartPlatformSmallServesAndRegisters(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	svc, cat, _, _, acceptor, _ := smallFixture(t, upstream)
	reg := testRegistration(t, 3600, 2*time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	id := mustParse(t, testPlatformSmall)
	if _, err := svc.Create(context.Background(),
		smallProfile(t, testPlatformSmall, true, &reg)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online", got)
	}
	if acceptor.Serving() != 1 {
		t.Errorf("Serving() = %d, want 1", acceptor.Serving())
	}
	node, _ := cat.Get(context.Background(), id)
	if _, ok := node.RegistrationResult(); !ok {
		t.Fatal("upstream registration result not recorded on the node")
	}
	sent := upstream.messages()
	if len(sent) == 0 || sent[0].msg.Method() != "REGISTER" {
		t.Fatalf("upstream half sent %d messages, want a REGISTER first", len(sent))
	}
}

// A failed upstream half unwinds the serving half: the node faults, stops
// serving, forgets the devices it had accepted and gives its port back.
func TestNodeService_StartPlatformSmallUpstreamFailureUnwindsServing(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{} // no answer at all → timeout
	svc, cat, lc, devices, acceptor, _ := smallFixture(t, upstream)
	reg := testRegistration(t, 3600, 50*time.Millisecond)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	id := mustParse(t, testPlatformSmall)
	ctx := context.Background()
	if _, err := svc.Create(ctx, smallProfile(t, testPlatformSmall, true, &reg)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A device that got registered before the upstream half failed: the
	// unwind must take it off the table with everything else.
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060",
		Now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	if err := devices.Upsert(ctx, id, dev.WithGranted(3600, dev.RegisteredAt())); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	err = svc.Start(ctx, id)
	if err == nil {
		t.Fatal("Start succeeded although the upstream half cannot be reached")
	}
	// The cause has to name the half that failed, or a faulted node is a
	// mystery to whoever reads the log.
	if !strings.Contains(err.Error(), "upstream half") {
		t.Errorf("error = %q, want it to name the upstream half", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusFault {
		t.Errorf("status = %s, want fault", got)
	}
	if acceptor.Serving() != 0 {
		t.Errorf("Serving() = %d, want 0 after the serving half was unwound", acceptor.Serving())
	}
	if got := len(devices.List(ctx, id)); got != 0 {
		t.Errorf("online table has %d rows after the unwind, want 0", got)
	}
	if lc.isBound(id) {
		t.Error("listener still bound after the node faulted")
	}
}

// Stopping a platform-small ends both halves: it says goodbye to its
// upstream, stops serving, stops its keepalives, and leaves the node offline
// with nothing left running.
func TestNodeService_StopPlatformSmallEndsBothHalves(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	svc, cat, lc, devices, acceptor, sockets := smallFixture(t, upstream)
	reg := testRegistration(t, 3600, 2*time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}
	keeper, err := NewKeeper(context.Background(), cat, sockets, r, &stubKeepaliveCodec{},
		&fakeClock{now: time.Now()}, realTickerFactory(), discardLogger())
	if err != nil {
		t.Fatalf("NewKeeper: %v", err)
	}
	if _, err := svc.WithKeeper(keeper); err != nil {
		t.Fatalf("WithKeeper: %v", err)
	}
	t.Cleanup(func() { keeper.Close() })

	id := mustParse(t, testPlatformSmall)
	ctx := context.Background()
	if _, err := svc.Create(ctx, smallProfile(t, testPlatformSmall, true, &reg)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if keeper.Running() != 1 {
		t.Errorf("keeper Running() = %d, want 1 while the node is online", keeper.Running())
	}

	if err := svc.Stop(ctx, id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOffline {
		t.Errorf("status = %s, want offline", got)
	}
	if acceptor.Serving() != 0 {
		t.Errorf("Serving() = %d, want 0 after Stop", acceptor.Serving())
	}
	if keeper.Running() != 0 {
		t.Errorf("keeper Running() = %d, want 0 after Stop", keeper.Running())
	}
	if got := len(devices.List(ctx, id)); got != 0 {
		t.Errorf("online table has %d rows after Stop, want 0", got)
	}
	if lc.isBound(id) {
		t.Error("listener still bound after Stop")
	}
	// The last thing the node said upstream was a goodbye: a REGISTER that
	// asks for no time at all.
	if !sentUnregister(upstream) {
		t.Errorf("no unregister among %d upstream messages, want one", len(upstream.messages()))
	}
}

// sentUnregister reports whether one of the messages sent upstream asks for
// no time at all — the goodbye a node owes the platform above it.
func sentUnregister(tr *scriptedTransport) bool {
	for _, m := range tr.messages() {
		if m.msg.Method() != "REGISTER" {
			continue
		}
		if h, ok := m.msg.Header("Expires"); ok && h.Value() == "0" {
			return true
		}
	}
	return false
}

// Starting a platform-small twice replaces its serving goroutine instead of
// running two of them: a node is either serving or it is not, and "twice"
// is not a state it can be in.
func TestNodeService_PlatformSmallRestartReplacesServing(t *testing.T) {
	t.Parallel()
	svc, cat, _, _, acceptor, _ := smallFixture(t, &scriptedTransport{})

	id := mustParse(t, testPlatformSmall)
	ctx := context.Background()
	if _, err := svc.Create(ctx, smallProfile(t, testPlatformSmall, true, nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := svc.Stop(ctx, id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := svc.Start(ctx, id); err != nil {
		t.Fatalf("Start again: %v", err)
	}
	if acceptor.Serving() != 1 {
		t.Errorf("Serving() = %d, want 1 after a restart", acceptor.Serving())
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online", got)
	}
}

// Two platform-small nodes in one process keep to themselves: one failing
// its upstream must not disturb the other's serving, devices or status.
func TestNodeService_PlatformSmallNodesAreIndependent(t *testing.T) {
	t.Parallel()
	good := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	bad := &scriptedTransport{} // never answers
	svc, cat, _, _, acceptor, sockets := smallFixture(t, good)
	sockets.perNode[testPlatformSmallFailing] = bad
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	ctx := context.Background()
	okID := mustParse(t, testPlatformSmall)
	badID := mustParse(t, testPlatformSmallFailing)
	nodes := []struct {
		id   model.NodeID
		addr string
		reg  model.Registration
	}{
		{okID, "127.0.0.1:15062", testRegistration(t, 3600, 2*time.Second)},
		{badID, "127.0.0.1:15063", testRegistration(t, 3600, 50*time.Millisecond)},
	}
	for _, c := range nodes {
		profile := smallProfileAt(t, c.id.String(), c.addr, true, &c.reg)
		if _, err := svc.Create(ctx, profile); err != nil {
			t.Fatalf("Create %s: %v", c.id, err)
		}
	}

	var wg sync.WaitGroup
	for _, c := range nodes {
		wg.Add(1)
		go func(id model.NodeID) {
			defer wg.Done()
			_ = svc.Start(ctx, id)
		}(c.id)
	}
	wg.Wait()

	if got := statusOf(t, cat, okID); got != model.StatusOnline {
		t.Errorf("reachable node status = %s, want online", got)
	}
	if got := statusOf(t, cat, badID); got != model.StatusFault {
		t.Errorf("unreachable node status = %s, want fault", got)
	}
	// The failing node was unwound; the healthy one is still serving.
	if acceptor.Serving() != 1 {
		t.Errorf("Serving() = %d, want 1 — only the healthy node", acceptor.Serving())
	}
}

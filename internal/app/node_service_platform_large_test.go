package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// largeFixture wires a NodeService with a real Acceptor so "a platform-large
// serves, and registers upstream when it declares one" is exercised without a
// socket. Both halves share upstream as the node's one listener — which is
// what happens on the wire, and what makes the sorting of arriving messages
// part of the behaviour under test.
func largeFixture(t *testing.T, upstream port.SIPTransport) (
	*NodeService, *fakeCatalogue, *fakeLifecycle, *fakeDevices, *Acceptor, *nodeSockets,
) {
	t.Helper()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	lc.transport = upstream
	sockets := &nodeSockets{fakeLifecycle: lc, perNode: map[string]port.SIPTransport{}}
	svc, err := NewNodeService(cat, sockets, cat, func(addr string, _ model.NodeID) (port.SIPTransport, error) {
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
		for _, n := range cat.List(context.Background()) {
			_ = svc.Stop(context.Background(), n.ID())
		}
	})
	return svc, cat, lc, devices, acceptor, sockets
}

// largeProfile is a platform-large profile: identity only, no serving policy
// and no upstream. A platform-large is a platform by its kind, so it serves by
// default — that default lives in the config layer, not in the profile.
// Declaring an upstream is what makes it the other half of the node.
func largeProfile(t *testing.T, id string, reg *model.Registration) model.NodeProfile {
	t.Helper()
	profile := testProfile(t, id, "127.0.0.1:15072")
	if reg != nil {
		var err error
		if profile, err = profile.WithRegistration(*reg); err != nil {
			t.Fatalf("WithRegistration: %v", err)
		}
	}
	return profile
}

// A platform-large with no upstream declared is a platform to its own
// downstreams and registers with nobody: it serves, comes online, and sends
// nothing upstream.
func TestNodeService_StartPlatformLargeServesOnly(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{}
	svc, cat, _, _, acceptor, _ := largeFixture(t, upstream)

	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(context.Background(), largeProfile(t, testPlatformLarge, nil)); err != nil {
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
		t.Errorf("a node with no upstream sent %d messages, want none", got)
	}
}

// Both halves: the node serves first and then registers with its upstream,
// over one listener it does not share with anyone. The node has one state
// machine, so the second half must not fail on the transition the serving half
// already made.
func TestNodeService_StartPlatformLargeServesAndRegisters(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	svc, cat, _, _, acceptor, _ := largeFixture(t, upstream)
	reg := testRegistration(t, 3600, 2*time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(context.Background(), largeProfile(t, testPlatformLarge, &reg)); err != nil {
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
func TestNodeService_StartPlatformLargeUpstreamFailureUnwindsServing(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{} // no answer at all → timeout
	svc, cat, lc, devices, acceptor, _ := largeFixture(t, upstream)
	reg := testRegistration(t, 3600, 50*time.Millisecond)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	id := mustParse(t, testPlatformLarge)
	ctx := context.Background()
	if _, err := svc.Create(ctx, largeProfile(t, testPlatformLarge, &reg)); err != nil {
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

// Stopping a platform-large that declared an upstream ends both halves: it
// says goodbye to that upstream, stops serving, stops its keepalives, and
// leaves the node offline with nothing left running. The goodbye is the point
// that separates this from a plain platform-large: a node that registered
// somewhere owes that platform an `Expires: 0` when it leaves.
func TestNodeService_StopPlatformLargeWithUpstreamEndsBothHalves(t *testing.T) {
	t.Parallel()
	upstream := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	svc, cat, lc, devices, acceptor, sockets := largeFixture(t, upstream)
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
	t.Cleanup(func() { _ = keeper.Close() })

	id := mustParse(t, testPlatformLarge)
	ctx := context.Background()
	if _, err := svc.Create(ctx, largeProfile(t, testPlatformLarge, &reg)); err != nil {
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
		t.Errorf("keeper Running() = 0 expected, got %d", keeper.Running())
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

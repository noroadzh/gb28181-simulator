package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// mustParse parses a node id or fails the test.
func mustParse(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

// statusOf returns the node's status as the registry holds it.
func statusOf(t *testing.T, cat *fakeCatalogue, id model.NodeID) model.Status {
	t.Helper()
	node, ok := cat.Get(context.Background(), id)
	if !ok {
		t.Fatalf("node %s is gone from the catalogue", id)
	}
	return node.Status()
}

const testPlatformLarge = "34020000002000000001"

// registrationFixture wires a NodeService whose node carries a
// registration and whose lifecycle hands out tr as the node's listener.
func registrationFixture(
	t *testing.T,
	id string,
	tr *scriptedTransport,
) (*NodeService, *fakeCatalogue, *fakeLifecycle) {
	t.Helper()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	lc.transport = tr
	adv := cat

	svc, err := NewNodeService(cat, lc, adv, func(addr string, _ model.NodeID) (port.SIPTransport, error) {
		return tr, nil
	}, &fakeClock{now: time.Now()})
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	return svc, cat, lc
}

// A node with no registration keeps the pre-change behaviour: start binds
// the listener and stops at registering.
func TestNodeService_StartWithoutRegistrationStaysRegistering(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{}
	svc, cat, lc := registrationFixture(t, testDevice, tr)

	id := mustParse(t, testDevice)
	if _, err := svc.Create(context.Background(),
		testProfile(t, testDevice, "127.0.0.1:15060")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(tr.messages()) != 0 {
		t.Errorf("an unregistered node sent %d messages, want 0", len(tr.messages()))
	}
	if got := statusOf(t, cat, id); got != model.StatusRegistering {
		t.Errorf("status = %s, want registering", got)
	}
	if !lc.isBound(id) {
		t.Error("listener not bound")
	}
}

// Start registers a device end to end: the node ends online and the
// registration outcome is recorded on it.
func TestNodeService_StartRegistersDevice(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	reg := testRegistration(t, 3600, 2*time.Second)
	svc, cat, _ := registrationFixture(t, testDevice, tr)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	id := mustParse(t, testDevice)
	profile, err := testProfile(t, testDevice, "127.0.0.1:15060").WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	if _, err := svc.Create(context.Background(), profile); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online", got)
	}
	node, _ := cat.Get(context.Background(), id)
	result, ok := node.RegistrationResult()
	if !ok {
		t.Fatal("registration result not recorded on the node")
	}
	if result.Server() != testServer || result.GrantedExpiry() != 3600 {
		t.Errorf("recorded result = %s", result)
	}
}

// A failed registration must not leave a half-started node: fault it and
// give the port back.
func TestNodeService_StartRegistrationFailureFaults(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{} // no answer at all → timeout
	reg := testRegistration(t, 3600, 50*time.Millisecond)
	svc, cat, lc := registrationFixture(t, testDevice, tr)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	id := mustParse(t, testDevice)
	profile, err := testProfile(t, testDevice, "127.0.0.1:15060").WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	if _, err := svc.Create(context.Background(), profile); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err = svc.Start(context.Background(), id)
	if err == nil {
		t.Fatal("Start succeeded, want a registration failure")
	}
	var regErr *RegistrationError
	if !errors.As(err, &regErr) {
		t.Fatalf("got %v, want a *RegistrationError", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusFault {
		t.Errorf("status = %s, want fault", got)
	}
	if lc.isBound(id) {
		t.Error("the listener was not released after a failed registration")
	}
	if lc.faults != 1 {
		t.Errorf("lifecycle.Fail called %d times, want 1", lc.faults)
	}
	if lc.lastFault == nil {
		t.Error("lifecycle.Fail received no reason")
	}
}

// Only device identities register; a platform carrying the same block must
// not start sending REGISTERs.
func TestNodeService_StartNonDeviceDoesNotRegister(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{}
	reg := testRegistration(t, 3600, time.Second)
	svc, cat, _ := registrationFixture(t, testPlatformLarge, tr)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}

	id := mustParse(t, testPlatformLarge)
	profile, err := testProfile(t, testPlatformLarge, "127.0.0.1:15061").WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	if _, err := svc.Create(context.Background(), profile); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(tr.messages()) != 0 {
		t.Errorf("a platform node sent %d messages, want 0", len(tr.messages()))
	}
	if got := statusOf(t, cat, id); got != model.StatusRegistering {
		t.Errorf("status = %s, want registering", got)
	}
}

// A node that wants to register must fail loudly when the composition root
// forgot the registrar, rather than silently staying unregistered.
func TestNodeService_RegistrationWithoutRegistrarFails(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{}
	reg := testRegistration(t, 3600, time.Second)
	svc, cat, lc := registrationFixture(t, testDevice, tr)

	id := mustParse(t, testDevice)
	profile, err := testProfile(t, testDevice, "127.0.0.1:15060").WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	if _, err := svc.Create(context.Background(), profile); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err == nil {
		t.Fatal("Start succeeded without a registrar, want failure")
	}
	if got := statusOf(t, cat, id); got != model.StatusFault {
		t.Errorf("status = %s, want fault", got)
	}
	if lc.isBound(id) {
		t.Error("the listener was not released")
	}
}

func TestNodeService_WithRegistrarRejectsNil(t *testing.T) {
	t.Parallel()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	svc, err := NewNodeService(cat, lc, cat,
		func(addr string, _ model.NodeID) (port.SIPTransport, error) { return &scriptedTransport{}, nil },
		&fakeClock{})
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	if _, err := svc.WithRegistrar(nil); err == nil {
		t.Fatal("WithRegistrar(nil) must fail")
	}
}

// A faulted node can be started again once the cause is fixed.
func TestNodeService_FaultedNodeCanRestart(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{}
	reg := testRegistration(t, 3600, 50*time.Millisecond)
	svc, cat, _ := registrationFixture(t, testDevice, tr)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	if _, err := svc.WithRegistrar(r); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}
	id := mustParse(t, testDevice)
	profile, err := testProfile(t, testDevice, "127.0.0.1:15060").WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	if _, err := svc.Create(context.Background(), profile); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err == nil {
		t.Fatal("Start succeeded, want a timeout failure")
	}
	// Second attempt: the platform answers this time.
	tr.in = []incoming{{msg: response(t, 401, callIDHeader(testCallID),
		model.NewHeader("WWW-Authenticate", `Digest realm="3402000000", nonce="n1", qop="auth", algorithm=MD5`)),
		peer: testServer}}
	r2 := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r2.newCallID = func() string { return testCallID }
	if _, err := svc.WithRegistrar(r2); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}
	tr.in = append(tr.in, incoming{
		msg:  response(t, 200, callIDHeader(testCallID)),
		peer: testServer,
	})
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("restart after a fault: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status after restart = %s, want online", got)
	}
}

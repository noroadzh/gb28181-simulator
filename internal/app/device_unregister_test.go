package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// unregisterFixture wires a NodeService whose device carries a registration
// and talks through tr, plus the keeper that holds it open.
func unregisterFixture(
	t *testing.T,
	tr port.SIPTransport,
	reg model.Registration,
	clock *syncClock,
) (*NodeService, *fakeCatalogue, *fakeLifecycle, *Keeper, model.NodeID) {
	t.Helper()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	lc.transport = tr

	svc, err := NewNodeService(cat, lc, cat, func(string) (port.SIPTransport, error) {
		return tr, nil
	}, clock)
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	registrar := newTestRegistrar(t, &stubAuthorizer{}, clock)
	if _, err := svc.WithRegistrar(registrar); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}
	keeper, err := NewKeeper(context.Background(), cat, lc, registrar,
		&stubKeepaliveCodec{}, clock, func(time.Duration) port.Ticker {
			return newScriptedTicker()
		}, discardLogger())
	if err != nil {
		t.Fatalf("NewKeeper: %v", err)
	}
	if _, err := svc.WithKeeper(keeper); err != nil {
		t.Fatalf("WithKeeper: %v", err)
	}

	profile, err := testProfile(t, testDevice, "127.0.0.1:15060").WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	node, err := svc.Create(context.Background(), profile)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return svc, cat, lc, keeper, node.ID()
}

func unregisterRegistration(t *testing.T) model.Registration {
	t.Helper()
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:               testServer,
		Password:             "secret",
		Expires:              3600,
		Timeout:              2 * time.Second,
		HeartbeatInterval:    60 * time.Second,
		HeartbeatTimeout:     50 * time.Millisecond,
		HeartbeatMaxFailures: 3,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	return reg
}

// expiresOf reads the Expires header of a sent REGISTER.
func expiresOf(t *testing.T, m model.Message) string {
	t.Helper()
	h, ok := m.Header("Expires")
	if !ok {
		t.Fatalf("REGISTER carries no Expires header: %s", m.String())
	}
	return h.Value()
}

// Leaving is a REGISTER with `Expires: 0`, and it answers the challenge
// exactly like joining does.
func TestRegistrar_UnregisterSendsExpiresZero(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	tr.challenge = true
	clock := newSyncClock(time.Now())
	reg := unregisterRegistration(t)

	cat := newFakeCatalogue()
	profile := testProfile(t, testDevice, "127.0.0.1:15060")
	node, err := cat.Register(context.Background(), profile)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	r := newTestRegistrar(t, &stubAuthorizer{}, clock)

	if _, err := r.Unregister(context.Background(), tr, node, reg); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	msgs := tr.messages()
	if len(msgs) != 2 {
		t.Fatalf("sent %d messages, want 2 (REGISTER + the one answering 401)", len(msgs))
	}
	for i, m := range msgs {
		if m.Method() != "REGISTER" {
			t.Fatalf("message %d is %s, want REGISTER", i, m.Method())
		}
		if got := expiresOf(t, m); got != "0" {
			t.Errorf("message %d Expires = %s, want 0", i, got)
		}
	}
	if _, ok := msgs[1].Header("Authorization"); !ok {
		t.Error("the retried REGISTER carries no Authorization")
	}
}

// A leave that gets no answer fails at the timeout stage rather than
// pretending the platform agreed.
func TestRegistrar_UnregisterTimeoutNamesStage(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	tr.silent["REGISTER"] = true
	clock := newSyncClock(time.Now())
	reg := unregisterRegistration(t)

	cat := newFakeCatalogue()
	node, err := cat.Register(context.Background(), testProfile(t, testDevice, "127.0.0.1:15060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	r := newTestRegistrar(t, &stubAuthorizer{}, clock)

	_, err = r.Unregister(context.Background(), tr, node, reg)
	if err == nil {
		t.Fatal("Unregister succeeded although the platform never answered")
	}
	var staged port.StagedFailure
	if !errors.As(err, &staged) {
		t.Fatalf("error does not name a stage: %v", err)
	}
	if staged.FailureStage() != string(StageTimeout) {
		t.Errorf("stage = %q, want %q", staged.FailureStage(), StageTimeout)
	}
}

// A node that is online leaves for good: it goes offline, its listener is
// released, and nothing keeps it alive any more.
func TestNodeService_UnregisterGoesOffline(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	tr.challenge = true
	clock := newSyncClock(time.Now())
	reg := unregisterRegistration(t)
	svc, cat, lc, keeper, id := unregisterFixture(t, tr, reg, clock)

	ctx := context.Background()
	if err := svc.Start(ctx, id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Fatalf("status after start = %s, want online", got)
	}
	if keeper.Running() != 1 {
		t.Fatalf("keeper running %d sessions, want 1", keeper.Running())
	}

	if err := svc.Unregister(ctx, id); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOffline {
		t.Errorf("status = %s, want offline", got)
	}
	if lc.isBound(id) {
		t.Error("listener still bound after unregistering")
	}
	if keeper.Running() != 0 {
		t.Errorf("keeper still running %d sessions after unregistering", keeper.Running())
	}
}

// A platform that refuses the leave does not cost the node its
// registration: it stays online, and the caller hears which stage failed.
func TestNodeService_UnregisterFailureKeepsNodeOnline(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	tr.challenge = true
	clock := newSyncClock(time.Now())
	reg := unregisterRegistration(t)
	svc, cat, _, keeper, id := unregisterFixture(t, tr, reg, clock)

	ctx := context.Background()
	if err := svc.Start(ctx, id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	tr.silent["REGISTER"] = true

	if err := svc.Unregister(ctx, id); err == nil {
		t.Fatal("Unregister succeeded although the platform stayed silent")
	} else if !errors.Is(err, ErrRegisterTimeout) {
		t.Errorf("error = %v, want the registration timeout", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online — the registration still stands", got)
	}
	if keeper.Running() != 1 {
		t.Errorf("keeper running %d sessions, want 1 — the node is still online", keeper.Running())
	}
	keeper.Stop(id)
}

// Leaving is only legal for a node that actually holds a registration.
func TestNodeService_UnregisterRejectsOfflineNode(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	clock := newSyncClock(time.Now())
	reg := unregisterRegistration(t)
	svc, _, _, _, id := unregisterFixture(t, tr, reg, clock)

	err := svc.Unregister(context.Background(), id)
	if !errors.Is(err, model.ErrIllegalTransition) {
		t.Fatalf("error = %v, want ErrIllegalTransition", err)
	}
	if n := tr.count("REGISTER"); n != 0 {
		t.Errorf("sent %d REGISTERs for an offline node, want 0", n)
	}
}

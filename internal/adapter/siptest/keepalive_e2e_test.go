package siptest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	sipauth "github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptest"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/platform/clock"
)

// keepaliveService wires the real pieces — registry, lifecycle, registrar
// and keeper — over real sockets, with the production clock and ticker.
// Closing the returned function stops the background work.
func keepaliveService(t *testing.T, ctx context.Context) (*app.NodeService, func()) {
	t.Helper()
	registry := nodereg.New()
	factory := func(addr string, _ model.NodeID) (port.SIPTransport, error) {
		tr, err := siptransport.New("udp://" + addr)
		if err != nil {
			return nil, err
		}
		return siptransport.NewPortAdapter(tr), nil
	}
	lifecycle := nodereg.NewLifecycle(registry, factory)
	svc, err := app.NewNodeService(registry, lifecycle, registry, factory, nil)
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	authorizer, err := sipauth.NewAuthorizerAdapter(sipauth.NewAuthorizer(nil))
	if err != nil {
		t.Fatalf("authorizer: %v", err)
	}
	registrar, err := app.NewRegistrar(authorizer, clock.Real(), discardLogger())
	if err != nil {
		t.Fatalf("NewRegistrar: %v", err)
	}
	if _, err := svc.WithRegistrar(registrar); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}
	keeper, err := app.NewKeeper(ctx, registry, lifecycle, registrar,
		manscdp.NewKeepaliveCodec(), clock.Real(), clock.RealTicker(), discardLogger())
	if err != nil {
		t.Fatalf("NewKeeper: %v", err)
	}
	if _, err := svc.WithKeeper(keeper); err != nil {
		t.Fatalf("WithKeeper: %v", err)
	}
	return svc, func() { _ = keeper.Close() }
}

// keepaliveRegistration is a registration whose heartbeat runs fast enough
// for a test: a beat every 200ms, unanswered after 100ms, tolerated twice.
func keepaliveRegistration(t *testing.T, server string, maxFailures uint32) model.Registration {
	t.Helper()
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:               server,
		ServerID:             e2eServer,
		Password:             e2ePasswd,
		Expires:              3600,
		Timeout:              2 * time.Second,
		HeartbeatInterval:    200 * time.Millisecond,
		HeartbeatTimeout:     100 * time.Millisecond,
		HeartbeatMaxFailures: maxFailures,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	return reg
}

// waitUntil polls cond until it holds or the deadline passes.
func waitUntil(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// An online device keeps beating: the platform receives real MESSAGE
// keepalives over the wire, with a sequence number that moves forward.
func TestDeviceSendsKeepalivesOverUDP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	uas, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewUAS: %v", err)
	}
	defer func() { _ = uas.Close() }()
	go func() { _ = uas.Serve(ctx) }()

	svc, stop := keepaliveService(t, ctx)
	defer stop()

	reg := keepaliveRegistration(t, uas.Addr(), 3)
	created, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, created.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitUntil(t, "two keepalives", 10*time.Second, func() bool {
		return len(uas.Keepalives()) >= 2
	})
	node, _ := svc.Get(ctx, created.ID())
	if node.Status() != model.StatusOnline {
		t.Errorf("status = %s, want online", node.Status())
	}

	beats := uas.Keepalives()
	if len(beats) < 2 {
		t.Fatalf("received %d keepalives, want at least 2", len(beats))
	}
	for i, beat := range beats {
		if beat.Method() != "MESSAGE" {
			t.Fatalf("keepalive %d is a %s", i, beat.Method())
		}
		if got := headerValue(t, beat, "Content-Type"); got != "Application/MANSCDP+XML" {
			t.Errorf("keepalive %d Content-Type = %q, want Application/MANSCDP+XML", i, got)
		}
		if !strings.Contains(beat.Body(), "<CmdType>Keepalive</CmdType>") {
			t.Errorf("keepalive %d body is not a keepalive notify: %s", i, beat.Body())
		}
		if !strings.Contains(beat.Body(), "<DeviceID>"+e2eDeviceA+"</DeviceID>") {
			t.Errorf("keepalive %d body names a different device: %s", i, beat.Body())
		}
	}
	if first, second := snOf(t, beats[0].Body()), snOf(t, beats[1].Body()); second <= first {
		t.Errorf("sequence number did not increase: %d then %d", first, second)
	}
}

// A platform that stops answering costs the node its online status after
// the tolerated number of beats, and the beats stop with it.
func TestDeviceFaultsWhenKeepalivesGoUnanswered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	uas, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd, siptest.WithSilentMessages())
	if err != nil {
		t.Fatalf("NewUAS: %v", err)
	}
	defer func() { _ = uas.Close() }()
	go func() { _ = uas.Serve(ctx) }()

	svc, stop := keepaliveService(t, ctx)
	defer stop()

	reg := keepaliveRegistration(t, uas.Addr(), 3)
	created, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, created.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitUntil(t, "the node to fault", 10*time.Second, func() bool {
		n, _ := svc.Get(ctx, created.ID())
		return n.Status() == model.StatusFault
	})
	// The listener is free again: the same address can be bound at once.
	addr := freeAddr(t)
	tr, err := siptransport.New("udp://" + addr)
	if err != nil {
		t.Fatalf("rebind %s: %v", addr, err)
	}
	_ = tr.Close()

	beats := len(uas.Keepalives())
	time.Sleep(600 * time.Millisecond)
	if later := len(uas.Keepalives()); later != beats {
		t.Errorf("keepalives went from %d to %d after faulting — the loop kept running", beats, later)
	}
}

// Leaving for good: the node unregisters, goes offline, and stops beating.
func TestDeviceUnregisterStopsKeepalives(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	uas, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewUAS: %v", err)
	}
	defer func() { _ = uas.Close() }()
	go func() { _ = uas.Serve(ctx) }()

	svc, stop := keepaliveService(t, ctx)
	defer stop()

	reg := keepaliveRegistration(t, uas.Addr(), 3)
	created, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, created.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, "a keepalive", 10*time.Second, func() bool {
		return len(uas.Keepalives()) >= 1
	})

	if err := svc.Unregister(ctx, created.ID()); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	node, _ := svc.Get(ctx, created.ID())
	if node.Status() != model.StatusOffline {
		t.Errorf("status = %s, want offline", node.Status())
	}

	before := len(uas.Received())
	time.Sleep(600 * time.Millisecond)
	if after := len(uas.Received()); after != before {
		t.Errorf("platform received %d more messages after unregistering, want 0", after-before)
	}
}

// snOf reads the sequence number out of a rendered keepalive body.
func snOf(t *testing.T, body string) int {
	t.Helper()
	const open, close = "<SN>", "</SN>"
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("no SN in body: %s", body)
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("unterminated SN in body: %s", body)
	}
	n := 0
	for _, r := range rest[:end] {
		if r < '0' || r > '9' {
			t.Fatalf("SN is not a number: %q", rest[:end])
		}
		n = n*10 + int(r-'0')
	}
	return n
}

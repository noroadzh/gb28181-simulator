package siptest_test

import (
	"context"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
	"github.com/your-org/gb28181-simulator/internal/adapter/capture"
	"github.com/your-org/gb28181-simulator/internal/adapter/credstore"
	"github.com/your-org/gb28181-simulator/internal/adapter/devicereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	sipauth "github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/platform/clock"
)

// exceptionService is a small variation on platformService that also wires
// the fault store through both the acceptor (request gate) and the node
// service (HTTP/query API), and uses the transport's WithNodeID option so
// every wire event is stamped with its owning node.
func exceptionService(t *testing.T, ctx context.Context, accounts *credstore.Store, devices *devicereg.Registry) (
	*app.NodeService, *app.Keeper, *app.Acceptor, *app.FaultStoreAdapter, func(),
) {
	t.Helper()
	registry := nodereg.New()
	factory := func(addr string, nodeID model.NodeID) (port.SIPTransport, error) {
		tr, err := siptransport.New("udp://"+addr, siptransport.WithNodeID(nodeID.String()))
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
	authenticator, err := sipauth.NewAuthenticatorAdapter(sipauth.NewResponder(nil))
	if err != nil {
		t.Fatalf("authenticator: %v", err)
	}
	challenger, err := sipauth.NewChallengerAdapter(sipauth.NewChallenger(nil))
	if err != nil {
		t.Fatalf("challenger: %v", err)
	}
	acceptor, err := app.NewAcceptor(ctx, clock.Real(), challenger, authenticator,
		accounts, devices, manscdp.NewMANSCDPCodec(), clock.RealTicker(), discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	if _, err := svc.WithAcceptor(acceptor); err != nil {
		t.Fatalf("WithAcceptor: %v", err)
	}
	faults := app.NewFaultStore(registry, nil)
	acceptor.WithFaults(faults)
	if _, err := svc.WithFaults(faults); err != nil {
		t.Fatalf("WithFaults: %v", err)
	}
	return svc, keeper, acceptor, faults, func() {
		_ = acceptor.Close()
		_ = keeper.Close()
	}
}

// A canned refusal poisons the first registration; clearing the profile
// lets a fresh device through. Every packet that crossed the platform's
// socket is in the capture ring under the platform's own node id.
func TestExceptionAndCaptureE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, acceptor, faults, stop := exceptionService(t, ctx, accounts, devices)
	defer stop()
	_ = acceptor

	store := capture.New(nil)
	audit.SetEmitter(capture.AuditBridge(store))
	defer audit.SetEmitter(nil)

	platformID, err := model.ParseNodeID(e2eServer)
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	platform, err := svc.Create(ctx, platformProfile(t, e2eServer, freeAddr(t)))
	if err != nil {
		t.Fatalf("Create(platform): %v", err)
	}
	cred, err := model.NewCredentials(e2eDeviceA, e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if err := accounts.Add(platformID, cred); err != nil {
		t.Fatalf("accounts.Add: %v", err)
	}
	if err := svc.Start(ctx, platform.ID()); err != nil {
		t.Fatalf("Start(platform): %v", err)
	}

	// Poison REGISTER with a canned 403.
	if err := svc.InstallFault(ctx, platform.ID(), model.FaultProfile{
		Canned: map[string]int{"REGISTER": 403},
	}); err != nil {
		t.Fatalf("InstallFault: %v", err)
	}

	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:    platform.Profile().Addr(),
		ServerID:  e2eServer,
		Password:  e2ePasswd,
		Expires:   3600,
		Timeout:   3 * time.Second,
		Transport: "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}

	// First attempt: refused by the injected fault.
	if _, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg)); err != nil {
		t.Fatalf("Create(device A): %v", err)
	}
	deviceA, err := model.ParseNodeID(e2eDeviceA)
	if err != nil {
		t.Fatalf("ParseNodeID(A): %v", err)
	}
	if err := svc.Start(ctx, deviceA); err == nil {
		t.Fatal("Start(device A) succeeded although REGISTER was canned to 403")
	}
	nodeA, _ := svc.Get(ctx, deviceA)
	if nodeA.Status() != model.StatusFault {
		t.Errorf("device A status = %s, want fault", nodeA.Status())
	}

	// The refusal crossed the wire twice: request in, canned answer out.
	waitUntil(t, "the capture ring to hold the refused exchange", 5*time.Second, func() bool {
		return len(store.Query(platformID.String(), 100)) >= 2
	})

	// Lift the fault; a second device registers normally.
	if err := svc.ClearFault(ctx, platform.ID()); err != nil {
		t.Fatalf("ClearFault: %v", err)
	}
	if _, _, ok := svc.GetFault(ctx, platform.ID()); ok {
		t.Error("fault still present after ClearFault")
	}
	credB, err := model.NewCredentials(e2eDeviceB, e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewCredentials(B): %v", err)
	}
	if err := accounts.Add(platformID, credB); err != nil {
		t.Fatalf("accounts.Add(B): %v", err)
	}

	deviceB, err := svc.Create(ctx, deviceProfile(t, e2eDeviceB, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create(device B): %v", err)
	}
	if err := svc.Start(ctx, deviceB.ID()); err != nil {
		t.Fatalf("Start(device B): %v", err)
	}
	nodeB, _ := svc.Get(ctx, deviceB.ID())
	if nodeB.Status() != model.StatusOnline {
		t.Errorf("device B status = %s, want online", nodeB.Status())
	}
	waitUntil(t, "the platform to record device B", 10*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 1
	})

	// Every packet the platform saw — the refused exchange and the
	// successful four-way registration — belongs to the platform node.
	events := store.Query(platformID.String(), 100)
	if len(events) < 4 {
		t.Fatalf("capture events = %d, want >= 4", len(events))
	}
	directions := 0
	for i, evt := range events {
		if evt.NodeID != platformID.String() {
			t.Errorf("event[%d].NodeID = %q, want %q", i, evt.NodeID, platformID.String())
		}
		switch evt.Direction {
		case model.DirTransmit, model.DirReceive:
			directions++
		default:
			t.Errorf("event[%d].Direction = %q, want %q or %q", i, evt.Direction, model.DirTransmit, model.DirReceive)
		}
	}
	if directions == 0 {
		t.Error("no event carries a direction")
	}
	if len(faults.FaultCounters(platformID)) != 0 {
		t.Errorf("counters survived ClearFault: %v", faults.FaultCounters(platformID))
	}
}

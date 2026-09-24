package siptest_test

import (
	"context"
	"testing"
	"time"

	sipauth "github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/adapter/credstore"
	"github.com/your-org/gb28181-simulator/internal/adapter/devicereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/platform/clock"
)

// platformService wires the real pieces of both identities over real
// sockets: a device that registers and a platform that accepts it. Closing
// the returned function stops both sides' background work.
// The keeper is handed back as well, so a test can end the device side's
// background work — heartbeats and renewals — while the platform keeps
// serving, which is what a registration that lapses looks like.
func platformService(t *testing.T, ctx context.Context, accounts *credstore.Store, devices *devicereg.Registry) (
	*app.NodeService, *app.Keeper, func(),
) {
	t.Helper()
	registry := nodereg.New()
	factory := func(addr string) (port.SIPTransport, error) {
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
	// The device half: a client that answers a challenge.
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
	// The platform half: a server that challenges and checks.
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
	return svc, keeper, func() {
		_ = acceptor.Close()
		_ = keeper.Close()
	}
}

func platformProfile(t *testing.T, id, addr string) model.NodeProfile {
	t.Helper()
	profile, err := model.NewNodeProfile(id, addr, e2eDomain, "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	serving, err := model.DefaultPlatformServing(e2eDomain)
	if err != nil {
		t.Fatalf("DefaultPlatformServing: %v", err)
	}
	profile, err = profile.WithPlatformServing(serving)
	if err != nil {
		t.Fatalf("WithPlatformServing: %v", err)
	}
	return profile
}

// The whole point of the platform identity: a real device, built by this
// simulator, registers with a real platform built by the same simulator —
// challenge and all — and shows up in the platform's online table.
func TestPlatformAcceptsDeviceRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

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
	platformNode, _ := svc.Get(ctx, platform.ID())
	if platformNode.Status() != model.StatusOnline {
		t.Fatalf("platform status = %s, want online (serving)", platformNode.Status())
	}

	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:    platform.Profile().Addr(),
		ServerID:  e2eServer,
		Password:  e2ePasswd,
		Expires:   3600,
		Timeout:   5 * time.Second,
		Transport: "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	device, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create(device): %v", err)
	}
	if err := svc.Start(ctx, device.ID()); err != nil {
		t.Fatalf("Start(device): %v", err)
	}

	node, _ := svc.Get(ctx, device.ID())
	if node.Status() != model.StatusOnline {
		t.Fatalf("device status = %s, want online", node.Status())
	}
	waitUntil(t, "the platform to record the device", 10*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 1
	})
	row, ok := devices.Lookup(ctx, platformID, e2eDeviceA)
	if !ok {
		t.Fatal("the platform recorded nobody")
	}
	if row.GrantedExpiry() != 3600 {
		t.Errorf("granted = %d, want 3600", row.GrantedExpiry())
	}
	if row.Addr() == "" {
		t.Error("the row has no source address")
	}
	if row.Transport() != "udp" {
		t.Errorf("transport = %q, want udp", row.Transport())
	}
}

// Stopping the platform ends its serving: the table is cleared, because a
// platform that is not listening can no longer vouch for anyone.
func TestPlatformClearsDevicesWhenStopped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

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

	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:    platform.Profile().Addr(),
		ServerID:  e2eServer,
		Password:  e2ePasswd,
		Expires:   3600,
		Timeout:   5 * time.Second,
		Transport: "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	device, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create(device): %v", err)
	}
	if err := svc.Start(ctx, device.ID()); err != nil {
		t.Fatalf("Start(device): %v", err)
	}
	waitUntil(t, "the platform to record the device", 10*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 1
	})

	if err := svc.Stop(ctx, platform.ID()); err != nil {
		t.Fatalf("Stop(platform): %v", err)
	}
	if got := len(devices.List(ctx, platformID)); got != 0 {
		t.Errorf("table has %d rows after the platform stopped, want 0", got)
	}
	node, _ := svc.Get(ctx, platform.ID())
	if node.Status() != model.StatusOffline {
		t.Errorf("platform status = %s, want offline", node.Status())
	}
}

// A device the platform has no account for is refused: it does not come
// online and it does not appear in the table.
func TestPlatformRefusesUnknownDevice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	platformID, err := model.ParseNodeID(e2eServer)
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	platform, err := svc.Create(ctx, platformProfile(t, e2eServer, freeAddr(t)))
	if err != nil {
		t.Fatalf("Create(platform): %v", err)
	}
	if err := svc.Start(ctx, platform.ID()); err != nil {
		t.Fatalf("Start(platform): %v", err)
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
	device, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create(device): %v", err)
	}
	if err := svc.Start(ctx, device.ID()); err == nil {
		t.Fatal("Start(device) succeeded although the platform knows no such account")
	}
	if got := len(devices.List(ctx, platformID)); got != 0 {
		t.Errorf("table has %d rows, want 0", got)
	}
	node, _ := svc.Get(ctx, device.ID())
	if node.Status() != model.StatusFault {
		t.Errorf("device status = %s, want fault", node.Status())
	}
}

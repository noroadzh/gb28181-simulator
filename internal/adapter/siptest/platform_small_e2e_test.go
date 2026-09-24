package siptest_test

import (
	"context"
	"encoding/xml"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/credstore"
	"github.com/your-org/gb28181-simulator/internal/adapter/devicereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// e2eSmall is the platform-small in the middle of the cascade: a platform to
// the device below it, a subordinate to the platform above it.
const e2eSmall = "34020000002160000001"

// smallProfile is a platform-small entry: it serves its own downstreams
// (`platform:`) and registers with the platform above it (`registration:`).
func smallProfile(t *testing.T, id, addr string, reg model.Registration) model.NodeProfile {
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
	profile, err = profile.WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	return profile
}

// accountOn gives platformID an account for username, so it can challenge
// that subordinate.
func accountOn(t *testing.T, accounts *credstore.Store, platformID model.NodeID, username string) {
	t.Helper()
	cred, err := model.NewCredentials(username, e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if err := accounts.Add(platformID, cred); err != nil {
		t.Fatalf("accounts.Add: %v", err)
	}
}

// cascade is the three-level topology behind these tests: a device at the
// bottom, a platform-small in the middle, a platform-large at the top. Each
// platform holds an account for the level directly below it, and only for
// that level.
type cascade struct {
	large     model.NodeID
	small     model.NodeID
	device    model.NodeID
	largeAddr string
	smallAddr string
}

// startCascade builds the three levels over real sockets and waits until
// both platforms hold the node below them.
func startCascade(
	t *testing.T,
	ctx context.Context,
	svc *app.NodeService,
	accounts *credstore.Store,
	devices *devicereg.Registry,
) cascade {
	t.Helper()
	largeID, err := model.ParseNodeID(e2eServer)
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	smallID, err := model.ParseNodeID(e2eSmall)
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}

	// The platform above: it serves, and it knows the node in the middle.
	large, err := svc.Create(ctx, platformProfile(t, e2eServer, freeAddr(t)))
	if err != nil {
		t.Fatalf("Create(large): %v", err)
	}
	accountOn(t, accounts, largeID, e2eSmall)
	if err := svc.Start(ctx, large.ID()); err != nil {
		t.Fatalf("Start(large): %v", err)
	}

	// The platform in the middle: it serves the device below it and
	// registers with the platform above it, over one listener.
	up, err := model.NewRegistration(model.RegistrationParams{
		Server:            large.Profile().Addr(),
		ServerID:          e2eServer,
		Password:          e2ePasswd,
		Expires:           3600,
		Timeout:           8 * time.Second,
		HeartbeatInterval: time.Second,
		HeartbeatTimeout:  300 * time.Millisecond,
		Transport:         "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	small, err := svc.Create(ctx, smallProfile(t, e2eSmall, freeAddr(t), up))
	if err != nil {
		t.Fatalf("Create(small): %v", err)
	}
	accountOn(t, accounts, smallID, e2eDeviceA)
	if err := svc.Start(ctx, small.ID()); err != nil {
		t.Fatalf("Start(small): %v", err)
	}

	// The device at the bottom.
	down, err := model.NewRegistration(model.RegistrationParams{
		Server:            small.Profile().Addr(),
		ServerID:          e2eSmall,
		Password:          e2ePasswd,
		Expires:           3600,
		Timeout:           5 * time.Second,
		HeartbeatInterval: time.Second,
		HeartbeatTimeout:  300 * time.Millisecond,
		Transport:         "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	device, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), down))
	if err != nil {
		t.Fatalf("Create(device): %v", err)
	}
	if err := svc.Start(ctx, device.ID()); err != nil {
		t.Fatalf("Start(device): %v", err)
	}

	c := cascade{
		large:     largeID,
		small:     smallID,
		device:    device.ID(),
		largeAddr: large.Profile().Addr(),
		smallAddr: small.Profile().Addr(),
	}
	waitUntil(t, "the small platform to register with the large one", 20*time.Second, func() bool {
		return len(devices.List(ctx, largeID)) == 1
	})
	waitUntil(t, "the device to register with the small platform", 20*time.Second, func() bool {
		return len(devices.List(ctx, smallID)) == 1
	})
	return c
}

// A cascade of three: a device registers with a platform-small, which
// registers with a platform-large. The node in the middle is both halves at
// once, over one listener of its own.
func TestPlatformSmallRelaysThreeLevels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	c := startCascade(t, ctx, svc, accounts, devices)

	smallNode, _ := svc.Get(ctx, c.small)
	if smallNode.Status() != model.StatusOnline {
		t.Fatalf("small status = %s, want online", smallNode.Status())
	}
	if _, ok := devices.Lookup(ctx, c.large, e2eSmall); !ok {
		t.Errorf("the large platform holds nobody named %s", e2eSmall)
	}
	if _, ok := devices.Lookup(ctx, c.small, e2eDeviceA); !ok {
		t.Errorf("the small platform holds nobody named %s", e2eDeviceA)
	}
	// The large platform does not see through to the device below the
	// middle one: cross-level catalogue aggregation is not this change.
	if _, ok := devices.Lookup(ctx, c.large, e2eDeviceA); ok {
		t.Error("the large platform reports a device that is not its own downstream")
	}
}

// The platform above asks the platform in the middle for its catalogue the
// way a real one does — a MESSAGE written by hand — and gets the devices
// that registered with the middle one, and only those.
func TestPlatformSmallAnswersUpstreamCatalogQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	c := startCascade(t, ctx, svc, accounts, devices)

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	tr := siptransport.NewPortAdapter(client)

	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Notify><CmdType>Catalog</CmdType><SN>17</SN>` +
		`<DeviceID>` + e2eSmall + `</DeviceID></Notify>`
	query, err := model.NewRequest("MESSAGE", "sip:"+e2eSmall+"@"+e2eDomain, []model.Header{
		model.NewHeader("From", "<sip:"+e2eServer+"@"+e2eDomain+">;tag=upstream1"),
		model.NewHeader("To", "<sip:"+e2eSmall+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "upstream-catalog-1"),
		model.NewHeader("CSeq", "1 MESSAGE"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
		model.NewHeader("Max-Forwards", "70"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := tr.Send(ctx, query, c.smallAddr); err != nil {
		t.Fatalf("Send: %v", err)
	}

	resp, _, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	var answer catalogAnswer
	if err := xml.Unmarshal([]byte(resp.Body()), &answer); err != nil {
		t.Fatalf("the answer is not readable XML: %v\n%s", err, resp.Body())
	}
	if answer.SN != 17 {
		t.Errorf("sn = %d, want the one the query carried", answer.SN)
	}
	if answer.DeviceID != e2eSmall {
		t.Errorf("device id = %q, want the platform that answered", answer.DeviceID)
	}
	if answer.SumNum != 1 {
		t.Fatalf("sum = %d, want 1 — the device below the middle", answer.SumNum)
	}
	if item := answer.DeviceList.Items[0]; item.DeviceID != e2eDeviceA {
		t.Errorf("item = %s, want %s", item.DeviceID, e2eDeviceA)
	}
}

// An upstream that holds no account for the platform in the middle refuses
// it, and the refusal takes the whole node down: the half that was already
// serving is unwound rather than left running on its own.
func TestPlatformSmallFaultsWhenUpstreamRefusesIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	largeID, err := model.ParseNodeID(e2eServer)
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	large, err := svc.Create(ctx, platformProfile(t, e2eServer, freeAddr(t)))
	if err != nil {
		t.Fatalf("Create(large): %v", err)
	}
	// No account for the platform-small: the platform above refuses it,
	// as it refuses any subordinate it does not know.
	if err := svc.Start(ctx, large.ID()); err != nil {
		t.Fatalf("Start(large): %v", err)
	}

	smallID, err := model.ParseNodeID(e2eSmall)
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	up, err := model.NewRegistration(model.RegistrationParams{
		Server:            large.Profile().Addr(),
		ServerID:          e2eServer,
		Password:          e2ePasswd,
		Expires:           3600,
		Timeout:           5 * time.Second,
		HeartbeatInterval: time.Second,
		HeartbeatTimeout:  300 * time.Millisecond,
		Transport:         "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	small, err := svc.Create(ctx, smallProfile(t, e2eSmall, freeAddr(t), up))
	if err != nil {
		t.Fatalf("Create(small): %v", err)
	}
	smallAddr := small.Profile().Addr()
	if err := svc.Start(ctx, small.ID()); err == nil {
		t.Fatal("Start(small) succeeded although the platform above knows no such account")
	}
	smallNode, _ := svc.Get(ctx, smallID)
	if smallNode.Status() != model.StatusFault {
		t.Errorf("small status = %s, want fault", smallNode.Status())
	}
	if got := len(devices.List(ctx, smallID)); got != 0 {
		t.Errorf("the unwound platform still holds %d rows, want 0", got)
	}
	if got := len(devices.List(ctx, largeID)); got != 0 {
		t.Errorf("the platform above recorded %d rows for a node it refused", got)
	}

	// Nothing is left serving: a device that tries now finds nobody home.
	down, err := model.NewRegistration(model.RegistrationParams{
		Server:            smallAddr,
		ServerID:          e2eSmall,
		Password:          e2ePasswd,
		Expires:           3600,
		Timeout:           3 * time.Second,
		HeartbeatInterval: time.Second,
		HeartbeatTimeout:  300 * time.Millisecond,
		Transport:         "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	device, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), down))
	if err != nil {
		t.Fatalf("Create(device): %v", err)
	}
	if err := svc.Start(ctx, device.ID()); err == nil {
		t.Fatal("the device registered with a platform-small that is no longer serving")
	}
}

// Stopping the platform in the middle ends both of its halves: it withdraws
// from the platform above, forgets the devices below, and goes offline.
func TestPlatformSmallStopUnwindsBothHalves(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	c := startCascade(t, ctx, svc, accounts, devices)

	if err := svc.Stop(ctx, c.small); err != nil {
		t.Fatalf("Stop(small): %v", err)
	}
	smallNode, _ := svc.Get(ctx, c.small)
	if smallNode.Status() != model.StatusOffline {
		t.Errorf("small status = %s, want offline", smallNode.Status())
	}
	if got := len(devices.List(ctx, c.small)); got != 0 {
		t.Errorf("the stopped platform still holds %d rows, want 0", got)
	}
	// The platform above lets go of it too: by its goodbye, or by its own
	// sweeping should the goodbye never arrive.
	waitUntil(t, "the platform above to forget the platform in the middle", 20*time.Second, func() bool {
		return len(devices.List(ctx, c.large)) == 0
	})
}

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// platformFixture wires a NodeService with a real Acceptor over the test
// doubles, so "a platform comes online by serving" is exercised without a
// socket.
func platformFixture(t *testing.T, tr *acceptorTransport) (
	*NodeService, *fakeCatalogue, *fakeLifecycle, *fakeDevices, *Acceptor,
) {
	t.Helper()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	lc.transport = tr
	svc, err := NewNodeService(cat, lc, cat, func(string) (port.SIPTransport, error) {
		return tr, nil
	}, &fakeClock{now: time.Now()})
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	devices := newFakeDevices()
	acceptor, err := NewAcceptor(context.Background(), newSyncClock(time.Now()), &fakeChallenger{},
		&fakeAuthenticator{}, newFakeCredentials(), devices, discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	if _, err := svc.WithAcceptor(acceptor); err != nil {
		t.Fatalf("WithAcceptor: %v", err)
	}
	t.Cleanup(func() { _ = acceptor.Close() })
	return svc, cat, lc, devices, acceptor
}

func platformNodeProfile(t *testing.T, id string) model.NodeProfile {
	t.Helper()
	profile := testProfile(t, id, "127.0.0.1:15061")
	serving, err := model.DefaultPlatformServing("3402000000")
	if err != nil {
		t.Fatalf("DefaultPlatformServing: %v", err)
	}
	profile, err = profile.WithPlatformServing(serving)
	if err != nil {
		t.Fatalf("WithPlatformServing: %v", err)
	}
	return profile
}

// A platform comes online by serving: starting it binds the listener,
// launches the serving goroutine and advances the node.
func TestNodeService_StartPlatformServes(t *testing.T) {
	t.Parallel()
	tr := newAcceptorTransport()
	svc, cat, _, _, acceptor := platformFixture(t, tr)

	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(context.Background(), platformNodeProfile(t, testPlatformLarge)); err != nil {
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
}

// A platform without an acceptor keeps the old behaviour instead of
// pretending to serve: it stays where the lifecycle left it.
func TestNodeService_StartPlatformWithoutAcceptorStaysRegistering(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{}
	svc, cat, lc := registrationFixture(t, testPlatformLarge, tr, nil)
	lc.transport = tr

	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(context.Background(), platformNodeProfile(t, testPlatformLarge)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := statusOf(t, cat, id); got != model.StatusRegistering {
		t.Errorf("status = %s, want registering", got)
	}
	if len(tr.messages()) != 0 {
		t.Errorf("a platform with no acceptor sent %d messages, want 0", len(tr.messages()))
	}
}

// Stopping a platform ends its serving before the listener is released, and
// forgets the devices it had accepted.
func TestNodeService_StopPlatformEndsServing(t *testing.T) {
	t.Parallel()
	tr := newAcceptorTransport()
	svc, cat, lc, devices, acceptor := platformFixture(t, tr)

	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(context.Background(), platformNodeProfile(t, testPlatformLarge)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(context.Background(), id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Pretend a device registered while the platform was serving.
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060",
		Now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	if err := devices.Upsert(context.Background(), id, dev.WithGranted(3600, dev.RegisteredAt())); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := svc.Stop(context.Background(), id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if acceptor.Serving() != 0 {
		t.Errorf("Serving() = %d, want 0 after Stop", acceptor.Serving())
	}
	if got := len(devices.List(context.Background(), id)); got != 0 {
		t.Errorf("table has %d rows after Stop, want 0", got)
	}
	if got := statusOf(t, cat, id); got != model.StatusOffline {
		t.Errorf("status = %s, want offline", got)
	}
	if lc.isBound(id) {
		t.Error("listener still bound after Stop")
	}
}

// The device table is read through the service so HTTP never reaches into
// the use case: an unknown node is reported, and a node that never served
// simply has no devices.
func TestNodeService_Devices(t *testing.T) {
	t.Parallel()
	tr := newAcceptorTransport()
	svc, _, _, devices, _ := platformFixture(t, tr)
	ctx := context.Background()

	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(ctx, platformNodeProfile(t, testPlatformLarge)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rows, err := svc.Devices(ctx, id)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0 before anyone registers", len(rows))
	}

	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060",
		Now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	if err := devices.Upsert(ctx, id, dev); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if rows, err = svc.Devices(ctx, id); err != nil || len(rows) != 1 {
		t.Errorf("Devices = %d rows, err %v; want 1, nil", len(rows), err)
	}
	one, err := svc.Device(ctx, id, "34020000011310000001")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if one.DeviceID() != "34020000011310000001" {
		t.Errorf("device id = %q", one.DeviceID())
	}

	unknown := mustParse(t, "34020000002000000099")
	if _, err := svc.Devices(ctx, unknown); !errors.Is(err, model.ErrUnknownNode) {
		t.Errorf("Devices(unknown) = %v, want ErrUnknownNode", err)
	}
	if _, err := svc.Device(ctx, id, "34020000041310000099"); !errors.Is(err, model.ErrUnknownDevice) {
		t.Errorf("Device(unknown) = %v, want ErrUnknownDevice", err)
	}
}

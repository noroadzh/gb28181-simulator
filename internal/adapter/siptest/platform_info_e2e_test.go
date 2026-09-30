package siptest_test

import (
	"context"
	"sync"
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

// fakePlaybackPort records every invocation so the test can inspect it.
// Access is guarded by mu so the test is safe under -race: the acceptor
// writes from its serving goroutine, the test goroutine reads from its
// assertion block.
type fakePlaybackPort struct {
	mu      sync.Mutex
	plays   []playCall
	queries []string
}

type playCall struct {
	deviceID  string
	channelID string
	startTime string
	endTime   string
	scale     float64
}

func (f *fakePlaybackPort) Play(_ context.Context, deviceID, channelID, startTime, endTime string, scale float64) (string, error) {
	f.mu.Lock()
	f.plays = append(f.plays, playCall{deviceID, channelID, startTime, endTime, scale})
	f.mu.Unlock()
	return "session-1", nil
}
func (f *fakePlaybackPort) Stop(_ context.Context, _ string) error { return nil }
func (f *fakePlaybackPort) SetScale(_ context.Context, sessionID string, scale float64) error {
	f.mu.Lock()
	f.plays = append(f.plays, playCall{deviceID: sessionID, scale: scale})
	f.mu.Unlock()
	return nil
}
func (f *fakePlaybackPort) Query(_ context.Context, sessionID string) (port.PlaybackState, error) {
	f.mu.Lock()
	f.queries = append(f.queries, sessionID)
	f.mu.Unlock()
	return port.PlaybackState{SessionID: sessionID, Status: "playing"}, nil
}

// snapshot returns a copy of the current plays slice for assertion. Tests
// must use this instead of touching f.plays directly.
func (f *fakePlaybackPort) snapshot() []playCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]playCall, len(f.plays))
	copy(cp, f.plays)
	return cp
}

// platformServiceWithPlayback wires a full platform service (NodeService +
// Keeper + Acceptor) with a playback port attached, so the acceptor's INFO
// handler has a real downstream for MANSRTSP bodies (task 7.5).
func platformServiceWithPlayback(t *testing.T, ctx context.Context, accounts *credstore.Store, devices *devicereg.Registry,
	fp *fakePlaybackPort,
) (*app.NodeService, *app.Keeper, func()) {
	t.Helper()
	registry := nodereg.New()
	factory := func(addr string, _ model.NodeID) (port.SIPTransport, error) {
		tr, err := siptransport.New("udp://"+addr, siptransport.WithNodeID(""))
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
		t.Fatalf("NewAuthorizer: %v", err)
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
		t.Fatalf("NewAuthenticator: %v", err)
	}
	challenger, err := sipauth.NewChallengerAdapter(sipauth.NewChallenger(nil))
	if err != nil {
		t.Fatalf("NewChallenger: %v", err)
	}
	acceptor, err := app.NewAcceptor(ctx, clock.Real(), challenger, authenticator,
		accounts, devices, manscdp.NewMANSCDPCodec(), clock.RealTicker(), testLogger(t))
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	acceptor.WithPlayback(fp)
	if _, err := svc.WithAcceptor(acceptor); err != nil {
		t.Fatalf("WithAcceptor: %v", err)
	}
	return svc, keeper, func() {
		_ = acceptor.Close()
		_ = keeper.Close()
	}
}

// TestPlatformAnswersInfoPlay exercises the INFO → MANSRTSP PLAY path
// (task 7.5): a device sends an INFO carrying a PLAY body to its platform;
// the platform must answer 200 OK and forward the command to the playback
// port.
func TestPlatformAnswersInfoPlay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	fp := &fakePlaybackPort{}
	svc, _, stop := platformServiceWithPlayback(t, ctx, accounts, devices, fp)
	defer stop()

	platform := platformProfile(t, e2eServer, freeAddr(t))
	platformNode := startPlatform(t, ctx, svc, accounts, platform)
	startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 3600, 5*time.Second))

	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		platformID := platformNode.ID()
		_, ok := devices.Lookup(ctx, platformID, e2eDeviceA)
		return ok
	})

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	tr := siptransport.NewPortAdapter(client)

	body := `PLAY RTSP/1.0` + "\r\n" +
		`CSeq: 1` + "\r\n" +
		`Scale: 1.0` + "\r\n" +
		`Range: npt=0-` + "\r\n"
	info, err := model.NewRequest("INFO", "sip:"+e2eDeviceA+"@"+e2eDomain, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=info1"),
		model.NewHeader("To", "<sip:"+e2eServer+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "info-play-1"),
		model.NewHeader("CSeq", "1 INFO"),
		model.NewHeader("Content-Type", "Application/MANSRTSP"),
		model.NewHeader("Max-Forwards", "70"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := tr.Send(ctx, info, platformNode.Profile().Addr()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	resp, _, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200\n%s", resp.StatusCode(), resp.Body())
	}
	if callID, ok := resp.Header("Call-ID"); !ok || callID.Value() != "info-play-1" {
		t.Errorf("Call-ID = %v, want info-play-1", callID)
	}
	if got := len(fp.snapshot()); got != 1 {
		t.Fatalf("playback.Play called %d times, want 1", got)
	}
	call := fp.snapshot()[0]
	if call.deviceID != e2eDeviceA {
		t.Errorf("deviceID = %q, want %s", call.deviceID, e2eDeviceA)
	}
	if call.channelID != "" {
		t.Errorf("channelID = %q, want empty", call.channelID)
	}
	if call.startTime != "0" {
		t.Errorf("startTime = %q, want 0 (npt=0 in MANSRTSP)", call.startTime)
	}
	if call.scale != 1.0 {
		t.Errorf("scale = %f, want 1.0", call.scale)
	}
}

// TestPlatformAnswersInfoPause exercises the INFO → MANSRTSP PAUSE path:
// the platform must answer 200 OK and forward the pause to the playback
// port with scale=0 (task 7.5).
func TestPlatformAnswersInfoPause(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	fp := &fakePlaybackPort{}
	svc, _, stop := platformServiceWithPlayback(t, ctx, accounts, devices, fp)
	defer stop()

	platform := platformProfile(t, e2eServer, freeAddr(t))
	platformNode := startPlatform(t, ctx, svc, accounts, platform)
	startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 3600, 5*time.Second))

	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		platformID := platformNode.ID()
		_, ok := devices.Lookup(ctx, platformID, e2eDeviceA)
		return ok
	})

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	tr := siptransport.NewPortAdapter(client)

	body := `PAUSE RTSP/1.0` + "\r\n" +
		`CSeq: 1` + "\r\n" +
		`Scale: 0` + "\r\n"
	info, err := model.NewRequest("INFO", "sip:"+e2eDeviceA+"@"+e2eDomain, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=info2"),
		model.NewHeader("To", "<sip:"+e2eServer+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "info-pause-1"),
		model.NewHeader("CSeq", "1 INFO"),
		model.NewHeader("Content-Type", "Application/MANSRTSP"),
		model.NewHeader("Max-Forwards", "70"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := tr.Send(ctx, info, platformNode.Profile().Addr()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	resp, _, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200\n%s", resp.StatusCode(), resp.Body())
	}
	if callID, ok := resp.Header("Call-ID"); !ok || callID.Value() != "info-pause-1" {
		t.Errorf("Call-ID = %v, want info-pause-1", callID)
	}
	if got := len(fp.snapshot()); got != 1 {
		t.Fatalf("playback.SetScale called %d times, want 1", got)
	}
	if fp.snapshot()[0].scale != 0 {
		t.Errorf("scale = %f, want 0 (paused)", fp.snapshot()[0].scale)
	}
	if fp.snapshot()[0].deviceID != "info-pause-1" {
		t.Errorf("sessionID = %q, want info-pause-1 (Call-ID)", fp.snapshot()[0].deviceID)
	}
}

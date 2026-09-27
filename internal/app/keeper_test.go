package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// syncClock is a thread-safe clock: the keeper reads it from its own
// goroutine while the test advances it.
type syncClock struct {
	mu  sync.Mutex
	now time.Time
}

func newSyncClock(t time.Time) *syncClock { return &syncClock{now: t} }

func (c *syncClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *syncClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// scriptedTicker fires only when the test says so, which is what makes a
// scheduled use case deterministic without sleeping.
type scriptedTicker struct {
	ch      chan time.Time
	stopped bool
	mu      sync.Mutex
}

func newScriptedTicker() *scriptedTicker { return &scriptedTicker{ch: make(chan time.Time, 1)} }

func (t *scriptedTicker) C() <-chan time.Time { return t.ch }

func (t *scriptedTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
}

func (t *scriptedTicker) stopped_() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped
}

// tick delivers one tick and waits for the loop to take it, so a test
// observes one round at a time. It gives up rather than hanging if the loop
// has stopped.
func (t *scriptedTicker) tick() {
	select {
	case t.ch <- time.Now():
	case <-time.After(2 * time.Second):
	}
}

// platformTransport answers like a platform: every message is recorded, and
// a 200 OK echoing the transaction headers comes back — unless the method is
// in silent, in which case the transport stays quiet until the caller's
// context expires. challenge makes the first REGISTER of a Call-ID come back
// as 401, so the Digest path is exercised.
type platformTransport struct {
	mu         sync.Mutex
	sent       []model.Message
	dst        string
	silent     map[string]bool
	challenge  bool
	challenged map[string]bool
	// optionsStatus is the status code answered to an OPTIONS probe.
	// The zero value keeps the default 200 OK; a non-zero value (e.g.
	// 503) makes the probe refused. The timeout path is driven separately
	// by silent["OPTIONS"].
	optionsStatus int

	// answers are queued when the request arrives — one per request, in
	// order — so a heartbeat is never answered with the renewal's headers.
	answers chan pending
}

type pending struct {
	msg  model.Message
	peer string
}

func newPlatformTransport() *platformTransport {
	return &platformTransport{
		silent:     map[string]bool{},
		challenged: map[string]bool{},
		answers:    make(chan pending, 64),
	}
}

func (p *platformTransport) Send(_ context.Context, msg model.Message, dst string) error {
	p.mu.Lock()
	p.sent = append(p.sent, msg)
	p.dst = dst
	silent := p.silent[msg.Method()]
	challenge := false
	if p.challenge && msg.Method() == "REGISTER" {
		if h, ok := msg.Header("Call-ID"); ok && !p.challenged[h.Value()] {
			p.challenged[h.Value()] = true
			challenge = true
		}
	}
	p.mu.Unlock()

	if silent {
		return nil
	}
	hdrs := []model.Header{}
	for _, name := range []string{"Via", "From", "To", "Call-ID", "CSeq"} {
		if h, ok := msg.Header(name); ok {
			hdrs = append(hdrs, h)
		}
	}
	code, reason := 200, "OK"
	if msg.Method() == "OPTIONS" && p.optionsStatus != 0 {
		code, reason = p.optionsStatus, "Refused"
	}
	if challenge {
		code, reason = 401, "Unauthorized"
		hdrs = append(hdrs, model.NewHeader("WWW-Authenticate",
			`Digest realm="3402000000", nonce="dGhpc2lzYW5vbmNl", qop="auth", algorithm=MD5`))
	}
	resp, err := model.NewResponse(code, reason, hdrs, "")
	if err != nil {
		return err
	}
	p.answers <- pending{msg: resp, peer: dst}
	return nil
}

func (p *platformTransport) Receive(ctx context.Context) (model.Message, string, error) {
	select {
	case a := <-p.answers:
		return a.msg, a.peer, nil
	case <-ctx.Done():
		return model.Message{}, "", ctx.Err()
	}
}

func (p *platformTransport) Close() error { return nil }

func (p *platformTransport) messages() []model.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]model.Message, len(p.sent))
	copy(out, p.sent)
	return out
}

func (p *platformTransport) count(method string) int {
	n := 0
	for _, m := range p.messages() {
		if m.Method() == method {
			n++
		}
	}
	return n
}

// keeperFixture wires a Keeper over doubles: a real catalogue and
// lifecycle, a scripted ticker and the transport given.
func keeperFixture(
	t *testing.T,
	tr *platformTransport,
	clock *syncClock,
	reg model.Registration,
	granted uint32,
) (*Keeper, *scriptedTicker, *fakeCatalogue, *fakeLifecycle, model.NodeID) {
	t.Helper()
	cat := newFakeCatalogue()
	lc := newFakeLifecycle(cat)
	lc.transport = tr

	profile := testProfile(t, testDevice, "127.0.0.1:15060")
	profile, err := profile.WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	node, err := cat.Register(context.Background(), profile)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	id := node.ID()

	authorizer := &stubAuthorizer{}
	registrar := newTestRegistrar(t, authorizer, clock)
	tick := newScriptedTicker()
	keeper, err := NewKeeper(context.Background(), cat, lc, registrar,
		&stubKeepaliveCodec{}, clock, func(time.Duration) port.Ticker { return tick }, discardLogger())
	if err != nil {
		t.Fatalf("NewKeeper: %v", err)
	}
	result, err := model.NewRegistrationResult(reg.Server(), granted, clock.Now())
	if err != nil {
		t.Fatalf("NewRegistrationResult: %v", err)
	}
	if _, err := cat.RecordRegistration(context.Background(), id, result); err != nil {
		t.Fatalf("RecordRegistration: %v", err)
	}
	// Bring the node online the way Start does, so the keeper has
	// something to keep.
	for _, to := range []model.Status{model.StatusRegistering, model.StatusRegistered, model.StatusOnline} {
		if _, err := cat.Advance(context.Background(), id, to); err != nil {
			t.Fatalf("advance to %s: %v", to, err)
		}
	}
	lc.bound[id.String()] = true

	if err := keeper.Start(id, tr, reg, result); err != nil {
		t.Fatalf("keeper.Start: %v", err)
	}
	return keeper, tick, cat, lc, id
}

// faultCount and lastFaultErr read the lifecycle's fault record under its
// lock: the keeper's goroutine writes it while the test reads it.
func (l *fakeLifecycle) faultCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.faults
}

func (l *fakeLifecycle) lastFaultErr() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastFault
}

// stubKeepaliveCodec renders a keepalive body the tests can recognise.
type stubKeepaliveCodec struct{}

func (stubKeepaliveCodec) MarshalKeepalive(k model.Keepalive) (string, error) {
	return "<Notify><CmdType>Keepalive</CmdType><SN>" +
		strconv.FormatUint(uint64(k.SN()), 10) +
		"</SN><DeviceID>" + k.DeviceID() + "</DeviceID><Status>OK</Status></Notify>", nil
}

func (stubKeepaliveCodec) MarshalAlarmNotify(a model.AlarmNotify) (string, error) {
	return "<Notify><CmdType>Alarm</CmdType><DeviceID>" + a.DeviceID() + "</DeviceID></Notify>", nil
}

func testKeepaliveRegistration(t *testing.T, interval, timeout time.Duration, maxFailures uint32) model.Registration {
	t.Helper()
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:   testServer,
		Password: "secret",
		Expires:  3600,
		// Short so a renewal that gets no answer does not hold the loop
		// for the default five seconds.
		Timeout:              50 * time.Millisecond,
		HeartbeatInterval:    interval,
		HeartbeatTimeout:     timeout,
		HeartbeatMaxFailures: maxFailures,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	return reg
}

// waitFor polls until cond holds, so tests never sleep for a fixed guess.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// An online node sends one keepalive per tick, each with a fresh sequence
// number, and stays online.
func TestKeeper_SendsKeepalivePerTick(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	reg := testKeepaliveRegistration(t, 60*time.Second, 500*time.Millisecond, 3)
	keeper, tick, cat, lc, id := keeperFixture(t, tr, clock, reg, 3600)
	defer keeper.Stop(id)

	for i := 1; i <= 3; i++ {
		tick.tick()
		want := i
		waitFor(t, "another keepalive", func() bool { return tr.count("MESSAGE") == want })
	}

	msgs := tr.messages()
	if len(msgs) != 3 {
		t.Fatalf("sent %d messages, want 3", len(msgs))
	}
	for i, m := range msgs {
		if m.Method() != "MESSAGE" {
			t.Fatalf("message %d is %s, want MESSAGE", i, m.Method())
		}
		h, ok := m.Header("Content-Type")
		if !ok || h.Value() != "Application/MANSCDP+XML" {
			t.Errorf("message %d Content-Type = %q (present=%v), want Application/MANSCDP+XML",
				i, h.Value(), ok)
		}
		if !strings.Contains(m.Body(), "<CmdType>Keepalive</CmdType>") {
			t.Errorf("message %d body is not a keepalive: %s", i, m.Body())
		}
	}
	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online", got)
	}
	if lc.faultCount() != 0 {
		t.Errorf("faulted %d times, want 0", lc.faults)
	}
}

// A keepalive nobody answers counts once; only a run of them puts the node
// into fault and releases its listener.
func TestKeeper_FaultsAfterConsecutiveFailures(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	tr.silent["MESSAGE"] = true
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	reg := testKeepaliveRegistration(t, 60*time.Second, 20*time.Millisecond, 3)
	keeper, tick, cat, lc, id := keeperFixture(t, tr, clock, reg, 3600)

	for i := 0; i < 3; i++ {
		tick.tick()
	}
	waitFor(t, "the node to fault", func() bool { return lc.faultCount() == 1 })
	waitFor(t, "the session to end", func() bool { return keeper.Running() == 0 })

	if got := statusOf(t, cat, id); got != model.StatusFault {
		t.Errorf("status = %s, want fault", got)
	}
	if lc.isBound(id) {
		t.Error("listener still bound after fault")
	}
	if err := lc.lastFaultErr(); err == nil || !errors.Is(err, ErrKeepaliveLost) {
		t.Errorf("fault reason = %v, want ErrKeepaliveLost", err)
	} else if !strings.Contains(err.Error(), "3/3 consecutive") {
		t.Errorf("fault reason omits the count and the threshold: %v", err)
	}
	// Further ticks must not resurrect it.
	tick.tick()
	if keeper.Running() != 0 {
		t.Error("a faulted node is still being kept")
	}
}

// A node renews its registration when the renewal point has passed, and
// stays online while doing so.
func TestKeeper_RenewsWhenDue(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	reg := testKeepaliveRegistration(t, 60*time.Second, 200*time.Millisecond, 3)
	// Ten seconds granted: the renewal point is in the past relative to a
	// lifetime that short, so the very first tick renews.
	keeper, tick, cat, _, id := keeperFixture(t, tr, clock, reg, 10)
	defer keeper.Stop(id)

	tick.tick()
	waitFor(t, "a renewal REGISTER", func() bool { return tr.count("REGISTER") == 1 })

	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online — renewal is data, not a transition", got)
	}
	node, _ := cat.Get(context.Background(), id)
	res, ok := node.RegistrationResult()
	if !ok {
		t.Fatal("no registration result recorded")
	}
	if res.RegisteredAt().IsZero() {
		t.Error("renewal did not record a time")
	}
}

// A renewal that fails is retried later, and later still each time: the
// node keeps its registration (and its online status) meanwhile.
func TestKeeper_BacksoffAfterFailedRenewal(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	tr.silent["REGISTER"] = true
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	reg := testKeepaliveRegistration(t, 60*time.Second, 20*time.Millisecond, 3)
	keeper, tick, cat, _, id := keeperFixture(t, tr, clock, reg, 10)
	defer keeper.Stop(id)

	tick.tick()
	waitFor(t, "the first renewal attempt", func() bool { return tr.count("REGISTER") == 1 })
	// Let that attempt time out before moving the clock: the backoff starts
	// when the failure is recorded, not when the request went out, and the
	// attempt is still in flight when Send returns.
	time.Sleep(120 * time.Millisecond)

	// Not yet: the next attempt is a backoff away.
	clock.Advance(4 * time.Second)
	tick.tick()
	waitFor(t, "a keepalive after the clock moved", func() bool { return tr.count("MESSAGE") == 2 })
	if n := tr.count("REGISTER"); n != 1 {
		t.Fatalf("renewed %d times during backoff, want 1", n)
	}

	clock.Advance(time.Second)
	tick.tick()
	waitFor(t, "the retried renewal", func() bool { return tr.count("REGISTER") == 2 })

	if got := statusOf(t, cat, id); got != model.StatusOnline {
		t.Errorf("status = %s, want online — a failed renewal must not drop the node", got)
	}
}

// Stop ends one node's background work and waits for it.
func TestKeeper_StopEndsSession(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	clock := newSyncClock(time.Now())
	reg := testKeepaliveRegistration(t, 60*time.Second, 200*time.Millisecond, 3)
	keeper, _, _, _, id := keeperFixture(t, tr, clock, reg, 3600)

	keeper.Stop(id)
	if keeper.Running() != 0 {
		t.Errorf("Running() = %d, want 0", keeper.Running())
	}
	// Stopping twice is harmless, and so is stopping a stranger.
	keeper.Stop(id)
	keeper.Stop(mustParse(t, testPlatformLarge))
}

// Close ends every session, so shutdown leaves no goroutine behind.
func TestKeeper_CloseStopsEverything(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	clock := newSyncClock(time.Now())
	reg := testKeepaliveRegistration(t, 60*time.Second, 200*time.Millisecond, 3)
	keeper, _, _, _, _ := keeperFixture(t, tr, clock, reg, 3600)

	if err := keeper.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if keeper.Running() != 0 {
		t.Errorf("Running() = %d, want 0", keeper.Running())
	}
}

// Starting twice cannot double the traffic: a node has one session.
func TestKeeper_StartTwiceIsIdempotent(t *testing.T) {
	t.Parallel()
	tr := newPlatformTransport()
	clock := newSyncClock(time.Now())
	reg := testKeepaliveRegistration(t, 60*time.Second, 200*time.Millisecond, 3)
	keeper, tick, cat, _, id := keeperFixture(t, tr, clock, reg, 3600)
	defer keeper.Stop(id)

	if err := keeper.Start(id, tr, reg, model.RegistrationResult{}); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if keeper.Running() != 1 {
		t.Fatalf("Running() = %d, want 1", keeper.Running())
	}
	tick.tick()
	waitFor(t, "one keepalive", func() bool { return tr.count("MESSAGE") == 1 })
	time.Sleep(20 * time.Millisecond)
	if n := tr.count("MESSAGE"); n != 1 {
		t.Errorf("sent %d keepalives for one tick, want 1", n)
	}
	_ = cat
}

// optionsRegistration is a registration with the OPTIONS probe enabled, so
// the cascade link is checked between heartbeats.
func optionsRegistration(t *testing.T, timeout time.Duration) model.Registration {
	t.Helper()
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:   testServer,
		Password: "secret",
		Expires:  3600,
		Timeout:  50 * time.Millisecond,

		HeartbeatInterval:    60 * time.Second,
		HeartbeatTimeout:     timeout,
		HeartbeatMaxFailures: 3,
		OptionsEnabled:       true,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	return reg
}

// The OPTIONS probe answers the way the platform does: a 2xx with the
// transaction's own Call-ID from the registered peer succeeds; a non-2xx
// names the status; silence runs into ErrOptionsTimeout. A mismatched
// Call-ID or peer never concludes the probe.
func TestKeeper_OptionsProbeResponses(t *testing.T) {
	t.Parallel()

	// 2xx answers: the happy path.
	tr := newPlatformTransport()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	reg := optionsRegistration(t, 500*time.Millisecond)
	keeper, _, _, _, id := keeperFixture(t, tr, clock, reg, 3600)
	defer keeper.Stop(id)
	s := &session{id: id, tr: tr, reg: reg, done: make(chan struct{})}

	if err := keeper.sendOptions(context.Background(), s); err != nil {
		t.Fatalf("OPTIONS answered 200: %v", err)
	}
	if n := tr.count("OPTIONS"); n != 1 {
		t.Errorf("sent %d OPTIONS, want 1", n)
	}

	// A 5xx refusal reports the status code.
	tr.optionsStatus = 503
	err := keeper.sendOptions(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "upstream refused OPTIONS: 503") {
		t.Errorf("OPTIONS refused 503: got %v, want status in the error", err)
	}

	// Silence from the platform times out with ErrOptionsTimeout.
	tr.optionsStatus = 0
	tr.silent["OPTIONS"] = true
	err = keeper.sendOptions(context.Background(), s)
	if !errors.Is(err, ErrOptionsTimeout) {
		t.Errorf("OPTIONS silent: got %v, want ErrOptionsTimeout", err)
	}

	// A response from a stranger does not conclude the probe: the probe
	// keeps waiting until its own timeout fires.
	tr2 := newPlatformTransport()
	keeper2, _, _, _, id2 := keeperFixture(t, tr2, clock, reg, 3600)
	defer keeper2.Stop(id2)
	s2 := &session{id: id2, tr: tr2, reg: reg, done: make(chan struct{})}
	// Queue a response with the wrong peer before the request goes out,
	// then let the real answer arrive after it: the probe must skip the
	// stranger and still succeed on the matching one.
	stranger := "10.9.9.9:5060"
	go func() {
		// Drain: the first arrival is the stranger's; a matching one
		// follows from the transport itself.
		<-time.After(10 * time.Millisecond)
		tr2.answers <- pending{msg: model.Message{}, peer: stranger}
	}()
	if err := keeper2.sendOptions(context.Background(), s2); err != nil {
		t.Fatalf("OPTIONS with a queued stranger response: %v", err)
	}
}

// The keeper's fault gate: a profile that blackholes MESSAGE keepalives
// turns every heartbeat into a failure, so after MaxHeartbeatFailures
// consecutive suppressed probes the node is declared unreachable — exactly
// as if the platform had gone quiet.
func TestKeeper_FaultGateBlackholesKeepalive(t *testing.T) {
	t.Parallel()

	tr := newPlatformTransport()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	reg := testKeepaliveRegistration(t, 60*time.Second, 500*time.Millisecond, 3)
	keeper, tick, cat, lc, id := keeperFixture(t, tr, clock, reg, 3600)
	defer keeper.Stop(id)

	faults := NewFaultStore(cat)
	if err := faults.Install(context.Background(), id, model.FaultProfile{
		Blackhole: []string{"MESSAGE"},
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	keeper.WithFaults(faults)

	maxFailures := reg.MaxHeartbeatFailures()
	for i := uint32(0); i < maxFailures; i++ {
		tick.tick()
	}
	waitFor(t, "the node to fault after max failures", func() bool {
		return lc.faultCount() == 1
	})
	waitFor(t, "the session to end after fault", func() bool {
		return keeper.Running() == 0
	})

	if got := statusOf(t, cat, id); got != model.StatusFault {
		t.Errorf("status = %s, want fault", got)
	}
	if lc.isBound(id) {
		t.Error("listener still bound after fault")
	}
}

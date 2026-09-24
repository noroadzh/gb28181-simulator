package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// fakeMANSCDP stands in for the codec: it hands back whatever notify the
// test scripted, and it records every catalog it was asked to render, which
// is what the assertions read. The body it renders is a readable stand-in —
// the real bytes are the codec's own golden tests' business.
type fakeMANSCDP struct {
	mu         sync.Mutex
	notify     model.Notify
	decodeErr  error
	bodies     []string
	catalogs   []model.Catalog
	marshalErr error
}

func newFakeMANSCDP() *fakeMANSCDP { return &fakeMANSCDP{} }

func (f *fakeMANSCDP) DecodeNotify(body string) (model.Notify, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies = append(f.bodies, body)
	return f.notify, f.decodeErr
}

func (f *fakeMANSCDP) MarshalCatalog(catalog model.Catalog) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogs = append(f.catalogs, catalog)
	if f.marshalErr != nil {
		return "", f.marshalErr
	}
	var b strings.Builder
	fmt.Fprintf(&b, "catalog sn=%d sum=%d", catalog.SN(), catalog.SumNum())
	for _, it := range catalog.Items() {
		b.WriteString(" " + it.DeviceID())
	}
	return b.String(), nil
}

func (f *fakeMANSCDP) rendered() []model.Catalog {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Catalog, len(f.catalogs))
	copy(out, f.catalogs)
	return out
}

func (f *fakeMANSCDP) setNotify(n model.Notify) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notify = n
}

// tickerScript hands the sweeper a ticker per node and keeps them, so a test
// can fire a sweep by hand instead of sleeping through one.
type tickerScript struct {
	mu      sync.Mutex
	tickers []*scriptedTicker
}

func newTickerScript() *tickerScript { return &tickerScript{} }

func (s *tickerScript) factory() port.TickerFactory {
	return func(time.Duration) port.Ticker {
		s.mu.Lock()
		defer s.mu.Unlock()
		t := newScriptedTicker()
		s.tickers = append(s.tickers, t)
		return t
	}
}

// count is how many tickers were handed out, which is also how many nodes
// are being swept.
func (s *tickerScript) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tickers)
}

func (s *tickerScript) tick() {
	s.mu.Lock()
	tickers := make([]*scriptedTicker, len(s.tickers))
	copy(tickers, s.tickers)
	s.mu.Unlock()
	for _, t := range tickers {
		t.tick()
	}
}

// waitForTickers waits until n tickers were handed out: the sweeper starts
// in the serving goroutine, so ticking before it exists would be a no-op
// that passes for the wrong reason.
func (s *tickerScript) waitForTickers(tb testing.TB, n int) {
	tb.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.count() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	tb.Fatalf("waited for %d sweepers, got %d", n, s.count())
}

// messageHarness is a platform serving one node, with every collaborator a
// fake and the clock and the sweep beat under the test's control.
type messageHarness struct {
	acceptor *Acceptor
	tr       *acceptorTransport
	creds    *fakeCredentials
	devices  *fakeDevices
	clock    *syncClock
	codec    *fakeMANSCDP
	tickers  *tickerScript
	nodeID   model.NodeID
}

func newMessageHarness(t *testing.T, policy model.ExpiresPolicy) *messageHarness {
	t.Helper()
	nodeID := mustPlatformNode(t)
	tr := newAcceptorTransport()
	creds := newFakeCredentials()
	devices := newFakeDevices()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	codec := newFakeMANSCDP()
	tickers := newTickerScript()
	acceptor, err := NewAcceptor(context.Background(), clock, &fakeChallenger{},
		&fakeAuthenticator{}, creds, devices, codec, tickers.factory(), discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	if err := acceptor.Serve(nodeID, tr, "3402000000", policy); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = acceptor.Close() })
	return &messageHarness{
		acceptor: acceptor,
		tr:       tr,
		creds:    creds,
		devices:  devices,
		clock:    clock,
		codec:    codec,
		tickers:  tickers,
		nodeID:   nodeID,
	}
}

// register puts a device in the online table the honest way: an
// authenticated REGISTER that is granted expires seconds.
func (h *messageHarness) register(t *testing.T, deviceID string, expires string) model.DownstreamDevice {
	t.Helper()
	h.creds.add(t, h.nodeID, deviceID, "secret")
	req := registerRequest(t, deviceID,
		model.NewHeader("Authorization", `Digest username="`+deviceID+`", response="ok"`),
		model.NewHeader("Expires", expires))
	if resp := h.tr.deliver(t, req); resp.StatusCode() != 200 {
		t.Fatalf("register %s: status = %d, want 200", deviceID, resp.StatusCode())
	}
	dev, ok := h.devices.Lookup(context.Background(), h.nodeID, deviceID)
	if !ok {
		t.Fatalf("register %s: no row in the online table", deviceID)
	}
	return dev
}

func messageRequest(t *testing.T, deviceID, body string) model.Message {
	t.Helper()
	msg, err := model.NewRequest("MESSAGE", "sip:"+deviceID+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=msg1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", "call-2"),
		model.NewHeader("CSeq", "2 MESSAGE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-2"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

func mustNotify(t *testing.T, cmdType, deviceID string, sn uint32) model.Notify {
	t.Helper()
	n, err := model.NewNotify(cmdType, deviceID, sn, "")
	if err != nil {
		t.Fatalf("NewNotify: %v", err)
	}
	return n
}

// silence delivers a request that must not be answered and waits long enough
// for an answer that never comes. Asserting on an absence needs a moment of
// patience, or it would pass before the loop had even read the message.
func (h *messageHarness) silence(t *testing.T, req model.Message) {
	t.Helper()
	before := h.tr.answers()
	h.tr.inbound <- req
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if h.tr.answers() > before {
			t.Fatalf("an answer was sent for a message the platform must ignore: %d",
				h.tr.answers())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func defaultHarnessPolicy(t *testing.T) model.ExpiresPolicy {
	t.Helper()
	policy, err := model.NewExpiresPolicy(60, 3600, 7200)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	return policy
}

// A keepalive is evidence that a device is there, not a request for more
// time: it moves last-seen and leaves the granted lifetime alone.
func TestAcceptor_KeepaliveRefreshesWithoutExtending(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	dev := h.register(t, "34020000011310000001", "3600")
	grantedAt := dev.ExpiresAt()

	h.clock.Advance(30 * time.Second)
	h.codec.setNotify(mustNotify(t, model.CmdTypeKeepalive, "34020000011310000001", 7))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Notify/>"))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	after, ok := h.devices.Lookup(context.Background(), h.nodeID, "34020000011310000001")
	if !ok {
		t.Fatal("a keepalive dropped its device from the table")
	}
	if !after.LastSeenAt().After(dev.LastSeenAt()) {
		t.Errorf("last seen = %s, want it moved forward from %s", after.LastSeenAt(), dev.LastSeenAt())
	}
	if !after.ExpiresAt().Equal(grantedAt) {
		t.Errorf("expires at = %s, want it untouched at %s", after.ExpiresAt(), grantedAt)
	}
	if after.GrantedExpiry() != dev.GrantedExpiry() {
		t.Errorf("granted = %d, want %d", after.GrantedExpiry(), dev.GrantedExpiry())
	}
	if got := h.devices.List(context.Background(), h.nodeID); len(got) != 1 {
		t.Errorf("table has %d rows, want 1", len(got))
	}
}

// A platform that answers "OK" to a device it never granted anything to
// would be claiming a registration it does not hold.
func TestAcceptor_KeepaliveFromStrangerIsIgnored(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	dev := h.register(t, "34020000011310000001", "3600")

	h.codec.setNotify(mustNotify(t, model.CmdTypeKeepalive, "34020000011310000009", 7))
	h.silence(t, messageRequest(t, "34020000011310000009", "<Notify/>"))

	after, ok := h.devices.Lookup(context.Background(), h.nodeID, "34020000011310000001")
	if !ok {
		t.Fatal("a stranger's keepalive disturbed a registered device")
	}
	if !after.LastSeenAt().Equal(dev.LastSeenAt()) {
		t.Error("a stranger's keepalive moved another device's last-seen time")
	}
	if _, ok := h.devices.Lookup(context.Background(), h.nodeID, "34020000011310000009"); ok {
		t.Error("a stranger's keepalive put it in the online table")
	}
}

// A catalog query is answered with the table of the node it arrived at,
// ordered and counted, echoing the sequence number it asked with.
func TestAcceptor_AnswersCatalogQuery(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000003", "3600")
	h.register(t, "34020000011310000001", "3600")
	h.register(t, "34020000011310000002", "3600")

	h.codec.setNotify(mustNotify(t, model.CmdTypeCatalog, "34020000002000000001", 42))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "Content-Type"); got != "Application/MANSCDP+XML" {
		t.Errorf("Content-Type = %q, want a MANSCDP body announced", got)
	}
	rendered := h.codec.rendered()
	if len(rendered) != 1 {
		t.Fatalf("rendered %d catalogs, want 1", len(rendered))
	}
	got := rendered[0]
	if got.SN() != 42 {
		t.Errorf("sn = %d, want the one the query carried", got.SN())
	}
	if got.DeviceID() != "34020000002000000001" {
		t.Errorf("device id = %q, want the platform answering", got.DeviceID())
	}
	if got.SumNum() != 3 {
		t.Fatalf("sum = %d, want 3", got.SumNum())
	}
	ids := make([]string, 0, len(got.Items()))
	for _, it := range got.Items() {
		ids = append(ids, it.DeviceID())
		if it.Status() != model.CatalogStatusON {
			t.Errorf("%s status = %q, want ON", it.DeviceID(), it.Status())
		}
		if it.Address() == "" {
			t.Errorf("%s has no address in the catalog", it.DeviceID())
		}
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("catalog items are not ordered by device id: %v", ids)
	}
	if resp.Body() == "" {
		t.Error("the answer carries no body")
	}
}

// An empty table is a legitimate answer, not an error: the platform is
// there and has nobody online.
func TestAcceptor_AnswersCatalogQueryWithEmptyTable(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.codec.setNotify(mustNotify(t, model.CmdTypeCatalog, "34020000002000000001", 8))
	resp := h.tr.deliver(t, messageRequest(t, "34020000002000000001", "<Query/>"))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	rendered := h.codec.rendered()
	if len(rendered) != 1 || rendered[0].SumNum() != 0 {
		t.Fatalf("catalog = %v, want one empty answer", rendered)
	}
}

// Two platforms sharing a process must not see each other's downstreams:
// the catalog is per node, not per process.
func TestAcceptor_CatalogIsPerNode(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	secondID, err := model.ParseNodeID("34020000002000000002")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	secondTransport := newAcceptorTransport()
	if err := h.acceptor.Serve(secondID, secondTransport, "3402000000", defaultHarnessPolicy(t)); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	h.creds.add(t, secondID, "34020000011310000002", "secret")
	secondTransport.deliver(t, registerRequest(t, "34020000011310000002",
		model.NewHeader("Authorization", `Digest username="34020000011310000002", response="ok"`),
		model.NewHeader("Expires", "3600")))
	h.register(t, "34020000011310000001", "3600")

	h.codec.setNotify(mustNotify(t, model.CmdTypeCatalog, "34020000002000000001", 5))
	h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))

	rendered := h.codec.rendered()
	if len(rendered) != 1 {
		t.Fatalf("rendered %d catalogs, want 1", len(rendered))
	}
	if rendered[0].SumNum() != 1 {
		t.Fatalf("sum = %d, want 1", rendered[0].SumNum())
	}
	if rendered[0].Items()[0].DeviceID() != "34020000011310000001" {
		t.Errorf("the catalog of one node reports %s, a device of another",
			rendered[0].Items()[0].DeviceID())
	}
}

// Without a sequence number the answer could not be matched to the
// question, so the query is dropped rather than answered with a guess.
func TestAcceptor_IgnoresCatalogQueryWithoutSN(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000001", "3600")

	h.codec.setNotify(mustNotify(t, model.CmdTypeCatalog, "34020000002000000001", 0))
	h.silence(t, messageRequest(t, "34020000011310000001", "<Query/>"))

	if got := h.codec.rendered(); len(got) != 0 {
		t.Errorf("rendered %d catalogs for a query without an SN", len(got))
	}
}

// A command the platform does not serve is dropped, not answered with an
// error the peer cannot interpret.
func TestAcceptor_IgnoresUnknownCommand(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000001", "3600")

	h.codec.setNotify(mustNotify(t, "Alarm", "34020000011310000001", 3))
	h.silence(t, messageRequest(t, "34020000011310000001", "<Notify/>"))
}

// A body that cannot be read is data, not a crash: the platform ignores it
// and goes on serving the next message.
func TestAcceptor_UnreadableMessageIsIgnored(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.creds.add(t, h.nodeID, "34020000011310000001", "secret")

	h.codec.decodeErr = fmt.Errorf("not xml at all")
	h.silence(t, messageRequest(t, "34020000011310000001", "}}}"))

	// The loop survived: the very next REGISTER is still served.
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`),
		model.NewHeader("Expires", "3600"))
	if resp := h.tr.deliver(t, req); resp.StatusCode() != 200 {
		t.Fatalf("status = %d after an unreadable MESSAGE, want 200", resp.StatusCode())
	}
}

// A device that stops re-registering is thrown out when the lifetime it was
// granted lapses; one that is still inside it is left alone.
func TestAcceptor_SweepsExpiredDevices(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	short := h.register(t, "34020000011310000001", "60")
	long := h.register(t, "34020000011310000002", "7200")
	h.tickers.waitForTickers(t, 1)

	h.clock.Advance(61 * time.Second)
	h.tickers.tick()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := h.devices.Lookup(context.Background(), h.nodeID, "34020000011310000001"); !ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := h.devices.Lookup(context.Background(), h.nodeID, "34020000011310000001"); ok {
		t.Errorf("%s outlived the %ds it was granted and is still online",
			short.DeviceID(), short.GrantedExpiry())
	}
	if _, ok := h.devices.Lookup(context.Background(), h.nodeID, "34020000011310000002"); !ok {
		t.Errorf("%s was swept with %ds of its lifetime left",
			long.DeviceID(), long.GrantedExpiry())
	}
}

// A sweeping platform that never woke up would keep silent devices online
// forever, so the beat must be bounded by the lifetime it grants rather
// than by anything generous.
func TestAcceptor_SweepIntervalFollowsThePolicy(t *testing.T) {
	if got := sweepIntervalFor(mustPolicy(t, 60, 3600, 7200)); got != 30*time.Second {
		t.Errorf("interval for min 60 = %s, want 30s", got)
	}
	if got := sweepIntervalFor(mustPolicy(t, 2, 3600, 7200)); got != sweepMinInterval {
		t.Errorf("interval for min 2 = %s, want the lower bound %s", got, sweepMinInterval)
	}
	if got := sweepIntervalFor(mustPolicy(t, 86400, 86400, 86400)); got != sweepMaxInterval {
		t.Errorf("interval for min 86400 = %s, want the upper bound %s", got, sweepMaxInterval)
	}
}

func mustPolicy(t *testing.T, min, def, max uint32) model.ExpiresPolicy {
	t.Helper()
	p, err := model.NewExpiresPolicy(min, def, max)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	return p
}

// Stopping a node ends its sweeper with it: a tick after the stop must not
// touch a table nobody owns any more.
func TestAcceptor_SweepStopsWithTheNode(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000001", "60")
	h.tickers.waitForTickers(t, 1)

	h.acceptor.Stop(h.nodeID)

	h.clock.Advance(120 * time.Second)
	h.tickers.tick()
	time.Sleep(50 * time.Millisecond)
	if h.acceptor.Serving() != 0 {
		t.Errorf("serving = %d after Stop, want 0", h.acceptor.Serving())
	}
}

// Acceptor.Close is the shutdown path, and it must join every sweeper: a
// goroutine left behind would keep sweeping a table that is going away.
func TestAcceptor_CloseEndsSweepers(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000001", "60")
	h.tickers.waitForTickers(t, 1)

	if err := h.acceptor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, ticker := range h.tickers.tickers {
		if !ticker.stopped_() {
			t.Error("a sweeper's ticker was never stopped")
		}
	}
}

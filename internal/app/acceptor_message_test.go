package app

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// fakeMANSCDP stands in for the codec: it hands back whatever notify the
// test scripted, and it records every catalog it was asked to render, which
// is what the assertions read. The body it renders is a readable stand-in —
// the real bytes are the codec's own golden tests' business.
type fakeMANSCDP struct {
	mu          sync.Mutex
	notify      model.Notify
	decodeErr   error
	bodies      []string
	catalogs    []model.Catalog
	marshalErr  error
	recordQuery model.RecordInfoQuery
	presetLists []model.PresetListResponse
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
		b.WriteByte(' ')
		b.WriteString(it.DeviceID())
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

func (f *fakeMANSCDP) setRecordQuery(q model.RecordInfoQuery) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordQuery = q
}

func (f *fakeMANSCDP) renderedPresetLists() []model.PresetListResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.PresetListResponse, len(f.presetLists))
	copy(out, f.presetLists)
	return out
}

func (f *fakeMANSCDP) DecodeDeviceInfoQuery(body string) (model.DeviceInfoQuery, error) {
	return model.DeviceInfoQuery{}, nil
}

func (f *fakeMANSCDP) MarshalDeviceInfoResponse(resp model.DeviceInfoResponse) (string, error) {
	return "device-info-response", nil
}

func (f *fakeMANSCDP) DecodeRecordInfoQuery(body string) (model.RecordInfoQuery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies = append(f.bodies, body)
	return f.recordQuery, f.decodeErr
}

func (f *fakeMANSCDP) MarshalRecordInfoResponse(resp model.RecordInfoResponse) (string, error) {
	return fmt.Sprintf("record-info-response-SumNum=%d", resp.SumNum), nil
}

func (f *fakeMANSCDP) DecodeAlarmNotify(body string) (model.AlarmNotify, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.decodeErr != nil {
		return model.AlarmNotify{}, f.decodeErr
	}
	return model.NewAlarmNotify(model.AlarmNotifyParams{
		SN:            f.notify.SN(),
		DeviceID:      f.notify.DeviceID(),
		EventType:     "alarm",
		AlarmPriority: 1,
		AlarmMethod:   1,
	})
}

func (f *fakeMANSCDP) MarshalAlarmAck(ack model.AlarmAck) (string, error) {
	return "alarm-ack", nil
}

func (f *fakeMANSCDP) DecodePTZControl(body string) (model.PTZControl, error) {
	return model.PTZControl{}, nil
}

func (f *fakeMANSCDP) MarshalPTZControl(control model.PTZControl) (string, error) {
	return "ptz-control", nil
}

func (f *fakeMANSCDP) DecodePresetQuery(body string) (model.PresetQuery, error) {
	return model.PresetQuery{}, nil
}

func (f *fakeMANSCDP) MarshalPresetList(resp model.PresetListResponse) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presetLists = append(f.presetLists, resp)
	return "preset-list", nil
}

func (f *fakeMANSCDP) MarshalPresetAck(ack model.PresetAck) (string, error) {
	return "preset-ack", nil
}

func (f *fakeMANSCDP) DecodeHomePositionQuery(body string) (model.HomePositionQuery, error) {
	return model.HomePositionQuery{}, nil
}

func (f *fakeMANSCDP) MarshalHomePositionQuery(_ model.HomePositionQuery, _ uint32) (string, error) {
	return "home-position-query", nil
}

func (f *fakeMANSCDP) DecodeHomePositionSet(body string) (model.HomePositionSet, error) {
	return model.HomePositionSet{}, nil
}

func (f *fakeMANSCDP) MarshalHomePositionSet(_ model.HomePosition, _ uint32) (string, error) {
	return "home-position-set", nil
}

func (f *fakeMANSCDP) DecodeHomePositionResponse(body string) (model.HomePositionResponse, error) {
	return model.HomePositionResponse{}, nil
}

func (f *fakeMANSCDP) MarshalHomePositionResponse(_ model.HomePositionResponse, _ uint32) (string, error) {
	return "home-position-response", nil
}

func (f *fakeMANSCDP) DecodeCruiseTrackListQuery(body string) (model.CruiseTrackListQuery, error) {
	return model.CruiseTrackListQuery{}, nil
}

func (f *fakeMANSCDP) MarshalCruiseTrackListQuery(_ model.CruiseTrackListQuery, _ uint32) (string, error) {
	return "cruise-track-list-query", nil
}

func (f *fakeMANSCDP) DecodeCruiseTrackListResponse(body string) (model.CruiseTrackListResponse, error) {
	return model.CruiseTrackListResponse{}, nil
}

func (f *fakeMANSCDP) MarshalCruiseTrackListResponse(_ model.CruiseTrackListResponse, _ uint32) (string, error) {
	return "cruise-track-list-response", nil
}

func (f *fakeMANSCDP) DecodeSnapShotCommand(body string) (model.SnapShotCommand, error) {
	return model.SnapShotCommand{}, nil
}

func (f *fakeMANSCDP) MarshalSnapShotCommand(_ model.SnapShotCommand, _ uint32) (string, error) {
	return "snapshot-command", nil
}

func (f *fakeMANSCDP) DecodeSnapShotResponse(body string) (model.SnapShotResponse, error) {
	return model.SnapShotResponse{}, nil
}

func (f *fakeMANSCDP) MarshalSnapShotResponse(_ model.SnapShotResponse, _ uint32) (string, error) {
	return "snapshot-response", nil
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

	// Alarm used to be unknown but Change 10 answers it; a genuinely
	// unknown command type keeps testing the ignore path.
	h.codec.setNotify(mustNotify(t, "MobilePosition", "34020000011310000001", 3))
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

// ----------------------------------------------------------------------------
// INVITE / media pipeline
// ----------------------------------------------------------------------------

func inviteRequest(t *testing.T, deviceID, callID, body string) model.Message {
	t.Helper()
	msg, err := model.NewRequest("INVITE", "sip:"+deviceID+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=inv1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-inv"),
		model.NewHeader("Contact", "<sip:"+deviceID+"@127.0.0.1:15060>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// ackRequest and byeRequest were defined for tests that never used them;
// they were removed to silence the unusedfunc linter. Re-add if a future
// test needs to construct those requests in isolation.

var validSDP = `v=0
o=34020000011310000001 0 0 IN IP4 127.0.0.1
s=Play
c=IN IP4 127.0.0.1
t=0 0
m=video 6000 RTP/AVP 96
a=rtpmap:96 PS/90000
`

// An INVITE with valid SDP is answered 200 OK; without a dialog manager the
// pipeline is not created, but the response is still correct.
func TestAcceptor_InviteWithSDPAnswers200OK(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	resp := h.tr.deliver(t, inviteRequest(t, "34020000011310000001", "inv-call-1", validSDP))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if resp.Body() == "" {
		t.Error("200 OK body is empty, want SDP answer")
	}
}

// An INVITE with unreadable body is answered 400 Bad Request.
func TestAcceptor_InviteWithBadSDPAnswers400(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	resp := h.tr.deliver(t, inviteRequest(t, "34020000011310000001", "inv-call-2", "not sdp at all"))

	if resp.StatusCode() != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode())
	}
}

// An INVITE without a Call-ID is answered 400 Bad Request.
func TestAcceptor_InviteWithoutCallIDAnswers400(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	msg, err := model.NewRequest("INVITE", "sip:34020000002000000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000011310000001@3402000000>;tag=inv1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		// intentionally no Call-ID
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-inv"),
	}, validSDP)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp := h.tr.deliver(t, msg)

	if resp.StatusCode() != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode())
	}
}

// An INVITE without an m= line is answered 200 OK; the platform accepts it
// without media negotiation.
func TestAcceptor_InviteWithoutMediaAnswers200OK(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	noMediaSDP := `v=0
o=34020000011310000001 0 0 IN IP4 127.0.0.1
s=Play
c=IN IP4 127.0.0.1
t=0 0
`
	resp := h.tr.deliver(t, inviteRequest(t, "34020000011310000001", "inv-call-3", noMediaSDP))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
}

// An INVITE 200 OK carries the Allow header listing all supported methods.
func TestAcceptor_Invite200OKCarriesAllowHeader(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	resp := h.tr.deliver(t, inviteRequest(t, "34020000011310000001", "inv-call-4", validSDP))

	allow := responseHeader(t, resp, "Allow")
	for _, m := range []string{"INVITE", "ACK", "BYE", "OPTIONS", "SUBSCRIBE", "MESSAGE", "REGISTER"} {
		if !strings.Contains(allow, m) {
			t.Errorf("Allow header %q is missing method %s", allow, m)
		}
	}
}

// ----------------------------------------------------------------------------
// SUBSCRIBE / catalog notifications
// ----------------------------------------------------------------------------

func subscribeRequest(t *testing.T, deviceID, callID string, expires string) model.Message {
	t.Helper()
	hdrs := []model.Header{
		model.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=sub1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 SUBSCRIBE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-sub"),
		model.NewHeader("Event", "catalog"),
	}
	if expires != "" {
		hdrs = append(hdrs, model.NewHeader("Expires", expires))
	}
	msg, err := model.NewRequest("SUBSCRIBE", "sip:34020000002000000001@3402000000", hdrs, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// A catalog SUBSCRIBE is answered 200 OK; the initial NOTIFY carries the
// current device list.
func TestAcceptor_SubscribeCatalogAnswers200AndNotifies(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000001", "3600")
	h.register(t, "34020000011310000002", "3600")

	resp := h.tr.deliver(t, subscribeRequest(t, "34020000011310000003", "sub-call-1", "3600"))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "Expires"); got != "3600" {
		t.Errorf("Expires = %q, want 3600", got)
	}

	// The initial NOTIFY is sent via the transport; the codec is asked to
	// render the catalog.
	rendered := h.codec.rendered()
	if len(rendered) == 0 {
		t.Fatal("no NOTIFY was sent after SUBSCRIBE")
	}
	cat := rendered[len(rendered)-1]
	if cat.SumNum() != 2 {
		t.Errorf("catalog item count = %d, want 2", cat.SumNum())
	}
}

// A SUBSCRIBE with Expires 0 removes the subscription and answers 200 OK.
func TestAcceptor_SubscribeExpiresZeroRemovesAndAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))

	// First subscribe.
	h.tr.deliver(t, subscribeRequest(t, "34020000011310000003", "sub-call-2", "3600"))
	renderedBefore := len(h.codec.rendered())
	_ = renderedBefore

	// Then unsubscribe.
	resp := h.tr.deliver(t, subscribeRequest(t, "34020000011310000003", "sub-call-2", "0"))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
}

// A SUBSCRIBE for an Event other than catalog is rejected with 489 Bad Event.
func TestAcceptor_SubscribeNonCatalogAnswers489(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	msg, err := model.NewRequest("SUBSCRIBE", "sip:34020000002000000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000011310000001@3402000000>;tag=sub1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", "sub-call-3"),
		model.NewHeader("CSeq", "1 SUBSCRIBE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-sub"),
		model.NewHeader("Event", "presence"),
		model.NewHeader("Expires", "3600"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp := h.tr.deliver(t, msg)

	if resp.StatusCode() != 489 {
		t.Fatalf("status = %d, want 489", resp.StatusCode())
	}
	if got := resp.StatusText(); got != "Bad Event" {
		t.Errorf("reason = %q, want 'Bad Event'", got)
	}
}

// A SUBSCRIBE without a Call-ID is answered 400 Bad Request.
func TestAcceptor_SubscribeWithoutCallIDAnswers400(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	msg, err := model.NewRequest("SUBSCRIBE", "sip:34020000002000000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000011310000001@3402000000>;tag=sub1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		// intentionally no Call-ID
		model.NewHeader("CSeq", "1 SUBSCRIBE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-sub"),
		model.NewHeader("Event", "catalog"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp := h.tr.deliver(t, msg)

	if resp.StatusCode() != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode())
	}
}

// After a device registers, every active subscriber receives a fresh NOTIFY.
func TestAcceptor_DeviceRegistrationTriggersCatalogNotify(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))

	// Register a subscriber first.
	h.tr.deliver(t, subscribeRequest(t, "34020000011310000003", "sub-call-4", "3600"))
	before := len(h.codec.rendered())

	// Register a device.
	h.register(t, "34020000011310000001", "3600")

	after := len(h.codec.rendered())
	if after <= before {
		t.Errorf("no new NOTIFY after device registration: %d → %d", before, after)
	}
}

// ----------------------------------------------------------------------------
// OPTIONS / capability query
// ----------------------------------------------------------------------------

func optionsRequest(t *testing.T, deviceID string) model.Message {
	t.Helper()
	msg, err := model.NewRequest("OPTIONS", "sip:"+deviceID+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=opt1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", "opt-call-1"),
		model.NewHeader("CSeq", "1 OPTIONS"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-opt"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// An OPTIONS request is answered 200 OK with Allow and Accept headers.
func TestAcceptor_OptionsAnswers200OK(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	resp := h.tr.deliver(t, optionsRequest(t, "34020000011310000001"))

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	allow := responseHeader(t, resp, "Allow")
	if !strings.Contains(allow, "INVITE") {
		t.Errorf("Allow %q missing INVITE", allow)
	}
	if !strings.Contains(allow, "MESSAGE") {
		t.Errorf("Allow %q missing MESSAGE", allow)
	}
	if responseHeader(t, resp, "Accept") == "" {
		t.Error("Accept header missing")
	}
}

// ----------------------------------------------------------------------------
// MediaStatus
// ----------------------------------------------------------------------------

type fakeMediaStatusPort struct {
	handle func(ctx context.Context, report model.MediaStatusReport) error
}

func (f *fakeMediaStatusPort) HandleMediaStatus(ctx context.Context, report model.MediaStatusReport) error {
	if f.handle == nil {
		return nil
	}
	return f.handle(ctx, report)
}

func mediaStatusRequest(t *testing.T, deviceID string, sn uint32) model.Message {
	t.Helper()
	body := fmt.Sprintf(`<Notify>
  <CmdType>MediaStatus</CmdType>
  <DeviceID>%s</DeviceID>
  <SN>%d</SN>
  <Status>OK</Status>
</Notify>`, deviceID, sn)
	return messageRequest(t, deviceID, body)
}

// A MediaStatus notify is answered 200 OK when the port is set.
func TestAcceptor_MediaStatusAnswers200WithHandler(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))

	var handled bool
	h.acceptor.WithMediaStatus(&fakeMediaStatusPort{
		handle: func(ctx context.Context, r model.MediaStatusReport) error {
			handled = true
			return nil
		},
	})

	h.codec.setNotify(mustNotify(t, model.CmdTypeMediaStatus, "34020000011310000001", 99))
	resp := h.tr.deliver(t, mediaStatusRequest(t, "34020000011310000001", 99))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if !handled {
		t.Error("MediaStatus handler was not called")
	}
}

// A MediaStatus notify without a handler is still answered 200 OK (silently).
func TestAcceptor_MediaStatusAnswers200WithoutHandler(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	// No WithMediaStatus call.

	h.codec.setNotify(mustNotify(t, model.CmdTypeMediaStatus, "34020000011310000001", 98))
	resp := h.tr.deliver(t, mediaStatusRequest(t, "34020000011310000001", 98))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
}

// A MediaStatus from a stranger is still answered 200 OK.
func TestAcceptor_MediaStatusFromStrangerAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	var handled bool
	h.acceptor.WithMediaStatus(&fakeMediaStatusPort{
		handle: func(ctx context.Context, r model.MediaStatusReport) error {
			handled = true
			return nil
		},
	})

	// No device registered for this ID, but the acceptor still answers 200.
	h.codec.setNotify(mustNotify(t, model.CmdTypeMediaStatus, "34020000011310009999", 97))
	resp := h.tr.deliver(t, mediaStatusRequest(t, "34020000011310009999", 97))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	// The handler is invoked even for unregistered devices (no registry gate).
	if !handled {
		t.Error("MediaStatus handler was not called for stranger device")
	}
}

// newMessageHarnessWithRegistry is like newMessageHarness but also registers
// the platform node in a nodereg.Registry and attaches it to the acceptor.
func newMessageHarnessWithRegistry(t *testing.T) (*messageHarness, *nodereg.Registry) {
	t.Helper()
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	reg := nodereg.New()
	profile, err := model.NewNodeProfile(h.nodeID.String(), "127.0.0.1:5060", "3402000000", "")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if _, err := reg.Register(context.Background(), profile); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h.acceptor.WithNodeRegistry(reg)
	return h, reg
}

// TestAcceptor_AnswersDeviceInfo verifies the acceptor answers a DeviceInfo
// query with the XML body the codec renders (task 6.1).
func TestAcceptor_AnswersDeviceInfo(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000001", "3600")
	h.codec.setNotify(mustNotify(t, model.CmdTypeDeviceInfo, "34020000011310000001", 11))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := resp.Body(); !strings.Contains(got, "device-info-response") {
		t.Errorf("device info body missing expected marker:\n%s", got)
	}
}

// TestAcceptor_AnswersAlarmAndStoresSnapshot verifies the acceptor sends an
// AlarmAck and appends the alarm snapshot to the serving node's in-memory
// profile when a node registry is attached (tasks 3.2/6.2).
func TestAcceptor_AnswersAlarmAndStoresSnapshot(t *testing.T) {
	h, reg := newMessageHarnessWithRegistry(t)
	h.register(t, "34020000011310000001", "3600")
	h.codec.setNotify(mustNotify(t, model.CmdTypeAlarm, "34020000011310000001", 9))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Notify/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := resp.Body(); !strings.Contains(got, "alarm-ack") {
		t.Errorf("alarm ack missing in body:\n%s", got)
	}
	node, ok := reg.Get(context.Background(), h.nodeID)
	if !ok {
		t.Fatal("platform node not found in registry")
	}
	if len(node.Profile().Alarms()) != 1 {
		t.Fatalf("alarms in profile = %d, want 1", len(node.Profile().Alarms()))
	}
	snap := node.Profile().Alarms()[0]
	if snap.Priority() != 1 || snap.Method() != 1 || snap.Description() != "" {
		t.Errorf("snapshot = %+v, want priority=1/method=1", snap)
	}
}

// TestAcceptor_AnswersRecordInfoFiltersByTimeRange verifies the acceptor
// rejects records outside the query's StartTime/EndTime window and echoes
// the query SN (task 6.3).
func TestAcceptor_AnswersRecordInfoFiltersByTimeRange(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	// No device registered: stranger query still answers 200 (other tests
	// cover the gate). Time range is the thing under test.
	h.codec.setNotify(mustNotify(t, model.CmdTypeRecordInfo, "34020000011310000001", 13))
	h.codec.setRecordQuery(model.RecordInfoQuery{
		DeviceID:  "34020000011310000001",
		StartTime: "20260925T010000",
		EndTime:   "20260925T020000",
	})
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	body := resp.Body()
	if !strings.Contains(body, "SumNum=0") {
		t.Errorf("filtered record list must be empty:\n%s", body)
	}
	// A query covering the synthetic record must return one item.
	h.codec.setNotify(mustNotify(t, model.CmdTypeRecordInfo, "34020000011310000001", 14))
	h.codec.setRecordQuery(model.RecordInfoQuery{
		DeviceID:  "34020000011310000001",
		StartTime: "20260924T000000",
		EndTime:   "20260926T000000",
	})
	resp = h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	body = resp.Body()
	if !strings.Contains(body, "SumNum=1") {
		t.Errorf("matching record list must have SumNum=1:\n%s", body)
	}
}

// TestAcceptor_AnswersPresetQueryFromProfile verifies the acceptor answers a
// PresetQuery with the preset list the node's profile carries, returning an
// empty list when there are no presets (task 3.5).
func TestAcceptor_AnswersPresetQueryFromProfile(t *testing.T) {
	h, reg := newMessageHarnessWithRegistry(t)
	node, _ := reg.Get(context.Background(), h.nodeID)
	next, _ := node.Profile().WithPresets([]model.PresetItem{
		{PresetIndex: 1, Name: "Entrance"},
		{PresetIndex: 2, Name: "Parking"},
	})
	reg.MutateProfile(context.Background(), h.nodeID, func(np model.NodeProfile) (model.NodeProfile, error) {
		return next, nil
	})
	// Register one device so the platform is "serving".
	h.register(t, "34020000011310000001", "3600")
	h.codec.setNotify(mustNotify(t, model.CmdTypePresetQuery, "34020000011310000001", 5))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	rendered := h.codec.renderedPresetLists()
	if len(rendered) != 1 {
		t.Fatalf("rendered %d preset lists, want 1", len(rendered))
	}
	if rendered[0].SumNum != 2 {
		t.Errorf("preset list sum = %d, want 2", rendered[0].SumNum)
	}
	if len(rendered[0].Items) != 2 ||
		rendered[0].Items[0].PresetIndex != 1 || rendered[0].Items[0].Name != "Entrance" {
		t.Errorf("preset items = %+v", rendered[0].Items)
	}
}

// TestAcceptor_CatalogIncludesProfileChannels verifies the catalog answer
// carries the dynamic channels the node's profile registered, mixing the
// static device with the mutable channel items (task 5.2).
func TestAcceptor_CatalogIncludesProfileChannels(t *testing.T) {
	h, reg := newMessageHarnessWithRegistry(t)
	// Prepare a profile with two channels.
	channels := []model.Channel{
		mustChannel(t, "34020000001320000001", "channel-1", "34020000001320000001", model.ChannelStatusOnline),
		mustChannel(t, "34020000001320000002", "channel-2", "34020000001320000002", model.ChannelStatusOffline),
	}
	base, err := model.NewNodeProfile(h.nodeID.String(), "127.0.0.1:5060", "3402000000", "")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	next, _ := base.WithChannels(channels)
	if _, err := reg.MutateProfile(context.Background(), h.nodeID, func(np model.NodeProfile) (model.NodeProfile, error) {
		return next, nil
	}); err != nil {
		t.Fatalf("MutateProfile: %v", err)
	}
	// Register one device so the catalog is non-empty.
	h.register(t, "34020000011310000001", "3600")
	h.codec.setNotify(mustNotify(t, model.CmdTypeCatalog, "34020000002000000001", 7))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	rendered := h.codec.rendered()
	if len(rendered) != 1 {
		t.Fatalf("rendered %d catalogs, want 1", len(rendered))
	}
	if rendered[0].SumNum() != 3 {
		t.Fatalf("sum = %d, want 3 (1 device + 2 channels)", rendered[0].SumNum())
	}
	ids := make([]string, 0, len(rendered[0].Items()))
	for _, it := range rendered[0].Items() {
		ids = append(ids, it.DeviceID())
	}
	sort.Strings(ids)
	want := []string{
		"34020000001320000001",
		"34020000001320000002",
		"34020000011310000001",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("catalog ids = %v, want %v", ids, want)
	}
}

// mustChannel is a test helper that builds a validated Channel.
func mustChannel(t *testing.T, id, name, parentID string, status model.ChannelStatus) model.Channel {
	t.Helper()
	ch, err := model.NewChannel(id, name, parentID, status)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	return ch
}

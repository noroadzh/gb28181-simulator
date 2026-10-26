package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/cascade"
	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

const (
	relayLargeID  = "34020000002000000001"
	relaySmallID  = "34020000002160000001"
	relayDeviceID = "34020000001310000001"
	relayProbe    = "127.0.0.1:15060" // the peer address the fake transport reports
)

type relayHarness struct {
	acceptor *Acceptor
	tr       *acceptorTransport
	codec    *manscdp.MANSCDPCodecAdapter
}

// newRelayHarness serves platform-large with a cascade chain
// large -> small -> device, all registered so the handler can resolve hops.
func newRelayHarness(t *testing.T) *relayHarness {
	t.Helper()
	tr := newAcceptorTransport()
	codec := manscdp.NewMANSCDPCodec()
	acceptor, err := NewAcceptor(context.Background(), newSyncClock(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)),
		&fakeChallenger{}, &fakeAuthenticator{}, newFakeCredentials(), newFakeDevices(),
		codec, newTickerScript().factory(), discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}

	reg := nodereg.New()
	handler := cascade.New(nil, discardLogger())
	reg.WithCascadeHandler(handler)
	acceptor.WithNodeRegistry(reg).WithCascadeHandler(handler)

	for _, p := range []model.NodeProfile{
		relayProfile(t, relayDeviceID, "127.0.0.1:15063", relaySmallID, nil),
		relayProfile(t, relaySmallID, "127.0.0.1:15062", relayLargeID, []string{relayDeviceID}),
		relayProfile(t, relayLargeID, "127.0.0.1:15061", "", []string{relaySmallID}),
	} {
		if _, err := reg.Register(context.Background(), p); err != nil {
			t.Fatalf("register %s: %v", p.ID(), err)
		}
	}

	serving, err := model.DefaultPlatformServing("3402000000")
	if err != nil {
		t.Fatalf("DefaultPlatformServing: %v", err)
	}
	largeID := mustParse(t, relayLargeID)
	if err := acceptor.Serve(largeID, tr, serving.Realm(), serving.Policy()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = acceptor.Close() })

	return &relayHarness{acceptor: acceptor, tr: tr, codec: codec}
}

func relayProfile(t *testing.T, id, addr, parent string, children []string) model.NodeProfile {
	t.Helper()
	p, err := model.NewNodeProfile(id, addr, "3402000000", "sim")
	if err != nil {
		t.Fatalf("NewNodeProfile(%s): %v", id, err)
	}
	if parent != "" {
		if p, err = p.WithCascadeParent(parent); err != nil {
			t.Fatalf("WithCascadeParent(%s): %v", id, err)
		}
	}
	if len(children) > 0 {
		if p, err = p.WithCascadeChildren(children); err != nil {
			t.Fatalf("WithCascadeChildren(%s): %v", id, err)
		}
	}
	return p
}

// relayRequestFor builds the catalog query a probe sends for target, with any
// extra headers (X-PreferredPath, X-RoutePath) the hop selection needs.
func relayRequestFor(t *testing.T, codec *manscdp.MANSCDPCodecAdapter, target, callID, cseq string, extra ...model.Header) model.Message {
	t.Helper()
	body, err := codec.MarshalCatalogQuery(target, 1)
	if err != nil {
		t.Fatalf("MarshalCatalogQuery: %v", err)
	}
	hdrs := []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-probe"),
		model.NewHeader("From", "<sip:34020000002000000009@127.0.0.1>;tag=probe"),
		model.NewHeader("To", "<sip:"+target+"@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", cseq),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
	}
	hdrs = append(hdrs, extra...)
	msg, err := model.NewRequest("MESSAGE", "sip:"+target+"@3402000000", hdrs, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// waited returns the first delivery matching pred, polling until deadline.
func (h *relayHarness) waited(t *testing.T, pred func(model.Message, string) bool) (model.Message, string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.tr.mu.Lock()
		for i := range h.tr.sent {
			if pred(h.tr.sent[i], h.tr.peers[i]) {
				msg, peer := h.tr.sent[i], h.tr.peers[i]
				h.tr.mu.Unlock()
				return msg, peer
			}
		}
		h.tr.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for the expected send")
	return model.Message{}, ""
}

func (h *relayHarness) deliveries() int {
	h.tr.mu.Lock()
	defer h.tr.mu.Unlock()
	return len(h.tr.sent)
}

// Task 2.1: a request for a cascade child is delivered to that child, not
// answered locally and not sent back to the requester.
func TestRelay_ForwardsToCascadeChild(t *testing.T) {
	h := newRelayHarness(t)
	req := relayRequestFor(t, h.codec, relayDeviceID, "relay-1", "1 MESSAGE",
		model.NewHeader("X-PreferredPath", relaySmallID))

	h.tr.inbound <- req
	msg, peer := h.waited(t, func(m model.Message, _ string) bool { return m.IsRequest() })

	if peer != "127.0.0.1:15062" {
		t.Errorf("delivered to %q, want the small node's address", peer)
	}
	if msg.Method() != "MESSAGE" {
		t.Errorf("method = %q, want MESSAGE", msg.Method())
	}
	if got := headerValue(msg, "X-RoutePath"); got != relayLargeID {
		t.Errorf("X-RoutePath = %q, want %q", got, relayLargeID)
	}
	if got := headerValue(msg, "Call-ID"); got != "relay-1" {
		t.Errorf("Call-ID = %q, want it preserved", got)
	}
	// Our Via must be on top so the answer comes back here.
	firstVia, ok := msg.Header("Via")
	if !ok || !contains(firstVia.Value(), "127.0.0.1:15060") {
		t.Errorf("top Via = %q, want this node's address", firstVia.Value())
	}
}

// Task 2.2: the downstream answer goes back to the requester without this
// node's Via.
func TestRelay_ReturnsDownstreamAnswer(t *testing.T) {
	h := newRelayHarness(t)
	req := relayRequestFor(t, h.codec, relayDeviceID, "relay-2", "1 MESSAGE",
		model.NewHeader("X-PreferredPath", relaySmallID))
	h.tr.inbound <- req
	h.waited(t, func(m model.Message, _ string) bool { return m.IsRequest() })

	answer, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-relay"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-probe"),
		model.NewHeader("From", "<sip:34020000002000000009@127.0.0.1>;tag=probe"),
		model.NewHeader("To", "<sip:"+relayDeviceID+"@3402000000>;tag=device"),
		model.NewHeader("Call-ID", "relay-2"),
		model.NewHeader("CSeq", "1 MESSAGE"),
	}, "<Response/>")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	h.tr.inbound <- answer

	msg, peer := h.waited(t, func(m model.Message, _ string) bool { return !m.IsRequest() })
	if peer != relayProbe {
		t.Errorf("answer went to %q, want the original requester %q", peer, relayProbe)
	}
	if got := headerValue(msg, "Via"); !contains(got, "z9hG4bK-probe") {
		t.Errorf("top Via = %q, want the requester's branch", got)
	}
	if got := headerValue(msg, "Call-ID"); got != "relay-2" {
		t.Errorf("Call-ID = %q, want it preserved", got)
	}
}

// Task 2.3: no downstream answer within the wait becomes a 504 for the
// requester, and the table drains.
func TestRelay_TimesOutWith504(t *testing.T) {
	h := newRelayHarness(t)
	h.acceptor.relayWait = 30 * time.Millisecond

	req := relayRequestFor(t, h.codec, relayDeviceID, "relay-3", "1 MESSAGE",
		model.NewHeader("X-PreferredPath", relaySmallID))
	h.tr.inbound <- req

	msg, peer := h.waited(t, func(m model.Message, _ string) bool { return !m.IsRequest() })
	if msg.StatusCode() != 504 {
		t.Errorf("status = %d, want 504", msg.StatusCode())
	}
	if peer != relayProbe {
		t.Errorf("504 went to %q, want the requester", peer)
	}
	if got := h.acceptor.relay.size(); got != 0 {
		t.Errorf("relay table size = %d, want 0 after the timeout", got)
	}
}

// Task 2.4a: a message that already traversed this node is refused with 482
// and nothing is forwarded.
func TestRelay_RefusesLoop(t *testing.T) {
	h := newRelayHarness(t)
	req := relayRequestFor(t, h.codec, relayDeviceID, "relay-4", "1 MESSAGE",
		model.NewHeader("X-PreferredPath", relaySmallID),
		model.NewHeader("X-RoutePath", relayLargeID))
	h.tr.inbound <- req

	msg, _ := h.waited(t, func(m model.Message, _ string) bool { return !m.IsRequest() })
	if msg.StatusCode() != 482 {
		t.Errorf("status = %d, want 482", msg.StatusCode())
	}
	h.tr.mu.Lock()
	defer h.tr.mu.Unlock()
	for i, m := range h.tr.sent {
		if m.IsRequest() {
			t.Errorf("a request was forwarded to %s despite the loop", h.tr.peers[i])
		}
	}
}

// Task 2.4b: no usable next-hop address is a 500, not a silent drop.
func TestRelay_RefusesUnreachableNextHop(t *testing.T) {
	h := newRelayHarness(t)
	h.acceptor.registry = &emptyRegistry{NodeRegistry: h.acceptor.registry, hide: relaySmallID}

	req := relayRequestFor(t, h.codec, relayDeviceID, "relay-5", "1 MESSAGE",
		model.NewHeader("X-PreferredPath", relaySmallID))
	h.tr.inbound <- req

	msg, _ := h.waited(t, func(m model.Message, _ string) bool { return !m.IsRequest() })
	if msg.StatusCode() != 500 {
		t.Errorf("status = %d, want 500", msg.StatusCode())
	}
}

// Task 2.4c: a transport failure on the way out is a 500.
func TestRelay_RefusesOnSendFailure(t *testing.T) {
	h := newRelayHarness(t)
	h.tr.failSend = errors.New("network down")

	req := relayRequestFor(t, h.codec, relayDeviceID, "relay-6", "1 MESSAGE",
		model.NewHeader("X-PreferredPath", relaySmallID))
	h.tr.inbound <- req

	// The failure response cannot be sent either (Send always fails), so
	// assert on the table instead: the hand-off must not linger.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.acceptor.relay.size() == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("relay table kept a hand-off whose send failed")
}

// emptyRegistry hides one node from lookups, so a cascade child that exists
// in the topology cannot be resolved to an address.
type emptyRegistry struct {
	port.NodeRegistry
	hide string
}

func (r *emptyRegistry) Get(ctx context.Context, id model.NodeID) (model.Node, bool) {
	if id.String() == r.hide {
		return model.Node{}, false
	}
	return r.NodeRegistry.Get(ctx, id)
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

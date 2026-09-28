package app

import (
	"context"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// nodeRegistryFake lets a test return a node with a given profile. The
// embedded interface keeps the fake small: only Get is called on the
// subscription paths under test.
type nodeRegistryFake struct {
	port.NodeRegistry
	nodes map[model.NodeID]model.Node
}

func (r *nodeRegistryFake) Get(_ context.Context, id model.NodeID) (model.Node, bool) {
	n, ok := r.nodes[id]
	return n, ok
}

func withNodeRegistry(t *testing.T, h *messageHarness) *nodeRegistryFake {
	t.Helper()
	reg := &nodeRegistryFake{nodes: make(map[model.NodeID]model.Node)}
	h.acceptor.WithNodeRegistry(reg)
	return reg
}

// subscribeEventRequest builds a SUBSCRIBE request with the given event.
func subscribeEventRequest(t *testing.T, deviceID, callID, event, expires string) model.Message {
	t.Helper()
	hdrs := []model.Header{
		model.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=sub1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 SUBSCRIBE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-sub"),
	}
	if event != "" {
		hdrs = append(hdrs, model.NewHeader("Event", event))
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

func TestAcceptor_SubscribeEventsAnswer200UnknownAnswers489(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))

	cases := []struct {
		name  string
		event string
		want  int
	}{
		{"catalog", "catalog", 200},
		{"alarm", "alarm", 200},
		{"mobileposition", "mobileposition", 200},
		{"mixed case", "AlArm", 200},
		{"no event defaults to catalog", "", 200},
		{"unknown", "presence", 489},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			callID := "ev-" + strings.ReplaceAll(tc.name, " ", "-")
			resp := h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", callID, tc.event, "3600"))
			if resp.StatusCode() != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode(), tc.want)
			}
		})
	}
}

func TestAcceptor_SubscribeExpiresZeroClearsAndAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", "ev-zero", "alarm", "3600"))
	resp := h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", "ev-zero", "alarm", "0"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "Expires"); got != "0" {
		t.Errorf("Expires = %q, want 0", got)
	}
}

// TestAcceptor_SubscribeEventsDoNotCrossTalk asserts catalog subscribers
// never receive alarm NOTIFY and vice versa.
func TestAcceptor_SubscribeEventsDoNotCrossTalk(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.register(t, "34020000011310000001", "3600")

	// Subscribe two events: catalog on call-cat, alarm on call-alarm.
	h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", "call-cat", "catalog", "3600"))
	h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", "call-alarm", "alarm", "3600"))

	// Device registration: a catalog NOTIFY must go to call-cat only.
	before := len(h.tr.delivered())
	h.register(t, "34020000011310000002", "3600")
	sent := h.tr.delivered()[before:]
	foundCat, foundAlarm := 0, 0
	for _, m := range sent {
		if !strings.EqualFold(m.Method(), "NOTIFY") {
			continue
		}
		ev := responseHeader(t, m, "Event")
		callID := responseHeader(t, m, "Call-ID")
		switch {
		case ev == "catalog" && callID == "call-cat":
			foundCat++
		case ev == "alarm":
			foundAlarm++
		}
	}
	if foundCat != 1 {
		t.Errorf("catalog NOTIFY to catalog subscriber = %d, want 1", foundCat)
	}
	if foundAlarm != 0 {
		t.Errorf("alarm NOTIFY after registration = %d, want 0", foundAlarm)
	}

	// An alarm MESSAGE from downstream: an alarm NOTIFY must go to
	// call-alarm only.
	h.codec.setNotify(mustNotify(t, model.CmdTypeAlarm, "34020000011310000001", 1))
	before = len(h.tr.delivered())
	h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Notify/>"))
	sent = h.tr.delivered()[before:]
	foundCat, foundAlarm = 0, 0
	for _, m := range sent {
		if !strings.EqualFold(m.Method(), "NOTIFY") {
			continue
		}
		ev := responseHeader(t, m, "Event")
		callID := responseHeader(t, m, "Call-ID")
		switch {
		case ev == "catalog":
			foundCat++
		case ev == "alarm" && callID == "call-alarm":
			foundAlarm++
		}
	}
	if foundCat != 0 {
		t.Errorf("catalog NOTIFY after alarm = %d, want 0", foundCat)
	}
	if foundAlarm != 1 {
		t.Errorf("alarm NOTIFY to alarm subscriber = %d, want 1", foundAlarm)
	}
}

// TestAcceptor_SubscribeMobilePositionInitialNotify asserts the initial
// mobileposition NOTIFY carries the profile's configured position; an
// unconfigured position yields an empty-body NOTIFY.
func TestAcceptor_SubscribeMobilePositionInitialNotify(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	reg := withNodeRegistry(t, h)
	nodeID := mustPlatformNode(t)

	profile, err := model.NewNodeProfile(nodeID.String(), "127.0.0.1:15061", "3402000000", "sim")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	pos, err := model.NewPosition(116.397428, 39.90923, 10)
	if err != nil {
		t.Fatalf("NewPosition: %v", err)
	}
	reg.nodes[nodeID] = model.NewNode(profile.WithPosition(pos))

	before := len(h.tr.delivered())
	resp := h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", "call-pos", "mobileposition", "3600"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	sent := h.tr.delivered()[before:]
	found := 0
	for _, m := range sent {
		if !strings.EqualFold(m.Method(), "NOTIFY") {
			continue
		}
		if responseHeader(t, m, "Event") == "mobileposition" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("mobileposition NOTIFY count = %d, want 1", found)
	}
	rendered := h.codec.renderedMobilePositions()
	if len(rendered) != 1 {
		t.Fatalf("marshaler called %d times, want 1", len(rendered))
	}
	mp := rendered[0]
	if mp.Longitude() != 116.397428 || mp.Latitude() != 39.90923 || mp.Speed() != 10 {
		t.Errorf("initial mobileposition = %v, want the configured fix", mp)
	}
	if mp.DeviceID() != nodeID.String() {
		t.Errorf("mobileposition device = %q, want %q", mp.DeviceID(), nodeID.String())
	}

	// An unconfigured position still answers the SUBSCRIBE, with an empty
	// body NOTIFY: the marshaler is never called.
	delete(reg.nodes, nodeID)
	before = len(h.tr.delivered())
	h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", "call-pos-nofix", "mobileposition", "3600"))
	sent = h.tr.delivered()[before:]
	found = 0
	for _, m := range sent {
		if !strings.EqualFold(m.Method(), "NOTIFY") {
			continue
		}
		if responseHeader(t, m, "Event") == "mobileposition" {
			found++
		}
	}
	if found != 1 {
		t.Errorf("empty-position NOTIFY count = %d, want 1", found)
	}
	// The marshaler was skipped: no mobile position was rendered beyond
	// the earlier one.
	if n := len(h.codec.renderedMobilePositions()); n != 1 {
		t.Errorf("renderedMobilePositions = %d, want 1", n)
	}
}

// TestAcceptor_SetPositionPushesMobilePosition drives the runtime-API path:
// after SetPosition the mobileposition subscriber receives a NOTIFY with the
// new coordinates.
func TestAcceptor_SetPositionPushesMobilePosition(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	withNodeRegistry(t, h)

	// A mobileposition subscriber first, so the push has a target.
	h.tr.deliver(t, subscribeEventRequest(t, "34020000011310000003", "call-pos-push", "mobileposition", "3600"))

	pos, err := model.NewPosition(121.473701, 31.230416, 0)
	if err != nil {
		t.Fatalf("NewPosition: %v", err)
	}
	before := len(h.tr.delivered())
	h.acceptor.NotifyPositionChanged(context.Background(), h.nodeID, pos)

	sent := h.tr.delivered()[before:]
	found := 0
	for _, m := range sent {
		if !strings.EqualFold(m.Method(), "NOTIFY") {
			continue
		}
		if responseHeader(t, m, "Event") == "mobileposition" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("pushed mobileposition NOTIFY count = %d, want 1", found)
	}
	// The fake codec records what it rendered: exactly the supplied fix.
	rendered := h.codec.renderedMobilePositions()
	if len(rendered) != 1 {
		t.Fatalf("marshaler called %d times, want 1", len(rendered))
	}
	mp := rendered[0]
	if mp.DeviceID() != h.nodeID.String() ||
		mp.Longitude() != 121.473701 ||
		mp.Latitude() != 31.230416 ||
		mp.Speed() != 0 {
		t.Errorf("pushed mobileposition = %v, want the supplied fix", mp)
	}

	// A node that does not serve stays silent instead of panicking.
	other, err := model.ParseNodeID("34020000001320000099")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	h.acceptor.NotifyPositionChanged(context.Background(), other, pos)
}

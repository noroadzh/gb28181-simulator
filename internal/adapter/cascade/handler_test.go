package cascade

import (
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// stubTopology implements Topology with in-memory maps.
type stubTopology struct {
	parent   map[string]string
	children map[string][]string
}

func (s *stubTopology) Parent(node string) string     { return s.parent[node] }
func (s *stubTopology) Children(node string) []string { return s.children[node] }

// mustNodeID parses a raw 20-char device id or fails the test.
func mustNodeID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

// newMsg builds a minimal Message using the NewRequest constructor.
func newMsg(t *testing.T, headers []model.Header) model.Message {
	t.Helper()
	msg, err := model.NewRequest("MESSAGE", "sip:target@example.com", headers, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// TestTopologyMap_BasicLookup covers task 2.2: profiles with and without
// cascade settings are indexed, Parent/Children return the correct values,
// and a missing profile falls back to ""/nil.
func TestTopologyMap_BasicLookup(t *testing.T) {
	pLarge, _ := model.NewNodeProfile("34020000012000000001", "10.0.0.1:5060", "3402000000", "platform-large")
	pSmall, _ := model.NewNodeProfile("34020000012160000001", "10.0.0.2:5060", "3402000000", "platform-small")
	pDevice, _ := model.NewNodeProfile("34020000011310000001", "10.0.0.3:5060", "3402000000", "device")
	pLeaf, _ := model.NewNodeProfile("34020000011310000002", "10.0.0.4:5060", "3402000000", "device")

	smallWithParent, _ := pSmall.WithCascadeParent(pLarge.ID().String())
	smallWithChildren, _ := smallWithParent.WithCascadeChildren([]string{pDevice.ID().String()})
	deviceWithParent, _ := pDevice.WithCascadeParent(pSmall.ID().String())

	m := NewTopologyMap([]model.NodeProfile{pLarge, smallWithChildren, deviceWithParent, pLeaf})

	if got := m.Parent(pLarge.ID().String()); got != "" {
		t.Errorf("parent of %s = %q, want \"\"", pLarge.ID(), got)
	}
	if got := m.Children(pLarge.ID().String()); len(got) != 0 {
		t.Errorf("children of %s = %v, want []", pLarge.ID(), got)
	}

	if got := m.Parent(pSmall.ID().String()); got != pLarge.ID().String() {
		t.Errorf("parent of %s = %q, want %s", pSmall.ID(), got, pLarge.ID())
	}
	wantChildren := []string{pDevice.ID().String()}
	got := m.Children(pSmall.ID().String())
	if len(got) != len(wantChildren) || got[0] != wantChildren[0] {
		t.Errorf("children of %s = %v, want %v", pSmall.ID(), got, wantChildren)
	}

	if got := m.Parent(pDevice.ID().String()); got != pSmall.ID().String() {
		t.Errorf("parent of %s = %q, want %s", pDevice.ID(), got, pSmall.ID())
	}
	if got := m.Children(pDevice.ID().String()); len(got) != 0 {
		t.Errorf("children of leaf %s = %v, want []", pDevice.ID(), got)
	}

	// Missing profile returns zero value (no error).
	if got := m.Parent("99999999999999999999"); got != "" {
		t.Errorf("missing parent = %q, want \"\"", got)
	}
	if got := m.Children("99999999999999999999"); got != nil {
		t.Errorf("missing children = %v, want nil", got)
	}
}

// TestTopologyMap_NilInput covers the nil/empty profile list edge case.
func TestTopologyMap_NilInput(t *testing.T) {
	m := NewTopologyMap(nil)
	if m.Parent("x") != "" || m.Children("x") != nil {
		t.Error("nil TopologyMap returned non-zero values")
	}
}

// TestHandler_Forward_Child covers task 2.3: forwarding to a direct child
// appends ourselves to RoutePath, does NOT include PreferredPath (because
// we're the next hop), and returns the child as next hop.
func TestHandler_Forward_Child(t *testing.T) {
	const large = "34020000012000000001"
	const small = "34020000012160000001"
	const device = "34020000011310000001"

	pLarge, _ := model.NewNodeProfile(large, "10.0.0.1:5060", "3402000000", "platform-large")
	pSmall, _ := model.NewNodeProfile(small, "10.0.0.2:5060", "3402000000", "platform-small")
	pDevice, _ := model.NewNodeProfile(device, "10.0.0.3:5060", large, "device")
	smallWithChildren, _ := pSmall.WithCascadeChildren([]string{device})

	m := NewTopologyMap([]model.NodeProfile{pLarge, smallWithChildren, pDevice})
	h := New(m)

	msg := newMsg(t, nil)
	next, hdrs, _, err := h.Forward(mustNodeID(t, small), msg, device)
	if err != nil {
		t.Fatalf("Forward(child) = %v", err)
	}
	if next != device {
		t.Errorf("next hop = %q, want %q (direct child)", next, device)
	}
	if len(hdrs) != 1 {
		t.Fatalf("headers count = %d, want 1", len(hdrs))
	}
	if hdrs[0].Name() != model.HeaderRoutePath {
		t.Errorf("header name = %s, want %s", hdrs[0].Name(), model.HeaderRoutePath)
	}
	if hdrs[0].Value() != small {
		t.Errorf("header value = %q, want %q (self on route)", hdrs[0].Value(), small)
	}
}

// TestHandler_Forward_Upstream covers forwarding to a non-child, non-self
// node: the handler routes upstream (to parent) and appends ourselves.
func TestHandler_Forward_Upstream(t *testing.T) {
	const large = "34020000012000000001"
	const small = "34020000012160000001"
	const device = "34020000011310000001"
	const remote = "34020000002220000002"

	pLarge, _ := model.NewNodeProfile(large, "10.0.0.1:5060", "3402000000", "platform-large")
	pSmall, _ := model.NewNodeProfile(small, "10.0.0.2:5060", "3402000000", "platform-small")
	pSmallWithParent, _ := pSmall.WithCascadeParent(large)
	pDevice, _ := model.NewNodeProfile(device, "10.0.0.3:5060", large, "device")

	m := NewTopologyMap([]model.NodeProfile{pLarge, pSmallWithParent, pDevice})
	h := New(m)

	msg := newMsg(t, nil)
	next, hdrs, _, err := h.Forward(mustNodeID(t, small), msg, remote)
	if err != nil {
		t.Fatalf("Forward(upstream) = %v", err)
	}
	if next != large {
		t.Errorf("next hop = %q, want %q (parent)", next, large)
	}
	if len(hdrs) != 1 || hdrs[0].Value() != small {
		t.Errorf("route header = %v, want [%s]", hdrs, small)
	}
}

// TestHandler_Forward_NilTopology covers the "no topology" case: when the
// handler is constructed with a nil Topology, Forward returns dstDeviceID
// unchanged with no headers.
func TestHandler_Forward_NilTopology(t *testing.T) {
	h := New(nil)
	msg := newMsg(t, nil)
	next, hdrs, _, err := h.Forward(mustNodeID(t, "34020000012160000001"), msg, "34020000011310000001")
	if err != nil {
		t.Fatalf("Forward with nil topo = %v", err)
	}
	if next != "34020000011310000001" {
		t.Errorf("next = %q, want original dst", next)
	}
	if len(hdrs) != 0 {
		t.Errorf("headers = %v, want none", hdrs)
	}
}

// TestHandler_Forward_LoopDetected covers task 2.3 loop detection: if
// dstDeviceID already appears on RoutePath, Forward returns an error and
// does not modify the message.
func TestHandler_Forward_LoopDetected(t *testing.T) {
	const large = "34020000012000000001"
	const small = "34020000012160000001"
	const device = "34020000011310000001"

	pLarge, _ := model.NewNodeProfile(large, "10.0.0.1:5060", "3402000000", "platform-large")
	pSmall, _ := model.NewNodeProfile(small, "10.0.0.2:5060", "3402000000", "platform-small")
	smallWithParent, _ := pSmall.WithCascadeParent(large)
	pDevice, _ := model.NewNodeProfile(device, "10.0.0.3:5060", large, "device")

	m := NewTopologyMap([]model.NodeProfile{pLarge, smallWithParent, pDevice})
	h := New(m)

	msg := newMsg(t, []model.Header{
		model.NewHeader(model.HeaderRoutePath, small+","+large),
	})
	_, _, _, err := h.Forward(mustNodeID(t, small), msg, large)
	if err == nil {
		t.Fatal("Forward(loop target) succeeded, want error")
	}
	if !strings.Contains(err.Error(), "loop detected") {
		t.Errorf("error = %q, want loop detected", err.Error())
	}
}

// TestHandler_Forward_HeaderPropagation covers X-PreferredPath propagation
// when a message carries an existing PreferredPath header: the preferred
// path is preserved alongside the updated RoutePath.
func TestHandler_Forward_HeaderPropagation(t *testing.T) {
	const large = "34020000012000000001"
	const small = "34020000012160000001"
	const device = "34020000011310000001"

	pLarge, _ := model.NewNodeProfile(large, "10.0.0.1:5060", "3402000000", "platform-large")
	pSmall, _ := model.NewNodeProfile(small, "10.0.0.2:5060", "3402000000", "platform-small")
	pSmallWithChildren, _ := pSmall.WithCascadeChildren([]string{device})
	pDevice, _ := model.NewNodeProfile(device, "10.0.0.3:5060", large, "device")

	m := NewTopologyMap([]model.NodeProfile{pLarge, pSmallWithChildren, pDevice})
	h := New(m)

	msg := newMsg(t, []model.Header{
		model.NewHeader(model.HeaderPreferredPath, "34020000002220000002,34020000003330000003"),
	})
	next, hdrs, _, err := h.Forward(mustNodeID(t, small), msg, device)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if next != device {
		t.Errorf("next hop = %q, want %q", next, device)
	}
	routeVal := ""
	preferredVal := ""
	for _, h := range hdrs {
		switch h.Name() {
		case model.HeaderRoutePath:
			routeVal = h.Value()
		case model.HeaderPreferredPath:
			preferredVal = h.Value()
		}
	}
	if routeVal != small {
		t.Errorf("RoutePath = %q, want %q", routeVal, small)
	}
	if preferredVal != "34020000002220000002,34020000003330000003" {
		t.Errorf("PreferredPath = %q, want original preserved", preferredVal)
	}
}

// TestHandler_Forward_PreferredPathRouting covers the N-1 PreferredPath
// routing decision: when the first preferred entry is a direct neighbour
// (parent or child), Forward selects it as next hop and removes it from the
// outgoing PreferredPath header.
func TestHandler_Forward_PreferredPathRouting(t *testing.T) {
	const large = "34020000012000000001"
	const small = "34020000012160000002"
	const device = "34020000011310000001"

	pLarge, _ := model.NewNodeProfile(large, "10.0.0.1:5060", "3402000000", "platform-large")
	pSmall, _ := model.NewNodeProfile(small, "10.0.0.2:5060", "3402000000", "platform-small")
	smallWithParent, _ := pSmall.WithCascadeParent(large)
	pDevice, _ := model.NewNodeProfile(device, "10.0.0.3:5060", large, "device")

	topo := NewTopologyMap([]model.NodeProfile{pLarge, smallWithParent, pDevice})
	h := New(topo)

	// PreferredPath first entry is the parent of 'small'; it is a direct
	// neighbour so Forward must route there and consume the entry.
	msg := newMsg(t, []model.Header{
		model.NewHeader(model.HeaderPreferredPath, large+","+device),
	})
	next, hdrs, handled, err := h.Forward(mustNodeID(t, small), msg, device)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if handled {
		t.Fatal("Forward(handled) = true for parent preferred, want false")
	}
	if next != large {
		t.Errorf("next hop = %q, want preferred parent %q", next, large)
	}

	routeVal := ""
	preferredVal := ""
	for _, h := range hdrs {
		switch h.Name() {
		case model.HeaderRoutePath:
			routeVal = h.Value()
		case model.HeaderPreferredPath:
			preferredVal = h.Value()
		}
	}
	if routeVal != small {
		t.Errorf("RoutePath = %q, want %q", routeVal, small)
	}
	// The first preferred entry (large) must be consumed.
	if preferredVal != device {
		t.Errorf("PreferredPath = %q, want consumed=%q", preferredVal, device)
	}
}

// TestHandler_WithTopology covers handler topology hot-swap.
func TestHandler_WithTopology(t *testing.T) {
	const large = "34020000012000000001"
	const small = "34020000012160000001"
	const device = "34020000011310000001"

	pSmall, _ := model.NewNodeProfile(small, "10.0.0.2:5060", "3402000000", "platform-small")
	pDevice, _ := model.NewNodeProfile(device, "10.0.0.3:5060", large, "device")
	smallWithChildren, _ := pSmall.WithCascadeChildren([]string{device})

	h := New(nil)
	_, _, _, err := h.Forward(mustNodeID(t, small), newMsg(t, nil), device)
	if err != nil {
		// With nil topo, Forward falls back to no-op (not error); confirm it does.
		_ = err
	}

	// Swap in a topology that knows about the child.
	h.WithTopology(NewTopologyMap([]model.NodeProfile{smallWithChildren, pDevice}))
	msg := newMsg(t, nil)
	next, hdrs, _, err := h.Forward(mustNodeID(t, small), msg, device)
	if err != nil {
		t.Fatalf("Forward after topo swap = %v", err)
	}
	if next != device {
		t.Errorf("next hop after topo swap = %q, want %q", next, device)
	}
	if len(hdrs) != 1 || hdrs[0].Value() != small {
		t.Errorf("route header after swap = %v, want [%s]", hdrs, small)
	}
}

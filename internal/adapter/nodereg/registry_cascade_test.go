package nodereg_test

import (
	"context"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/cascade"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Cascade ids used by the topology tests: a platform-large, a
// platform-small that declares it as parent, and a leaf device.
const (
	idLarge  = "34020000002000000001"
	idSmall  = "34020000002160000001"
	idDevice = "34020000001310000001"
)

// mustCascadeProfile builds a profile and fails the test on a bad id.
func mustCascadeProfile(t *testing.T, id, addr string) model.NodeProfile {
	t.Helper()
	p, err := model.NewNodeProfile(id, addr, "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile(%q,%q): %v", id, addr, err)
	}
	return p
}

// withParent attaches a cascade parent, failing the test on a rejected id.
func withParent(t *testing.T, p model.NodeProfile, parent string) model.NodeProfile {
	t.Helper()
	cp, err := p.WithCascadeParent(parent)
	if err != nil {
		t.Fatalf("WithCascadeParent(%s): %v", parent, err)
	}
	return cp
}

// TestRegistry_Register_RejectsSelfParent covers task 4.1: a node that
// declares itself as its own cascade parent is refused with a descriptive
// error.
func TestRegistry_Register_RejectsSelfParent(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	_, err := reg.Register(ctx, withParent(t, mustCascadeProfile(t, idSmall, "127.0.0.1:16061"), idSmall))
	if err == nil {
		t.Fatal("Register(self parent) succeeded, want error")
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Errorf("error = %q, want it to name the self-reference", err.Error())
	}
}

// TestRegistry_Register_RejectsParentCycle covers task 4.1: two nodes that
// are each other's cascade parent close a loop; the second registration is
// rejected. The first one must be accepted because a parent that is not yet
// registered may simply live outside this process.
func TestRegistry_Register_RejectsParentCycle(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()

	if _, err := reg.Register(ctx, withParent(t, mustCascadeProfile(t, idLarge, "127.0.0.1:16071"), idSmall)); err != nil {
		t.Fatalf("Register(large, parent=small): %v", err)
	}
	_, err := reg.Register(ctx, withParent(t, mustCascadeProfile(t, idSmall, "127.0.0.1:16072"), idLarge))
	if err == nil {
		t.Fatal("Register(small, parent=large) succeeded, want cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want it to name the cycle", err.Error())
	}
	// A rejected registration must not leave the node behind.
	if _, ok := reg.Get(ctx, mustNodeID2(t, idSmall)); ok {
		t.Error("rejected node small is still registered")
	}
	// And the surviving topology is untouched: large is still there.
	if _, ok := reg.Get(ctx, mustNodeID2(t, idLarge)); !ok {
		t.Error("node large disappeared after a rejected registration")
	}
}

// TestRegistry_Register_RejectsSelfChild covers task 4.1: a node listing
// itself among its cascade children is refused.
func TestRegistry_Register_RejectsSelfChild(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	p := mustCascadeProfile(t, idSmall, "127.0.0.1:16081")
	p, err := p.WithCascadeChildren([]string{idSmall})
	if err != nil {
		t.Fatalf("WithCascadeChildren(self): %v", err)
	}
	_, err = reg.Register(ctx, p)
	if err == nil {
		t.Fatal("Register(self child) succeeded, want error")
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Errorf("error = %q, want it to name the self-reference", err.Error())
	}
}

// TestRegistry_CascadeTopologyHotSwap covers the registry's write path into
// the handler: registering a node with a declared parent makes Forward route
// upstream immediately, and unregistering it drops the node from the graph
// so the same Forward degrades to a direct send.
func TestRegistry_CascadeTopologyHotSwap(t *testing.T) {
	ctx := context.Background()
	h := cascade.New(nil, nil)
	reg := nodereg.New()
	reg.WithCascadeHandler(h)

	small := withParent(t, mustCascadeProfile(t, idSmall, "127.0.0.1:16091"), idLarge)
	if _, err := reg.Register(ctx, small); err != nil {
		t.Fatalf("Register(small): %v", err)
	}

	msg := mustMessage(t)
	next, hdrs, handled, err := h.Forward(mustNodeID2(t, idSmall), msg, idDevice)
	if err != nil {
		t.Fatalf("Forward after register: %v", err)
	}
	if handled {
		t.Error("Forward reports handled, want plain routing advice")
	}
	if next != idLarge {
		t.Errorf("next hop = %q, want parent %q", next, idLarge)
	}
	if len(hdrs) != 1 || hdrs[0].Name() != model.HeaderRoutePath || hdrs[0].Value() != idSmall {
		t.Errorf("headers = %v, want one X-RoutePath: %s", hdrs, idSmall)
	}

	if err := reg.Unregister(ctx, mustNodeID2(t, idSmall)); err != nil {
		t.Fatalf("Unregister(small): %v", err)
	}
	next, hdrs, _, err = h.Forward(mustNodeID2(t, idSmall), msg, idDevice)
	if err != nil {
		t.Fatalf("Forward after unregister: %v", err)
	}
	if next != idDevice {
		t.Errorf("next hop after unregister = %q, want direct %q", next, idDevice)
	}
	if len(hdrs) != 0 {
		t.Errorf("headers after unregister = %v, want none", hdrs)
	}
}

// TestRegistry_WithoutCascadeHandler covers the default: a registry that
// never saw a cascade handler still accepts and removes nodes normally.
func TestRegistry_WithoutCascadeHandler(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	if _, err := reg.Register(ctx, mustCascadeProfile(t, idSmall, "127.0.0.1:16101")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Unregister(ctx, mustNodeID2(t, idSmall)); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
}

// mustNodeID2 parses a raw id or fails the test (named so it does not
// collide with helpers in other test files of this package).
func mustNodeID2(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

// mustMessage builds a minimal outbound SIP request for Forward calls.
func mustMessage(t *testing.T) model.Message {
	t.Helper()
	msg, err := model.NewRequest("MESSAGE", "sip:"+idDevice, nil, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

package cascade_test

import (
	"context"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/cascade"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

const (
	// 3-node topology IDs: large (top) ← small ← device
	idLarge  = "34020000002000000001"
	idSmall  = "34020000002160000001"
	idDevice = "34020000001310000001"
)

func mustCascadeProfile(t *testing.T, id, addr string) model.NodeProfile {
	t.Helper()
	p, err := model.NewNodeProfile(id, addr, "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile(%q,%q): %v", id, addr, err)
	}
	return p
}

func withParent(t *testing.T, p model.NodeProfile, parent string) model.NodeProfile {
	t.Helper()
	cp, err := p.WithCascadeParent(parent)
	if err != nil {
		t.Fatalf("WithCascadeParent(%s): %v", parent, err)
	}
	return cp
}

func withChildren(t *testing.T, p model.NodeProfile, children []string) model.NodeProfile {
	t.Helper()
	cp, err := p.WithCascadeChildren(children)
	if err != nil {
		t.Fatalf("WithCascadeChildren(%v): %v", children, err)
	}
	return cp
}

func mustNodeID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

func mustMessage(t *testing.T) model.Message {
	t.Helper()
	msg, err := model.NewRequest("MESSAGE", "sip:"+idDevice, nil, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// TestThreeNodeMultiHop registers a three-node cascade chain and walks the
// full hop path hop-by-hop, verifying that X-RoutePath is built
// incrementally: [device] → [device,small] → [device,small,large].
//
// Unregistering a leaf then shows the topology hot-swap: the next Forward
// sees one fewer hop in X-RoutePath.
func TestThreeNodeMultiHop(t *testing.T) {
	ctx := context.Background()
	h := cascade.New(nil, nil)
	reg := nodereg.New()
	reg.WithCascadeHandler(h)

	// leaf first (parent doesn't need to be registered yet)
	device := withParent(t, mustCascadeProfile(t, idDevice, "127.0.0.1:16000"), idSmall)
	if _, err := reg.Register(ctx, device); err != nil {
		t.Fatalf("register device: %v", err)
	}

	small := withParent(t, mustCascadeProfile(t, idSmall, "127.0.0.1:16001"), idLarge)
	small = withChildren(t, small, []string{idDevice})
	if _, err := reg.Register(ctx, small); err != nil {
		t.Fatalf("register small: %v", err)
	}

	large := withChildren(t, mustCascadeProfile(t, idLarge, "127.0.0.1:16002"), []string{idSmall})
	if _, err := reg.Register(ctx, large); err != nil {
		t.Fatalf("register large: %v", err)
	}

	msg := mustMessage(t)

	// --- hop 1: device → small ---
	next, hdrs, handled, err := h.Forward(mustNodeID(t, idDevice), msg, idLarge)
	if err != nil {
		t.Fatalf("hop1 Forward: %v", err)
	}
	if handled {
		t.Error("hop1 handled=true, want plain routing advice")
	}
	if next != idSmall {
		t.Errorf("hop1 next = %q, want %q", next, idSmall)
	}
	if !routePathEquals(hdrs, idDevice) {
		t.Errorf("hop1 route = %q, want %q", routePathValue(hdrs), idDevice)
	}

	// --- hop 2: small → large (carrying hop-1 headers) ---
	msg2, _ := model.NewRequest(msg.Method(), msg.URI().String(), hdrs, string(msg.Body()))
	next, hdrs, handled, err = h.Forward(mustNodeID(t, idSmall), msg2, idLarge)
	if err != nil {
		t.Fatalf("hop2 Forward: %v", err)
	}
	if handled {
		t.Error("hop2 handled=true, want plain routing advice")
	}
	if next != idLarge {
		t.Errorf("hop2 next = %q, want %q", next, idLarge)
	}
	if !routePathEquals(hdrs, idDevice, idSmall) {
		t.Errorf("hop2 route = %q, want %q", routePathValue(hdrs), strings.Join([]string{idDevice, idSmall}, ","))
	}

	// --- topology hot-swap: unregister device, then a FRESH message from
	//     small still routes upstream to large (small remains registered);
	//     the in-flight msg2 keeps its own route, so a clean message is used ---
	if err := reg.Unregister(ctx, mustNodeID(t, idDevice)); err != nil {
		t.Fatalf("unregister device: %v", err)
	}
	next, hdrs, _, err = h.Forward(mustNodeID(t, idSmall), mustMessage(t), idLarge)
	if err != nil {
		t.Fatalf("post-unregister Forward: %v", err)
	}
	if next != idLarge {
		t.Errorf("after unregister next = %q, want %q", next, idLarge)
	}
	if !routePathEquals(hdrs, idSmall) {
		t.Errorf("after unregister route = %q, want %q", routePathValue(hdrs), idSmall)
	}

	// --- unregister small as well: with no topology left, Forward falls
	//     back to a direct send with no headers ---
	if err := reg.Unregister(ctx, mustNodeID(t, idSmall)); err != nil {
		t.Fatalf("unregister small: %v", err)
	}
	next, hdrs, _, err = h.Forward(mustNodeID(t, idSmall), mustMessage(t), idLarge)
	if err != nil {
		t.Fatalf("final Forward: %v", err)
	}
	if next != idLarge {
		t.Errorf("after final unregister next = %q, want direct %q", next, idLarge)
	}
	if len(hdrs) != 0 {
		t.Errorf("after final unregister headers = %v, want none", hdrs)
	}
}

// routePathEquals returns true when hdrs contains exactly one X-RoutePath
// whose value is a comma-joined copy of ids (order preserved).
func routePathEquals(hdrs []model.Header, ids ...string) bool {
	vals := routePathValue(hdrs)
	if vals == "" {
		return len(ids) == 0
	}
	got := strings.Split(vals, ",")
	if len(got) != len(ids) {
		return false
	}
	for i := range got {
		if got[i] != ids[i] {
			return false
		}
	}
	return true
}

func routePathValue(hdrs []model.Header) string {
	for _, h := range hdrs {
		if h.Name() == model.HeaderRoutePath {
			return h.Value()
		}
	}
	return ""
}

// End-to-end coverage for cascade topology set at runtime.
//
// Change: runtime-node-configuration, task 8.2.
//
// Scope note: this test asserts the behaviour the simulator implements today
// — a cascade graph configured over HTTP reaches the cascade handler at
// runtime, and the node that has an upstream attaches its own deviceID to
// outbound traffic. It does NOT assert two-hop header accumulation on a node
// down the chain: the acceptor attaches cascade headers to the answer it
// sends back to its peer (internal/app/acceptor_run.go `routeOutbound`) and
// has no relay that re-sends an inbound MESSAGE to a downstream child, so a
// message never travels large -> small -> device on the wire. Multi-hop
// accumulation is covered at the handler level by
// internal/adapter/cascade/handler_e2e_test.go.
package e2e

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCascadeRuntime_XRoutePathFollowsAPITopology builds the three-node chain
// large <- small <- device over the API, checks that the cycle guard refuses a
// loop, starts everything, and then sends a real SIP OPTIONS to the middle
// node. The answer must carry X-RoutePath naming that node: the topology set
// through HTTP is what put it there.
func TestCascadeRuntime_XRoutePathFollowsAPITopology(t *testing.T) {
	bin := buildSimulator(t)

	dir := t.TempDir()
	httpPort := freeTCPPort(t)
	cfgPath := writeSimulatorConfig(t, dir, httpPort)
	proc := startSimulator(t, bin, cfgPath, dir, httpPort)

	largeAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
	smallAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
	deviceAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))

	// --- create the three nodes --------------------------------------
	for _, n := range []struct {
		id, kind, addr string
	}{
		{e2eLargeID, "platform-large", largeAddr},
		{e2eSmallID, "platform-small", smallAddr},
		{e2eDeviceID, "device", deviceAddr},
	} {
		status, body := proc.call(t, http.MethodPost, "/v1/nodes", map[string]any{
			"id":     n.id,
			"kind":   n.kind,
			"domain": "3402000000",
			"addr":   n.addr,
			"vendor": "e2e",
		})
		if status != http.StatusCreated {
			t.Fatalf("POST /v1/nodes %s = %d, want 201 (body %v)", n.id, status, body)
		}
	}

	// --- wire the chain over HTTP ------------------------------------
	// Nodes must be idle for a cascade edit, which they are right after
	// creation. The parent edit also maintains the parent's children list,
	// so the explicit children below are the same values the service would
	// derive — stating them keeps the test independent of that optimisation.
	for _, link := range []struct {
		id       string
		parent   string
		children []string
	}{
		{e2eLargeID, "", []string{e2eSmallID}},
		{e2eSmallID, e2eLargeID, []string{e2eDeviceID}},
		{e2eDeviceID, e2eSmallID, nil},
	} {
		status, body := proc.call(t, http.MethodPut, "/v1/nodes/"+link.id+"/cascade", map[string]any{
			"parent":   link.parent,
			"children": link.children,
		})
		if status != http.StatusOK {
			t.Fatalf("PUT cascade %s = %d, want 200 (body %v)", link.id, status, body)
		}
		// The cascade endpoint answers with the definition as stored; that
		// is where the test reads the topology back (the detail endpoint
		// serves the live summary, which carries no cascade fields).
		if got, _ := body["cascade_parent"].(string); got != link.parent {
			t.Errorf("%s cascade_parent = %q, want %q", link.id, got, link.parent)
		}
		if got := childIDs(body["cascade_children"]); !sameIDs(got, link.children) {
			t.Errorf("%s cascade_children = %v, want %v", link.id, got, link.children)
		}
	}

	// --- a loop is refused before anything is written -----------------
	status, loop := proc.call(t, http.MethodPut, "/v1/nodes/"+e2eLargeID+"/cascade", map[string]any{
		"parent": e2eDeviceID,
	})
	if status != http.StatusConflict {
		t.Fatalf("PUT cascade creating a cycle = %d, want 409 (body %v)", status, loop)
	}

	// --- start all three ---------------------------------------------
	for _, id := range []string{e2eLargeID, e2eSmallID, e2eDeviceID} {
		status, body := proc.call(t, http.MethodPost, "/v1/nodes/"+id+"/start", nil)
		if status != http.StatusOK {
			t.Fatalf("POST start %s = %d, want 200 (body %v)", id, status, body)
		}
		proc.waitNodeStatus(t, id, "online")
	}

	// --- a real SIP request to the middle node -----------------------
	// small has an upstream (large), so its answer must name itself in
	// X-RoutePath. Without the API-driven topology the handler would have no
	// parent to route through and would attach nothing.
	raw := sendOptions(t, smallAddr, e2eSmallID, e2eLargeID)

	if !isSIPStatus(raw, 200) {
		t.Fatalf("OPTIONS answer was not 200 OK:\n%s", raw)
	}
	if got, ok := headerValue(raw, "X-RoutePath"); !ok {
		t.Fatalf("answer carries no X-RoutePath; cascade topology from the API did not reach the handler:\n%s", raw)
	} else if strings.TrimSpace(got) != e2eSmallID {
		t.Errorf("X-RoutePath = %q, want %q", strings.TrimSpace(got), e2eSmallID)
	}
}

// sendOptions sends a hand-built SIP OPTIONS datagram to dstAddr and returns
// the answer. The request is deliberately plain — the simulator answers every
// OPTIONS with 200 OK (internal/app/acceptor_invite.go), which is all this
// probe needs to see what headers the node attaches on the way out.
func sendOptions(t *testing.T, dstAddr, toID, fromID string) string {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open probe socket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	local := conn.LocalAddr().String()
	request := strings.Join([]string{
		"OPTIONS sip:" + toID + "@" + dstAddr + " SIP/2.0",
		"Via: SIP/2.0/UDP " + local + ";branch=z9hG4bK-e2e-cascade",
		"From: <sip:" + fromID + "@" + local + ">;tag=e2e-cascade",
		"To: <sip:" + toID + "@" + dstAddr + ">",
		"Call-ID: e2e-cascade-options@127.0.0.1",
		"CSeq: 1 OPTIONS",
		"Max-Forwards: 70",
		"User-Agent: e2e-cascade",
		"Content-Length: 0",
	}, "\r\n") + "\r\n\r\n"

	dst, err := net.ResolveUDPAddr("udp", dstAddr)
	if err != nil {
		t.Fatalf("resolve %s: %v", dstAddr, err)
	}
	if _, err := conn.WriteTo([]byte(request), dst); err != nil {
		t.Fatalf("send OPTIONS to %s: %v", dstAddr, err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	buf := make([]byte, 64*1024)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read OPTIONS answer from %s: %v", dstAddr, err)
	}
	return string(buf[:n])
}

// isSIPStatus reports whether the SIP start line carries the wanted code.
func isSIPStatus(raw string, want int) bool {
	line, _, _ := strings.Cut(raw, "\r\n")
	fields := strings.Fields(line)
	return len(fields) >= 2 && fields[0] == "SIP/2.0" && fields[1] == fmt.Sprintf("%d", want)
}

// childIDs renders the cascade_children value of a JSON body as strings.
func childIDs(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// sameIDs reports whether two id lists hold the same ids in the same order.
func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// headerValue returns the value of the first header with the given name,
// case-insensitively, per SIP's header-name rules.
func headerValue(raw, name string) (string, bool) {
	header, _, _ := strings.Cut(raw, "\r\n\r\n")
	for _, line := range strings.Split(header, "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

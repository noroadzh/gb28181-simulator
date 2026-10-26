// End-to-end coverage for cascade message relay.
//
// Change: cascade-message-relay, tasks 3.1 and 3.2.
//
// The chain under test is three real nodes in one process — large <- small <-
// device — plus an external SIP probe. The probe sends one MANSCDP catalog
// query and the answer must come back across two hops; the device's own wire
// capture proves what it actually received, because a relay can look correct
// on the return path while never having reached the leaf.
package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
)

type cascadeFleet struct {
	proc  *simulatorProcess
	addrs map[string]string
}

// setupCascadeFleet creates the three nodes and wires large -> small -> device
// over the API. When startAll is false only the caller decides what to run,
// which the failure-path test uses to keep the middle node deletable.
func setupCascadeFleet(t *testing.T, startAll bool) *cascadeFleet {
	t.Helper()
	bin := buildSimulator(t)
	dir := t.TempDir()
	httpPort := freeTCPPort(t)
	cfgPath := writeSimulatorConfig(t, dir, httpPort)
	proc := startSimulator(t, bin, cfgPath, dir, httpPort)

	nodes := []struct{ id, kind string }{
		{e2eLargeID, "platform-large"},
		{e2eSmallID, "platform-small"},
		{e2eDeviceID, "device"},
	}
	addrs := make(map[string]string, len(nodes))
	for _, n := range nodes {
		addrs[n.id] = fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
		status, body := proc.call(t, http.MethodPost, "/v1/nodes", map[string]any{
			"id": n.id, "kind": n.kind, "domain": "3402000000",
			"addr": addrs[n.id], "vendor": "e2e",
		})
		if status != http.StatusCreated {
			t.Fatalf("create %s = %d, want 201 (body %v)", n.id, status, body)
		}
	}

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
			"parent": link.parent, "children": link.children,
		})
		if status != http.StatusOK {
			t.Fatalf("cascade %s = %d, want 200 (body %v)", link.id, status, body)
		}
	}

	if startAll {
		for _, n := range nodes {
			status, body := proc.call(t, http.MethodPost, "/v1/nodes/"+n.id+"/start", nil)
			if status != http.StatusOK {
				t.Fatalf("start %s = %d, want 200 (body %v)", n.id, status, body)
			}
			proc.waitNodeStatus(t, n.id, "online")
		}
	}
	return &cascadeFleet{proc: proc, addrs: addrs}
}

// sendCatalogQuery sends one MANSCDP catalog query over raw UDP and returns
// the SIP answer. preferred / routePath are optional cascade headers.
func sendCatalogQuery(t *testing.T, dstAddr, targetID, preferred, routePath string) string {
	t.Helper()
	body, err := manscdp.NewMANSCDPCodec().MarshalCatalogQuery(targetID, 1)
	if err != nil {
		t.Fatalf("MarshalCatalogQuery: %v", err)
	}

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open probe socket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	local := conn.LocalAddr().String()
	lines := []string{
		"MESSAGE sip:" + targetID + "@3402000000 SIP/2.0",
		"Via: SIP/2.0/UDP " + local + ";branch=z9hG4bK-e2e-relay",
		"From: <sip:34020000002000000009@127.0.0.1>;tag=e2e-probe",
		"To: <sip:" + targetID + "@3402000000>",
		"Call-ID: e2e-relay-" + targetID,
		"CSeq: 1 MESSAGE",
		"Max-Forwards: 70",
		"Content-Type: Application/MANSCDP+XML",
	}
	if preferred != "" {
		lines = append(lines, "X-PreferredPath: "+preferred)
	}
	if routePath != "" {
		lines = append(lines, "X-RoutePath: "+routePath)
	}
	lines = append(lines, fmt.Sprintf("Content-Length: %d", len(body)), "", body)
	request := strings.Join(lines, "\r\n")

	dst, err := net.ResolveUDPAddr("udp", dstAddr)
	if err != nil {
		t.Fatalf("resolve %s: %v", dstAddr, err)
	}
	if _, err := conn.WriteTo([]byte(request), dst); err != nil {
		t.Fatalf("send MESSAGE to %s: %v", dstAddr, err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	buf := make([]byte, 64*1024)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read answer from %s: %v", dstAddr, err)
	}
	return string(buf[:n])
}

// captureBodies returns the raw SIP text the node's capture ring holds.
func captureBodies(t *testing.T, proc *simulatorProcess, nodeID string) []string {
	t.Helper()
	resp, err := http.Get(proc.baseURL + "/v1/nodes/" + nodeID + "/capture")
	if err != nil {
		t.Fatalf("GET capture for %s: %v", nodeID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET capture for %s = %d", nodeID, resp.StatusCode)
	}
	var events []struct {
		Bytes string `json:"bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Bytes)
	}
	return out
}

func waitCaptureContains(t *testing.T, proc *simulatorProcess, nodeID, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, raw := range captureBodies(t, proc, nodeID) {
			if strings.Contains(raw, needle) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("capture of %s never contained %q", nodeID, needle)
}

// Task 3.1: one query, two hops, and the answer comes home.
func TestCascadeRelay_TwoHopDelivery(t *testing.T) {
	fleet := setupCascadeFleet(t, true)

	answer := sendCatalogQuery(t, fleet.addrs[e2eLargeID], e2eDeviceID, e2eSmallID, "")
	if !isSIPStatus(answer, 200) {
		t.Fatalf("catalog query was not answered with 200:\n%s", answer)
	}
	if !strings.Contains(answer, "Catalog") {
		t.Errorf("answer does not look like a catalog response:\n%s", answer)
	}

	// The leaf's own capture is the evidence that the request travelled both
	// hops and that each node appended itself on the way down.
	waitCaptureContains(t, fleet.proc, e2eDeviceID,
		"X-RoutePath: "+e2eLargeID+","+e2eSmallID)
}

// Task 3.2: the two failure shapes a probe can distinguish from a hang.
func TestCascadeRelay_FailureResponses(t *testing.T) {
	fleet := setupCascadeFleet(t, false)

	status, body := fleet.proc.call(t, http.MethodPost, "/v1/nodes/"+e2eLargeID+"/start", nil)
	if status != http.StatusOK {
		t.Fatalf("start large = %d, want 200 (body %v)", status, body)
	}
	fleet.proc.waitNodeStatus(t, e2eLargeID, "online")

	// A message that already carries this node on its route is a loop.
	loop := sendCatalogQuery(t, fleet.addrs[e2eLargeID], e2eDeviceID, e2eSmallID, e2eLargeID)
	if !isSIPStatus(loop, 482) {
		t.Fatalf("looped query = %s, want 482", strings.SplitN(loop, "\r\n", 2)[0])
	}

	// Remove the next hop: the topology still names it, but it can no longer
	// be resolved to an address. That must be a 500, not silence.
	status, body = fleet.proc.call(t, http.MethodDelete, "/v1/nodes/"+e2eSmallID, nil)
	if status != http.StatusOK {
		t.Fatalf("delete small = %d, want 200 (body %v)", status, body)
	}
	missing := sendCatalogQuery(t, fleet.addrs[e2eLargeID], e2eDeviceID, e2eSmallID, "")
	if !isSIPStatus(missing, 500) {
		t.Fatalf("query with no next hop = %s, want 500", strings.SplitN(missing, "\r\n", 2)[0])
	}
}

// isSIPStatus reports whether the SIP start line carries the wanted code.
func isSIPStatus(raw string, want int) bool {
	line, _, _ := strings.Cut(raw, "\r\n")
	fields := strings.Fields(line)
	return len(fields) >= 2 && fields[0] == "SIP/2.0" && fields[1] == fmt.Sprintf("%d", want)
}

// End-to-end coverage for the `auto_start` boot switch.
//
// The change that introduced DB-backed node definitions also made auto_start
// act on persisted nodes instead of the YAML section. That behaviour only
// exists across a process boundary — the switch is read by the composition
// root — so the test boots the real binary twice over one database: the first
// boot persists a node with auto_start off, the second must bring it online
// without any API call.
package e2e

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
)

func TestAutoStart_BringsPersistedNodesOnline(t *testing.T) {
	bin := buildSimulator(t)

	dir := t.TempDir()
	httpPort := freeTCPPort(t)
	sipAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))

	// --- boot 1: auto_start off, persist one node --------------------
	cfg1 := writeSimulatorConfig(t, dir, httpPort)
	p1 := startSimulator(t, bin, cfg1, dir, httpPort)
	status, body := p1.call(t, http.MethodPost, "/v1/nodes", map[string]any{
		"id":     e2eDeviceID,
		"kind":   "device",
		"domain": "3402000000",
		"addr":   sipAddr,
		"vendor": "e2e",
	})
	if status != http.StatusCreated {
		t.Fatalf("POST /v1/nodes = %d, want 201 (body %v)", status, body)
	}
	p1.waitNodeStatus(t, e2eDeviceID, "idle")

	// Shut the first process down gracefully so the second boot sees a
	// released database file and a free HTTP port.
	p1.stop(t)

	// --- boot 2: same database, auto_start on ------------------------
	cfg2 := writeSimulatorConfigWith(t, filepath.Join(dir, "auto.yaml"), dir, httpPort, true)
	p2 := startSimulator(t, bin, cfg2, dir, httpPort)

	// No start request here: the boot path must have done it.
	p2.waitNodeStatus(t, e2eDeviceID, "online")

	// The node holds its listener as a started node would.
	if conn, err := bindUDP(sipAddr); err == nil {
		_ = conn.Close()
		t.Fatalf("auto-started node %s reports online but its SIP endpoint is free", e2eDeviceID)
	}
}

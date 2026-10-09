// Package e2e hosts lightweight end-to-end smoke tests that exercise the
// HTTP and WebSocket surface of the assembled server.
//
// Change: fix-problems-and-smoke-deploy-docs, task 3.1.
// Purpose: provide a fast (sub-second) probe used by scripts/smoke.sh step 6 to
// verify that the binaries produced by steps 1-5 boot a server that responds
// to /healthz, /v1/version, /v1/logs/stream, and the per-node faults endpoint
// the way the dashboard and operator scripts expect.
//
// These tests deliberately avoid SIP/SDP/PS protocol work: that coverage lives
// in the per-capability golden suites under internal/adapter and internal/app.
// The intent here is "does the binary boot and serve the operator surface?"

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// A representative 20-digit GB28181 node id used by the faults-endpoint probe.
// Real deployment ids follow this length rule (see project-skeleton spec §4).
const smokeNodeID = "34020000011310000001"

// newServer wires the production httpapi.Server behind an httptest server so
// tests can hit it over the loopback interface without binding a real port.
// It returns both the httptest server and the underlying hub so WebSocket
// probes can publish to the same hub the server subscribes to.
func newServer(t *testing.T) (*httptest.Server, *logging.Hub) {
	t.Helper()
	hub := logging.NewHub(16)
	srv := httpapi.NewServer(
		platformconfig.Config{},
		hub,
		httpapi.Version{Version: "smoke", Commit: "test", BuiltAt: "now"},
		nil, // NodeView - nil is allowed; node endpoints return 501
		nil, // ScenarioRunner - same
		nil, // ChannelView - same
		nil, // AccountAdmin - nil is allowed; account endpoints return 501
		nil, // *StreamingServer - tests don't use streaming
	)
	ts := httptest.NewServer(srv.Echo())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Shutdown(context.Background())
	})
	return ts, srv.Hub()
}

// TestSmokeHealthz verifies the legacy /healthz endpoint responds with the
// "status: ok" JSON body that operator scripts and the dashboard expect.
func TestSmokeHealthz(t *testing.T) {
	ts, _ := newServer(t)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body.status = %q, want ok", body["status"])
	}
}

// TestSmokeVersion verifies /v1/version returns the build metadata injected by
// the CLI entrypoint so monitoring and rollout checks have something to read.
func TestSmokeVersion(t *testing.T) {
	ts, _ := newServer(t)
	resp, err := http.Get(ts.URL + "/v1/version")
	if err != nil {
		t.Fatalf("GET /v1/version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["version"] != "smoke" {
		t.Errorf("version = %q, want smoke", body["version"])
	}
	if body["platform"] == "" {
		t.Errorf("platform missing in version body")
	}
}

// TestSmokeFaultsEndpoint verifies the fault panel endpoint responds with
// either an empty profile (200), a not-found (404), or a not-implemented (501)
// when the NodeView is nil. Anything in the 5xx range indicates a server-side
// regression we want smoke to catch before deploy.
func TestSmokeFaultsEndpoint(t *testing.T) {
	ts, _ := newServer(t)
	resp, err := http.Get(ts.URL + "/v1/nodes/" + smokeNodeID + "/faults")
	if err != nil {
		t.Fatalf("GET faults: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("server error: status=%d body=%s", resp.StatusCode, string(body))
	}
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNotFound &&
		resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("unexpected status = %d for faults endpoint", resp.StatusCode)
	}
}

// TestSmokeWebSocketLogs verifies /v1/logs/stream upgrades and emits at least
// one frame within 3 seconds when the server's own hub has a publisher. The
// dashboard and the logging WebSocket sink rely on this path; a regression
// here means the UI log stream is broken even if unit tests pass.
func TestSmokeWebSocketLogs(t *testing.T) {
	ts, hub := newServer(t)

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/v1/logs/stream"
	u, err := url.Parse(wsURL)
	if err != nil {
		t.Fatalf("parse ws url: %v", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("ws dial: %v (status=%v)", err, statusOf(resp))
	}
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close() }()

	// Publish to the server's own hub (not DefaultHub) so the WebSocket
	// handler that subscribed to it actually receives a frame.
	go func() {
		time.Sleep(100 * time.Millisecond)
		hub.Publish([]byte(`{"level":"info","msg":"smoke hello"}`))
	}()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if !strings.Contains(string(msg), "smoke hello") {
		t.Errorf("frame = %q, want it to contain 'smoke hello'", string(msg))
	}
}

// statusOf converts a possibly-nil http.Response to its status string for log
// lines that fire before the response is populated.
func statusOf(resp *http.Response) string {
	if resp == nil {
		return "<nil>"
	}
	return resp.Status
}

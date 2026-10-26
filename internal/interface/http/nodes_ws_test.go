// Tests for the node-state WebSocket feed (Change runtime-node-configuration,
// task 4.1). The dashboard's status cards depend on this stream being live:
// a node change that never reaches the socket leaves the operator looking at
// a stale card until the next poll.
package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/events"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// newNodeStreamServer serves only the event feed: the REST surface is not
// what this file exercises, and nil views keep the setup minimal.
func newNodeStreamServer(t *testing.T, hub *events.Hub) (*httptest.Server, *httpapi.Server) {
	t.Helper()
	srv := httpapi.NewServer(
		platformconfig.Config{},
		logging.NewHub(4),
		httpapi.Version{Version: "test"},
		nil, nil, nil, nil, nil,
	).WithEventsHub(hub)
	ts := httptest.NewServer(srv.Echo())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Shutdown(context.Background())
	})
	return ts, srv
}

// TestNodeStatusStream_DeliversEvents proves the socket fans out hub
// publications as JSON frames with the shape the WebUI binds to.
func TestNodeStatusStream_DeliversEvents(t *testing.T) {
	hub := events.NewHub(8)
	ts, _ := newNodeStreamServer(t, hub)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/nodes/stream"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v (status %v)", wsURL, err, statusOfResponse(resp))
	}
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close() }()

	// The handler subscribes after the upgrade returns, so a single publish
	// can precede the subscription. Republishing until the client sees a
	// frame removes that race without sleeping on a fixed delay.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			hub.Publish(events.Event{
				Type:   events.TypeNodeStatusChanged,
				ID:     "34020000001310000001",
				Status: "online",
				At:     time.Now(),
			})
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, frame, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	var got events.Event
	if err := json.Unmarshal(frame, &got); err != nil {
		t.Fatalf("frame %q is not a JSON event: %v", frame, err)
	}
	if got.Type != events.TypeNodeStatusChanged {
		t.Errorf("type = %q, want %q", got.Type, events.TypeNodeStatusChanged)
	}
	if got.ID != "34020000001310000001" {
		t.Errorf("id = %q, want the published node id", got.ID)
	}
	if got.Status != "online" {
		t.Errorf("status = %q, want online", got.Status)
	}
	if got.At.IsZero() {
		t.Error("at is zero; the frame should carry the publish time")
	}
}

// Without a hub the endpoint must refuse up front rather than hold a socket
// that can never carry an event.
func TestNodeStatusStream_NoHubIs503(t *testing.T) {
	srv := httpapi.NewServer(
		platformconfig.Config{},
		logging.NewHub(4),
		httpapi.Version{Version: "test"},
		nil, nil, nil, nil, nil,
	)
	ts := httptest.NewServer(srv.Echo())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Shutdown(context.Background())
	})

	resp, err := http.Get(ts.URL + "/v1/nodes/stream")
	if err != nil {
		t.Fatalf("GET /v1/nodes/stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func statusOfResponse(resp *http.Response) string {
	if resp == nil {
		return "<nil>"
	}
	return resp.Status
}

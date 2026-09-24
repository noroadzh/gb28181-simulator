package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// fakeNodeView is a minimal NodeView that enforces the real transition
// table, so the HTTP layer's status-code mapping is exercised against the
// same rules the domain uses.
type fakeNodeView struct {
	mu    sync.Mutex
	nodes map[string]model.Node
}

func newFakeNodeView() *fakeNodeView {
	return &fakeNodeView{nodes: map[string]model.Node{}}
}

// seed registers a node and drives it to status through legal transitions.
func (f *fakeNodeView) seed(t *testing.T, id, addr string, status model.Status) model.Node {
	t.Helper()
	profile, err := model.NewNodeProfile(id, addr, "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	node := model.NewNode(profile)
	// Walk the happy path plus a final stop so Offline is reachable too.
	for _, step := range []model.Status{
		model.StatusRegistering, model.StatusRegistered,
		model.StatusOnline, model.StatusOffline,
	} {
		if node.Status() == status {
			break
		}
		next, err := node.WithStatus(step)
		if err != nil {
			t.Fatalf("seed %s to %s: %v", id, status, err)
		}
		node = next
		if node.Status() == status {
			break
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes[id] = node
	return node
}

func (f *fakeNodeView) put(n model.Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes[n.ID().String()] = n
}

func (f *fakeNodeView) List(_ context.Context) []model.Node {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		out = append(out, n)
	}
	return out
}

func (f *fakeNodeView) Get(_ context.Context, id model.NodeID) (model.Node, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	return n, ok
}

func (f *fakeNodeView) Start(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	next, err := n.WithStatus(model.StatusRegistering)
	if err != nil {
		return err
	}
	f.nodes[id.String()] = next
	return nil
}

// Unregister mirrors the real service: only a node that is online (or at
// least registered) may leave, anything else is an illegal transition.
func (f *fakeNodeView) Unregister(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	if n.Status() != model.StatusOnline && n.Status() != model.StatusRegistered {
		return fmt.Errorf("%w: %s -> %s", model.ErrIllegalTransition, n.Status(), model.StatusOffline)
	}
	next, err := n.WithStatus(model.StatusOffline)
	if err != nil {
		return err
	}
	f.nodes[id.String()] = next
	return nil
}

func (f *fakeNodeView) Stop(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	next, err := n.WithStatus(model.StatusOffline)
	if err != nil {
		return err
	}
	f.nodes[id.String()] = next
	return nil
}

func newNodesServer(t *testing.T, view httpapi.NodeView) *httptest.Server {
	t.Helper()
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"}, view)
	return httptest.NewServer(s.Echo())
}

func getJSON(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

func postJSON(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestNodes_ListEmpty asserts an empty inventory renders as a JSON array,
// not null (task 8.1).
func TestNodes_ListEmpty(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()

	code, body := getJSON(t, ts.URL+"/v1/nodes")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/nodes = %d, want 200", code)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q is not a JSON array: %v", body, err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestNodes_ListFields asserts each entry carries id/kind/status/addr
// (task 8.1).
func TestNodes_ListFields(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusIdle)
	view.seed(t, "34020000012000000001", "127.0.0.1:5061", model.StatusIdle)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := getJSON(t, ts.URL+"/v1/nodes")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/nodes = %d, want 200", code)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%s)", len(got), body)
	}
	for _, entry := range got {
		for _, field := range []string{"id", "kind", "status", "addr"} {
			if _, ok := entry[field]; !ok {
				t.Errorf("entry %v missing field %q", entry, field)
			}
		}
	}
}

// TestNodes_DetailFoundAndUnknown asserts 200 for a known id and 404 with a
// JSON error body for an unknown one (task 8.2).
func TestNodes_DetailFoundAndUnknown(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusIdle)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := getJSON(t, ts.URL+"/v1/nodes/34020000011310000001")
	if code != http.StatusOK {
		t.Fatalf("GET known node = %d, want 200 (%s)", code, body)
	}
	if !json.Valid([]byte(body)) {
		t.Errorf("body %q is not valid JSON", body)
	}

	code, body = getJSON(t, ts.URL+"/v1/nodes/34020000011310000009")
	if code != http.StatusNotFound {
		t.Fatalf("GET unknown node = %d, want 404 (%s)", code, body)
	}
	var errBody map[string]string
	if err := json.Unmarshal([]byte(body), &errBody); err != nil {
		t.Fatalf("404 body %q is not a JSON object: %v", body, err)
	}
	if errBody["error"] == "" {
		t.Errorf("404 body %q has no error field", body)
	}
}

// TestNodes_StartStop asserts the control endpoints return 200 and move the
// node through registering then offline (task 8.3).
func TestNodes_StartStop(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOffline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := postJSON(t, ts.URL+"/v1/nodes/34020000011310000001/start")
	if code != http.StatusOK {
		t.Fatalf("POST start = %d, want 200 (%s)", code, body)
	}
	var started map[string]string
	if err := json.Unmarshal([]byte(body), &started); err != nil {
		t.Fatalf("start body: %v", err)
	}
	if started["status"] != "registering" {
		t.Errorf("status after start = %q, want registering", started["status"])
	}

	code, body = postJSON(t, ts.URL+"/v1/nodes/34020000011310000001/stop")
	if code != http.StatusOK {
		t.Fatalf("POST stop = %d, want 200 (%s)", code, body)
	}
	var stopped map[string]string
	if err := json.Unmarshal([]byte(body), &stopped); err != nil {
		t.Fatalf("stop body: %v", err)
	}
	if stopped["status"] != "offline" {
		t.Errorf("status after stop = %q, want offline", stopped["status"])
	}
}

// TestNodes_ConflictOnIllegalTransition asserts an illegal transition is 409
// and the body reports the status the node is actually in (design D8).
func TestNodes_ConflictOnIllegalTransition(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := postJSON(t, ts.URL+"/v1/nodes/34020000011310000001/start")
	if code != http.StatusConflict {
		t.Fatalf("POST start on an online node = %d, want 409 (%s)", code, body)
	}
	var conflict struct {
		Error  string `json:"error"`
		Status string `json:"status"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(body), &conflict); err != nil {
		t.Fatalf("409 body: %v", err)
	}
	if conflict.Status != "online" {
		t.Errorf("conflict status = %q, want online", conflict.Status)
	}
	if conflict.Action != "start" {
		t.Errorf("conflict action = %q, want start", conflict.Action)
	}
	if conflict.Error == "" {
		t.Error("conflict body has no error field")
	}

	// The node must be untouched by the refused request.
	if n, _ := view.Get(context.Background(), mustNodeID(t, "34020000011310000001")); n.Status() != model.StatusOnline {
		t.Errorf("node status = %v, want online (unchanged)", n.Status())
	}
}

// TestNodes_MalformedID asserts a syntactically invalid id is rejected
// before any lookup.
func TestNodes_MalformedID(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()
	if code, _ := getJSON(t, ts.URL+"/v1/nodes/not-an-id"); code != http.StatusBadRequest {
		t.Errorf("GET malformed id = %d, want 400", code)
	}
}

// TestNodes_HealthEndpointsUnaffected asserts the pre-existing endpoints
// still work after the node routes were added (task 8.4).
func TestNodes_HealthEndpointsUnaffected(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()
	for _, path := range []string{"/v1/health", "/healthz"} {
		code, body := getJSON(t, ts.URL+path)
		if code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (%s)", path, code, body)
		}
	}
}

func mustNodeID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

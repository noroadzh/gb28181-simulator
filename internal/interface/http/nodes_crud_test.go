// HTTP tests for the node-configuration CRUD surface (Change
// runtime-node-configuration, tasks 3.1–3.4).
//
// These run against the real stack — app.NodeService over a bootstrapped
// SQLite database — rather than the fakeNodeView the older endpoint tests
// use. The behaviour under test is precisely the handoff the fake would
// paper over: a request that must reach the store, and a store refusal that
// must become the right status code.
package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodesql"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformclock "github.com/your-org/gb28181-simulator/internal/platform/clock"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/events"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/storage"
)

const (
	nodeCRUDDeviceID = "34020000001310000001"
	nodeCRUDLargeID  = "34020000002000000001"
	nodeCRUDSmallID  = "34020000002160000001"
)

// nodeCRUDHarness is one HTTP server in front of a real service and store.
type nodeCRUDHarness struct {
	url      string
	service  *app.NodeService
	registry *nodereg.Registry
	store    *nodesql.Store
}

func newNodeCRUDHarness(t *testing.T) *nodeCRUDHarness {
	t.Helper()
	db, err := storage.Bootstrap(filepath.Join(t.TempDir(), "nodes.db"))
	if err != nil {
		t.Fatalf("storage.Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	registry := nodereg.New()
	factory := func(string, model.NodeID) (port.SIPTransport, error) {
		return nil, errors.New("transports are not used in these tests")
	}
	service, err := app.NewNodeService(
		registry,
		nodereg.NewLifecycle(registry, factory),
		registry,
		factory,
		platformclock.Real(),
	)
	if err != nil {
		t.Fatalf("app.NewNodeService: %v", err)
	}
	store := nodesql.New(db)
	if _, err := service.WithNodeStore(store); err != nil {
		t.Fatalf("WithNodeStore: %v", err)
	}
	if _, err := service.WithEvents(events.NewHub(32)); err != nil {
		t.Fatalf("WithEvents: %v", err)
	}

	srv := httpapi.NewServer(
		platformconfig.Config{},
		logging.NewHub(4),
		httpapi.Version{Version: "test"},
		service, nil, nil, nil, nil,
	)
	ts := httptest.NewServer(srv.Echo())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Shutdown(context.Background())
	})

	return &nodeCRUDHarness{url: ts.URL, service: service, registry: registry, store: store}
}

// request issues one JSON request and returns the status and raw body.
func (h *nodeCRUDHarness) request(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.url+path, rdr)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", method, path, err)
	}
	return resp.StatusCode, string(raw)
}

// create posts a minimal node definition and fails the test unless the API
// answers 201.
func (h *nodeCRUDHarness) create(t *testing.T, id, kind, addr string) {
	t.Helper()
	status, body := h.request(t, http.MethodPost, "/v1/nodes",
		`{"id":"`+id+`","kind":"`+kind+`","addr":"`+addr+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("create %s = %d, want 201 (body %s)", id, status, body)
	}
}

// advance walks a node to status through the legal transition table.
func (h *nodeCRUDHarness) advance(t *testing.T, id string, to model.Status) {
	t.Helper()
	nodeID, err := model.ParseNodeID(id)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", id, err)
	}
	ctx := context.Background()
	for _, step := range []model.Status{model.StatusRegistering, model.StatusRegistered, model.StatusOnline} {
		if _, err := h.registry.Advance(ctx, nodeID, step); err != nil {
			t.Fatalf("advance %s: %v", id, err)
		}
		if step == to {
			return
		}
	}
}

func decodeObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

// --- 3.4 end-to-end CRUD -------------------------------------------

func TestNodesCRUD_CreateReadUpdateDelete(t *testing.T) {
	h := newNodeCRUDHarness(t)
	ctx := context.Background()

	// create
	status, body := h.request(t, http.MethodPost, "/v1/nodes",
		`{"id":"`+nodeCRUDDeviceID+`","kind":"device","domain":"3402000000","addr":"127.0.0.1:15061","vendor":"acme"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST = %d, want 201 (body %s)", status, body)
	}
	created := decodeObject(t, body)
	if created["id"] != nodeCRUDDeviceID {
		t.Errorf("created id = %v, want %s", created["id"], nodeCRUDDeviceID)
	}
	if created["status"] != "idle" {
		t.Errorf("created status = %v, want idle", created["status"])
	}

	// read
	status, body = h.request(t, http.MethodGet, "/v1/nodes/"+nodeCRUDDeviceID, "")
	if status != http.StatusOK {
		t.Fatalf("GET = %d, want 200 (body %s)", status, body)
	}
	if got := decodeObject(t, body)["addr"]; got != "127.0.0.1:15061" {
		t.Errorf("GET addr = %v, want 127.0.0.1:15061", got)
	}

	// update
	status, body = h.request(t, http.MethodPut, "/v1/nodes/"+nodeCRUDDeviceID,
		`{"addr":"127.0.0.1:15062","vendor":"other"}`)
	if status != http.StatusOK {
		t.Fatalf("PUT = %d, want 200 (body %s)", status, body)
	}
	updated := decodeObject(t, body)
	if updated["addr"] != "127.0.0.1:15062" || updated["vendor"] != "other" {
		t.Errorf("PUT body = %v, want addr 15062 and vendor other", updated)
	}
	nodeID, _ := model.ParseNodeID(nodeCRUDDeviceID)
	stored, err := h.store.GetNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("store.GetNode: %v", err)
	}
	if stored.Addr != "127.0.0.1:15062" || stored.Vendor != "other" {
		t.Errorf("stored row = (%q, %q), want (127.0.0.1:15062, other)", stored.Addr, stored.Vendor)
	}

	// delete
	status, body = h.request(t, http.MethodDelete, "/v1/nodes/"+nodeCRUDDeviceID, "")
	if status != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200 (body %s)", status, body)
	}
	if got := decodeObject(t, body)["status"]; got != "deleted" {
		t.Errorf("DELETE body status = %v, want deleted", got)
	}
	if status, _ = h.request(t, http.MethodGet, "/v1/nodes/"+nodeCRUDDeviceID, ""); status != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", status)
	}
	if _, err := h.store.GetNode(ctx, nodeID); !errors.Is(err, port.ErrNodeNotFound) {
		t.Errorf("store.GetNode after delete = %v, want ErrNodeNotFound", err)
	}
}

// --- 3.1 status codes ----------------------------------------------

func TestNodesCRUD_StatusCodes(t *testing.T) {
	h := newNodeCRUDHarness(t)
	h.create(t, nodeCRUDDeviceID, "device", "127.0.0.1:15061")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"duplicate id", http.MethodPost, "/v1/nodes",
			`{"id":"` + nodeCRUDDeviceID + `","kind":"device","addr":"127.0.0.1:15063"}`, http.StatusConflict},
		{"short id", http.MethodPost, "/v1/nodes",
			`{"id":"340200000013100000","kind":"device","addr":"127.0.0.1:15063"}`, http.StatusBadRequest},
		{"missing addr", http.MethodPost, "/v1/nodes",
			`{"id":"34020000001310000002","kind":"device"}`, http.StatusBadRequest},
		{"unknown node", http.MethodGet, "/v1/nodes/34020000001310000009", "", http.StatusNotFound},
		{"update unknown", http.MethodPut, "/v1/nodes/34020000001310000009",
			`{"addr":"127.0.0.1:15064"}`, http.StatusNotFound},
		{"delete unknown", http.MethodDelete, "/v1/nodes/34020000001310000009", "", http.StatusNotFound},
		{"immutable id", http.MethodPut, "/v1/nodes/" + nodeCRUDDeviceID,
			`{"id":"` + nodeCRUDSmallID + `"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := h.request(t, tc.method, tc.path, tc.body)
			if status != tc.want {
				t.Errorf("%s %s = %d, want %d (body %s)", tc.method, tc.path, status, tc.want, body)
			}
		})
	}

	// A live node may not be deleted: the store row must survive the refusal.
	t.Run("delete live node", func(t *testing.T) {
		h.advance(t, nodeCRUDDeviceID, model.StatusOnline)
		status, body := h.request(t, http.MethodDelete, "/v1/nodes/"+nodeCRUDDeviceID, "")
		if status != http.StatusConflict {
			t.Fatalf("DELETE live node = %d, want 409 (body %s)", status, body)
		}
		nodeID, _ := model.ParseNodeID(nodeCRUDDeviceID)
		if _, err := h.store.GetNode(context.Background(), nodeID); err != nil {
			t.Errorf("row disappeared despite the 409: %v", err)
		}
	})
}

// --- 3.2 cascade endpoint ------------------------------------------

func TestNodesCascade_UpdateAndErrors(t *testing.T) {
	h := newNodeCRUDHarness(t)
	h.create(t, nodeCRUDLargeID, "platform-large", "127.0.0.1:15071")
	h.create(t, nodeCRUDSmallID, "platform-small", "127.0.0.1:15072")
	h.create(t, nodeCRUDDeviceID, "device", "127.0.0.1:15073")

	// normal update, echoed back as stored
	status, body := h.request(t, http.MethodPut, "/v1/nodes/"+nodeCRUDSmallID+"/cascade",
		`{"parent":"`+nodeCRUDLargeID+`","children":["`+nodeCRUDDeviceID+`"]}`)
	if status != http.StatusOK {
		t.Fatalf("PUT cascade = %d, want 200 (body %s)", status, body)
	}
	edit := decodeObject(t, body)
	if edit["cascade_parent"] != nodeCRUDLargeID {
		t.Errorf("cascade_parent = %v, want %s", edit["cascade_parent"], nodeCRUDLargeID)
	}
	children, _ := edit["cascade_children"].([]any)
	if len(children) != 1 || children[0] != nodeCRUDDeviceID {
		t.Errorf("cascade_children = %v, want [%s]", edit["cascade_children"], nodeCRUDDeviceID)
	}

	cases := []struct {
		name       string
		body       string
		want       int
		wantErrSub string
	}{
		{
			"self parent",
			`{"parent":"` + nodeCRUDLargeID + `"}`,
			http.StatusBadRequest,
			"node cannot be its own parent",
		},
		{
			// small was made large's child above, so giving large small as
			// its parent closes the loop.
			"ancestor cycle",
			`{"parent":"` + nodeCRUDSmallID + `"}`,
			http.StatusConflict,
			"cycle",
		},
		{
			"unknown parent",
			`{"parent":"34020000002000000009"}`,
			http.StatusNotFound,
			"parent node not found",
		},
		{
			"unknown child",
			`{"children":["34020000001320000009"]}`,
			http.StatusNotFound,
			"child node not found",
		},
		{
			"malformed parent",
			`{"parent":"not-an-id"}`,
			http.StatusBadRequest,
			"valid node id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Always edit the top node so the error cases do not collide
			// with the edges the happy-path call just wrote.
			status, body := h.request(t, http.MethodPut, "/v1/nodes/"+nodeCRUDLargeID+"/cascade", tc.body)
			if status != tc.want {
				t.Fatalf("PUT cascade = %d, want %d (body %s)", status, tc.want, body)
			}
			if !strings.Contains(body, tc.wantErrSub) {
				t.Errorf("body %s does not contain %q", body, tc.wantErrSub)
			}
		})
	}
}

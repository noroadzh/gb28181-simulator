package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/cascade"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodesql"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformclock "github.com/your-org/gb28181-simulator/internal/platform/clock"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/storage"
)

// These tests walk the whole path a node definition takes on its way to
// being served over HTTP: YAML (or an API call) in, storage out, GET
// /v1/nodes back. Everything in between is the production wiring borrowed
// verbatim from run() — the same storage.Bootstrap, the same nodesql.Store,
// the same nodereg + cascade + NodeService trio — so a break in the handoff
// shows up here rather than in a dashboard that silently lists nothing.

// bootFromStore assembles the slice of run() that matters to node
// definitions: a bootstrapped database, a store over it, and a NodeService
// whose nodes have already been loaded from that store. The transport
// factory is never invoked (nothing here starts a node), so a loopback
// address that is free at test time is enough to prove the wiring compiles
// and connects.
func bootFromStore(t *testing.T, nodes []platformconfig.NodeConfig) (*nodesql.Store, *httpapi.Server) {
	t.Helper()
	return bootFromStoreAt(t, filepath.Join(t.TempDir(), "test.db"), nodes)
}

// bootFromStoreAt is bootFromStore over an explicit database path. The
// restart tests pass the same path to consecutive boots to simulate a
// process restarting over one persistent database file.
func bootFromStoreAt(t *testing.T, dbPath string, nodes []platformconfig.NodeConfig) (*nodesql.Store, *httpapi.Server) {
	t.Helper()
	db, err := storage.Bootstrap(dbPath)
	if err != nil {
		t.Fatalf("storage.Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := nodesql.New(db)
	ctx := context.Background()

	// The cold-start handoff, exactly as run() performs it: YAML in, store
	// out, and on a store that already holds rows the YAML loses.
	if err := syncNodeStoreFromYAML(ctx, store, nodes); err != nil {
		t.Fatalf("syncNodeStoreFromYAML: %v", err)
	}

	registry := nodereg.New()
	cascadeHandler := cascade.New(nil, logging.L())
	registry.WithCascadeHandler(cascadeHandler)
	svc, err := app.NewNodeService(registry, nodereg.NewLifecycle(registry, bindTransport), registry, bindTransport, platformclock.Real())
	if err != nil {
		t.Fatalf("app.NewNodeService: %v", err)
	}
	// run() attaches the store to the service before loading (main.go's
	// "runtime node storage enabled" branch); without it LoadFromStore
	// sees no store and loads nothing, and the CRUD API serves an empty
	// fleet no matter what the store holds.
	if _, err := svc.WithNodeStore(store); err != nil {
		t.Fatalf("WithNodeStore: %v", err)
	}
	if err := svc.LoadFromStore(ctx); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	t.Cleanup(func() {
		for _, n := range registry.List(ctx) {
			_ = svc.Unregister(ctx, n.ID())
		}
	})

	srv := httpapi.NewServer(
		platformconfig.Config{},
		logging.NewHub(16),
		httpapi.Version{Version: "test", Commit: "test", BuiltAt: "now"},
		svc, nil, nil, nil, nil,
	)
	return store, srv
}

// listNodesOverHTTP issues the real request a dashboard makes on load.
func listNodesOverHTTP(t *testing.T, srv *httpapi.Server) []map[string]any {
	t.Helper()
	ts := httptest.NewServer(srv.Echo())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Shutdown(context.Background())
	})
	resp, err := http.Get(ts.URL + "/v1/nodes")
	if err != nil {
		t.Fatalf("GET /v1/nodes: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/nodes = %d, want 200", resp.StatusCode)
	}
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /v1/nodes: %v", err)
	}
	return body
}

// The headline promise: a node written in YAML shows up over HTTP on a cold
// database.
func TestYAMLImport_ServesOverHTTP(t *testing.T) {
	_, srv := bootFromStore(t, []platformconfig.NodeConfig{{
		ID: "34020000002000000001", Kind: "platform-large",
		Domain: "3402000000", Addr: "127.0.0.1:15061",
		Vendor: "acme",
	}})

	items := listNodesOverHTTP(t, srv)
	if len(items) != 1 {
		t.Fatalf("GET /v1/nodes returned %d nodes, want 1", len(items))
	}
	if got := items[0]["id"]; got != "34020000002000000001" {
		t.Errorf("id = %v, want 34020000002000000001", got)
	}
}

// A restart must bring back exactly what the store holds — including nodes
// that were never in YAML, because they were created over the API at
// runtime. This is the test that would catch a boot path quietly falling
// back to YAML.
func TestRestart_HonoursRuntimeNodesTheYAMLNeverMentioned(t *testing.T) {
	ctx := context.Background()
	nodes := []platformconfig.NodeConfig{{
		ID: "34020000002000000001", Kind: "platform-large",
		Domain: "3402000000", Addr: "127.0.0.1:15061",
	}}

	// First boot: YAML is imported, and an operator creates a second node
	// through the API path while the process is running.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, _ := bootFromStoreAt(t, dbPath, nodes)
	id, err := model.ParseNodeID("34020000011310000009")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	if err := store.CreateNode(ctx, model.StoredNode{
		ID:     id,
		Kind:   model.NodeKindDevice,
		Domain: "3402000000",
		Addr:   "127.0.0.1:15099",
	}); err != nil {
		t.Fatalf("store.CreateNode: %v", err)
	}

	// Second boot: a fresh service over the same store. The YAML does not
	// mention the API-created node, so it can only appear if the store is
	// really the source of truth.
	_, srv := bootFromStoreAt(t, dbPath, nodes)
	ids := map[string]bool{}
	for _, n := range listNodesOverHTTP(t, srv) {
		if s, ok := n["id"].(string); ok {
			ids[s] = true
		}
	}
	if !ids["34020000002000000001"] {
		t.Error("the YAML node did not survive the restart")
	}
	if !ids["34020000011310000009"] {
		t.Error("the API-created node did not survive the restart; the boot read YAML instead of the store")
	}
}

// Deleting a node at runtime must stick: the cold-start import only fires
// on an empty store, so a deleted node cannot reappear from YAML on the
// next boot.
func TestDeleteNode_DoesNotResurrectFromYAML(t *testing.T) {
	ctx := context.Background()
	nodes := []platformconfig.NodeConfig{
		{ID: "34020000002000000001", Kind: "platform-large", Domain: "3402000000", Addr: "127.0.0.1:15061"},
		{ID: "34020000011310000009", Kind: "device", Domain: "3402000000", Addr: "127.0.0.1:15099"},
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, _ := bootFromStoreAt(t, dbPath, nodes)
	id, err := model.ParseNodeID("34020000011310000009")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	if err := store.DeleteNode(ctx, id); err != nil {
		t.Fatalf("store.DeleteNode: %v", err)
	}

	_, srv := bootFromStoreAt(t, dbPath, nodes)
	for _, n := range listNodesOverHTTP(t, srv) {
		if n["id"] == "34020000011310000009" {
			t.Fatal("the deleted node came back on the next boot; the cold-start import is not gated on an empty store")
		}
	}
}

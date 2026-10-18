// Package nodesql — tests for the SQLite-backed runtime node
// configuration store. The test file lives in package nodesql (not
// nodesql_test) so it can reach the unexported helpers when the
// contract they enforce is itself the thing under test.
package nodesql

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/storage"
)

// newTestDB opens a throwaway SQLite database through storage.Bootstrap so
// the schema, the migration log and the foreign_keys pragma are wired the
// same way they are in production. Returning *sql.DB (not *Store) lets the
// test file build whatever Store shape it needs around it.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Bootstrap(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestStore wraps newTestDB with the same field-of-view the production
// code uses: a Store around an *sql.DB that already has FK and migrations
// applied. The Store struct has db unexported, so the test lives in the
// same package to construct it directly.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{db: newTestDB(t)}
}

// mustNodeID parses a 20-digit id the way ParseNodeID expects, with a
// failure path that is loud enough to debug without staring at a stack.
func mustNodeID(t *testing.T, id string) model.NodeID {
	t.Helper()
	nid, err := model.ParseNodeID(id)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", id, err)
	}
	return nid
}

// mustPolicy builds a non-zero ExpiresPolicy so the platform row carries
// values distinct from the schema defaults — otherwise the round-trip
// assertion cannot tell persistence from "I just read the column default".
func mustPolicy(t *testing.T) model.ExpiresPolicy {
	t.Helper()
	p, err := model.NewExpiresPolicy(120, 1800, 7200)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	return p
}

// mustRegistration builds a Registration with the given allow_no_auth bit
// and a matching password (empty when allowNoAuth is true, non-empty
// otherwise — NewRegistration refuses the empty-password / no-allow pair).
func mustRegistration(t *testing.T, allowNoAuth bool) model.Registration {
	t.Helper()
	pw := "secret-pw"
	if allowNoAuth {
		pw = ""
	}
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:               "10.0.0.1:5060",
		ServerID:             "34020000002000000001",
		Username:             "u1",
		Password:             pw,
		GBVersion:            "2016",
		Expires:              900,
		Timeout:              5 * time.Second,
		Transport:            "udp",
		AllowNoAuth:          allowNoAuth,
		HeartbeatInterval:    30 * time.Second,
		HeartbeatTimeout:     4 * time.Second,
		HeartbeatMaxFailures: 2,
	})
	if err != nil {
		t.Fatalf("NewRegistration(allowNoAuth=%v): %v", allowNoAuth, err)
	}
	return reg
}

// mkRegistrationRow builds a full StoredNode carrying a registration row
// and no platform row. Kind is platform-large because that is the kind
// the registration half of the spec is for.
func mkRegistrationNode(t *testing.T, id string, allowNoAuth bool) model.StoredNode {
	t.Helper()
	return model.StoredNode{
		ID:              mustNodeID(t, id),
		Kind:            model.NodeKindPlatformLarge,
		Domain:          "34020000",
		Addr:            "10.96.1.125:5060",
		Vendor:          "acme",
		CascadeParent:   "34020000002000000001",
		CascadeChildren: []string{"34020000001320000002", "34020000001320000003"},
		Registration:    mustRegistration(t, allowNoAuth),
		HasRegistration: true,
	}
}

// mkPlatformNode builds a full StoredNode carrying a platform-serving row
// and a registration row (platform-large may also register), with all
// platform policy fields distinct from defaults.
func mkPlatformNode(t *testing.T, id string, allowNoAuth bool) model.StoredNode {
	t.Helper()
	policy := mustPolicy(t)
	plat, err := model.NewPlatformServingWithNoAuth("34020000", policy, allowNoAuth)
	if err != nil {
		t.Fatalf("NewPlatformServingWithNoAuth: %v", err)
	}
	return model.StoredNode{
		ID:                   mustNodeID(t, id),
		Kind:                 model.NodeKindPlatformSmall,
		Domain:               "34020000",
		Addr:                 "10.96.1.130:5060",
		Vendor:               "acme",
		CascadeParent:        "34020000001320000001",
		CascadeChildren:      []string{"34020000001320000004"},
		Registration:         mustRegistration(t, allowNoAuth),
		HasRegistration:      true,
		Platform:             plat,
		HasPlatform:          true,
		CatalogPollIntervalMs: 60000,
	}
}

// assertEqualStoredNode compares two StoredNode values field by field
// except the Has* booleans (which the store sets from the presence of
// rows, not from the input) and CatalogPollIntervalMs (already covered
// by Platform pair check on the platform path). The diff output is the
// same go-cmp-style field path Go test prints, so a failure points at
// the exact place the round trip diverged.
func assertEqualStoredNode(t *testing.T, want, got model.StoredNode) {
	t.Helper()
	if want.ID != got.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if want.Kind != got.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, want.Kind)
	}
	if want.Domain != got.Domain {
		t.Errorf("Domain: got %q, want %q", got.Domain, want.Domain)
	}
	if want.Addr != got.Addr {
		t.Errorf("Addr: got %q, want %q", got.Addr, want.Addr)
	}
	if want.Vendor != got.Vendor {
		t.Errorf("Vendor: got %q, want %q", got.Vendor, want.Vendor)
	}
	if want.CascadeParent != got.CascadeParent {
		t.Errorf("CascadeParent: got %q, want %q", got.CascadeParent, want.CascadeParent)
	}
	if !reflect.DeepEqual(want.CascadeChildren, got.CascadeChildren) {
		t.Errorf("CascadeChildren: got %v, want %v", got.CascadeChildren, want.CascadeChildren)
	}
	if want.HasRegistration != got.HasRegistration {
		t.Errorf("HasRegistration: got %v, want %v", got.HasRegistration, want.HasRegistration)
	}
	if want.HasPlatform != got.HasPlatform {
		t.Errorf("HasPlatform: got %v, want %v", got.HasPlatform, want.HasPlatform)
	}
	if want.HasRegistration {
		assertEqualRegistration(t, want.Registration, got.Registration)
	}
	if want.HasPlatform {
		assertEqualPlatform(t, want.Platform, got.Platform)
		if want.CatalogPollIntervalMs != got.CatalogPollIntervalMs {
			t.Errorf("CatalogPollIntervalMs: got %d, want %d",
				got.CatalogPollIntervalMs, want.CatalogPollIntervalMs)
		}
	}
}

func assertEqualRegistration(t *testing.T, want, got model.Registration) {
	t.Helper()
	if want.Server() != got.Server() {
		t.Errorf("Registration.Server: got %q, want %q", got.Server(), want.Server())
	}
	if want.ServerID() != got.ServerID() {
		t.Errorf("Registration.ServerID: got %q, want %q", got.ServerID(), want.ServerID())
	}
	if want.Username() != got.Username() {
		t.Errorf("Registration.Username: got %q, want %q", got.Username(), want.Username())
	}
	if want.Password() != got.Password() {
		t.Errorf("Registration.Password: got %q, want %q", got.Password(), want.Password())
	}
	if want.GBVersion() != got.GBVersion() {
		t.Errorf("Registration.GBVersion: got %q, want %q", got.GBVersion(), want.GBVersion())
	}
	if want.Expires() != got.Expires() {
		t.Errorf("Registration.Expires: got %d, want %d", got.Expires(), want.Expires())
	}
	if want.Timeout() != got.Timeout() {
		t.Errorf("Registration.Timeout: got %v, want %v", got.Timeout(), want.Timeout())
	}
	if want.Transport() != got.Transport() {
		t.Errorf("Registration.Transport: got %q, want %q", got.Transport(), want.Transport())
	}
	if want.AllowNoAuth() != got.AllowNoAuth() {
		t.Errorf("Registration.AllowNoAuth: got %v, want %v", got.AllowNoAuth(), want.AllowNoAuth())
	}
	if want.HeartbeatInterval() != got.HeartbeatInterval() {
		t.Errorf("Registration.HeartbeatInterval: got %v, want %v",
			got.HeartbeatInterval(), want.HeartbeatInterval())
	}
	if want.HeartbeatTimeout() != got.HeartbeatTimeout() {
		t.Errorf("Registration.HeartbeatTimeout: got %v, want %v",
			got.HeartbeatTimeout(), want.HeartbeatTimeout())
	}
	if want.MaxHeartbeatFailures() != got.MaxHeartbeatFailures() {
		t.Errorf("Registration.MaxHeartbeatFailures: got %d, want %d",
			got.MaxHeartbeatFailures(), want.MaxHeartbeatFailures())
	}
}

func assertEqualPlatform(t *testing.T, want, got model.PlatformServing) {
	t.Helper()
	if want.Realm() != got.Realm() {
		t.Errorf("Platform.Realm: got %q, want %q", got.Realm(), want.Realm())
	}
	if want.AllowNoAuth() != got.AllowNoAuth() {
		t.Errorf("Platform.AllowNoAuth: got %v, want %v", got.AllowNoAuth(), want.AllowNoAuth())
	}
	if want.Policy().Min() != got.Policy().Min() {
		t.Errorf("Platform.Policy.Min: got %d, want %d",
			got.Policy().Min(), want.Policy().Min())
	}
	if want.Policy().Default() != got.Policy().Default() {
		t.Errorf("Platform.Policy.Default: got %d, want %d",
			got.Policy().Default(), want.Policy().Default())
	}
	if want.Policy().Max() != got.Policy().Max() {
		t.Errorf("Platform.Policy.Max: got %d, want %d",
			got.Policy().Max(), want.Policy().Max())
	}
}

// countChildRows returns the number of rows in node_registrations (when
// tableReg) or node_platforms (otherwise) for the given node id. It is
// the direct way to prove the FK cascade actually fired and the upsert
// replace actually replaced.
func countChildRows(t *testing.T, db *sql.DB, nodeID string, tableReg bool) int {
	t.Helper()
	tbl := "node_platforms"
	if tableReg {
		tbl = "node_registrations"
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+tbl+" WHERE node_id = ?", nodeID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", tbl, err)
	}
	return n
}

func TestCreateNode_AllFields_Persists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := mkPlatformNode(t, "34020000001320000010", true)

	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	got, err := s.GetNode(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	assertEqualStoredNode(t, in, got)
}

func TestCreateNode_AllowNoAuthFalse_RoundTripsPassword(t *testing.T) {
	// The allow_no_auth=0 path is the one that actually has a
	// password to lose; without an explicit check that path was
	// passing for the wrong reason (empty default in the schema).
	s := newTestStore(t)
	ctx := context.Background()
	in := mkRegistrationNode(t, "34020000001320000011", false)

	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	got, err := s.GetNode(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Registration.Password() != "secret-pw" {
		t.Errorf("Password: got %q, want %q", got.Registration.Password(), "secret-pw")
	}
	if got.Registration.AllowNoAuth() {
		t.Errorf("AllowNoAuth: got true, want false")
	}
}

func TestCreateNode_DuplicateID_ErrAlreadyExists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := mkRegistrationNode(t, "34020000001320000012", true)
	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode 1: %v", err)
	}
	err := s.CreateNode(ctx, in)
	if err == nil {
		t.Fatal("CreateNode 2: want ErrNodeAlreadyExists, got nil")
	}
	if !errors.Is(err, port.ErrNodeAlreadyExists) {
		t.Errorf("CreateNode 2: want errors.Is(ErrNodeAlreadyExists), got %v", err)
	}
}

func TestCreateNode_NilChildren_StoredAsEmptyJSON(t *testing.T) {
	// marshalChildren turns nil into "[]" so the column is well-formed
	// JSON; unmarshalChildren turns an empty string back into a nil
	// slice. The two together mean: write a node with no children,
	// read it back, get a nil slice (not an empty slice).
	s := newTestStore(t)
	ctx := context.Background()
	in := model.StoredNode{
		ID:     mustNodeID(t, "34020000001320000013"),
		Kind:   model.NodeKindDevice,
		Domain: "34020000",
		Addr:   "10.96.1.140:5060",
	}
	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	got, err := s.GetNode(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if len(got.CascadeChildren) != 0 {
		t.Errorf("CascadeChildren: got %v (len %d), want empty", got.CascadeChildren, len(got.CascadeChildren))
	}
}

func TestGetNode_NotFound_ErrNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetNode(context.Background(), mustNodeID(t, "34020000001320000099"))
	if err == nil {
		t.Fatal("GetNode: want ErrNodeNotFound, got nil")
	}
	if !errors.Is(err, port.ErrNodeNotFound) {
		t.Errorf("GetNode: want errors.Is(ErrNodeNotFound), got %v", err)
	}
}

func TestListNodes_OrderedByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Insert in non-sorted order to prove the ORDER BY id clause is
	// what sorts the output, not the insertion order.
	ids := []string{
		"34020000001320000020",
		"34020000001320000018",
		"34020000001320000019",
	}
	for _, id := range ids {
		n := model.StoredNode{
			ID:     mustNodeID(t, id),
			Kind:   model.NodeKindDevice,
			Domain: "34020000",
			Addr:   id + "@10.96.1.150:5060",
		}
		if err := s.CreateNode(ctx, n); err != nil {
			t.Fatalf("CreateNode %s: %v", id, err)
		}
	}
	got, err := s.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListNodes: got %d, want 3", len(got))
	}
	wantOrder := []string{
		"34020000001320000018",
		"34020000001320000019",
		"34020000001320000020",
	}
	for i, w := range wantOrder {
		if got[i].ID.String() != w {
			t.Errorf("ListNodes[%d].ID: got %q, want %q", i, got[i].ID, w)
		}
	}
}

func TestUpdateNode_ReplacesChildren(t *testing.T) {
	// The update path deletes the existing registration and platform
	// rows before re-inserting, so flipping HasRegistration from
	// true to false must leave no orphan registration behind.
	s := newTestStore(t)
	ctx := context.Background()
	in := mkPlatformNode(t, "34020000001320000030", true)
	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	updated := in
	updated.Addr = "10.96.1.131:5060"
	updated.Registration = mustRegistration(t, false) // password now "secret-pw"
	updated.HasRegistration = true // keep registration, just rewrite it
	updated.CatalogPollIntervalMs = 120000

	if err := s.UpdateNode(ctx, in.ID, updated); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	got, err := s.GetNode(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Addr != "10.96.1.131:5060" {
		t.Errorf("Addr: got %q, want %q", got.Addr, "10.96.1.131:5060")
	}
	if got.Registration.Password() != "secret-pw" {
		t.Errorf("Registration.Password: got %q, want %q", got.Registration.Password(), "secret-pw")
	}
	if got.Registration.AllowNoAuth() {
		t.Errorf("Registration.AllowNoAuth: got true, want false")
	}
	if got.CatalogPollIntervalMs != 120000 {
		t.Errorf("CatalogPollIntervalMs: got %d, want 120000", got.CatalogPollIntervalMs)
	}

	// Now drop the registration and confirm the row is gone, not stale.
	updated2 := updated
	updated2.HasRegistration = false
	if err := s.UpdateNode(ctx, in.ID, updated2); err != nil {
		t.Fatalf("UpdateNode (drop registration): %v", err)
	}
	if n := countChildRows(t, s.db, in.ID.String(), true); n != 0 {
		t.Errorf("node_registrations rows after drop: got %d, want 0", n)
	}
}

func TestUpdateNode_NotFound_ErrNotFound(t *testing.T) {
	s := newTestStore(t)
	in := mkRegistrationNode(t, "34020000001320000040", true)
	err := s.UpdateNode(context.Background(), mustNodeID(t, "34020000001320000099"), in)
	if err == nil {
		t.Fatal("UpdateNode: want ErrNodeNotFound, got nil")
	}
	if !errors.Is(err, port.ErrNodeNotFound) {
		t.Errorf("UpdateNode: want errors.Is(ErrNodeNotFound), got %v", err)
	}
}

func TestUpdateCascade_OnlyUpdatesCascadeFields(t *testing.T) {
	// The cascade-only path must not touch the rest of the node row;
	// a regression that rewrote kind/domain/addr/vendor from a
	// default would silently corrupt the registration target the
	// node dials out to.
	s := newTestStore(t)
	ctx := context.Background()
	in := mkRegistrationNode(t, "34020000001320000050", true)
	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	newParent := "34020000002000000099"
	newChildren := []string{"34020000001320000051", "34020000001320000052"}
	if err := s.UpdateCascade(ctx, in.ID, newParent, newChildren); err != nil {
		t.Fatalf("UpdateCascade: %v", err)
	}
	got, err := s.GetNode(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.CascadeParent != newParent {
		t.Errorf("CascadeParent: got %q, want %q", got.CascadeParent, newParent)
	}
	if !reflect.DeepEqual(got.CascadeChildren, newChildren) {
		t.Errorf("CascadeChildren: got %v, want %v", got.CascadeChildren, newChildren)
	}
	// The untouched fields must come back exactly as inserted.
	if got.Domain != in.Domain {
		t.Errorf("Domain: got %q, want %q (UpdateCascade must not rewrite)", got.Domain, in.Domain)
	}
	if got.Addr != in.Addr {
		t.Errorf("Addr: got %q, want %q (UpdateCascade must not rewrite)", got.Addr, in.Addr)
	}
	if got.Kind != in.Kind {
		t.Errorf("Kind: got %q, want %q (UpdateCascade must not rewrite)", got.Kind, in.Kind)
	}
	if !got.HasRegistration {
		t.Error("HasRegistration: got false, want true (UpdateCascade must not drop child rows)")
	}
}

func TestDeleteNode_CascadesChildRows(t *testing.T) {
	// The schema's ON DELETE CASCADE depends on PRAGMA foreign_keys=ON.
	// storage.Bootstrap turns that on, so this test is also a
	// regression test against the pragma being lost (e.g. by someone
	// opening the DB through a path that skips Bootstrap).
	s := newTestStore(t)
	ctx := context.Background()
	in := mkPlatformNode(t, "34020000001320000060", true)
	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	// Sanity: the child rows are present before the delete.
	if n := countChildRows(t, s.db, in.ID.String(), true); n != 1 {
		t.Fatalf("pre-delete registrations: got %d, want 1", n)
	}
	if n := countChildRows(t, s.db, in.ID.String(), false); n != 1 {
		t.Fatalf("pre-delete platforms: got %d, want 1", n)
	}
	if err := s.DeleteNode(ctx, in.ID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	if n := countChildRows(t, s.db, in.ID.String(), true); n != 0 {
		t.Errorf("post-delete registrations: got %d, want 0 (FK cascade missing?)", n)
	}
	if n := countChildRows(t, s.db, in.ID.String(), false); n != 0 {
		t.Errorf("post-delete platforms: got %d, want 0 (FK cascade missing?)", n)
	}
	// And the parent row itself must be gone, not just detached.
	_, err := s.GetNode(ctx, in.ID)
	if !errors.Is(err, port.ErrNodeNotFound) {
		t.Errorf("GetNode after delete: want ErrNodeNotFound, got %v", err)
	}
}

func TestDeleteNode_NotFound_ErrNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteNode(context.Background(), mustNodeID(t, "34020000001320000099"))
	if err == nil {
		t.Fatal("DeleteNode: want ErrNodeNotFound, got nil")
	}
	if !errors.Is(err, port.ErrNodeNotFound) {
		t.Errorf("DeleteNode: want errors.Is(ErrNodeNotFound), got %v", err)
	}
}

func TestUpsertPlatform_ReplacesExistingRow(t *testing.T) {
	// UpsertPlatform is a delete-then-insert: a second call with a
	// different poll interval must leave exactly one row, not two.
	// If the delete step were dropped, the second insert would fail
	// on the PK uniqueness constraint, so the success of this test
	// is itself the proof that the replace step ran.
	s := newTestStore(t)
	ctx := context.Background()
	in := mkPlatformNode(t, "34020000001320000070", true)
	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	policy2, err := model.NewExpiresPolicy(30, 600, 1800)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	plat2, err := model.NewPlatformServingWithNoAuth("34020099", policy2, false)
	if err != nil {
		t.Fatalf("NewPlatformServingWithNoAuth: %v", err)
	}
	if err := s.UpsertPlatform(ctx, in.ID, plat2, 15000); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}
	if n := countChildRows(t, s.db, in.ID.String(), false); n != 1 {
		t.Errorf("node_platforms rows after replace: got %d, want 1", n)
	}
	got, err := s.GetNode(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Platform.Realm() != "34020099" {
		t.Errorf("Platform.Realm: got %q, want %q", got.Platform.Realm(), "34020099")
	}
	if got.Platform.AllowNoAuth() {
		t.Errorf("Platform.AllowNoAuth: got true, want false")
	}
	if got.CatalogPollIntervalMs != 15000 {
		t.Errorf("CatalogPollIntervalMs: got %d, want 15000", got.CatalogPollIntervalMs)
	}
	if got.Platform.Policy().Min() != 30 || got.Platform.Policy().Default() != 600 || got.Platform.Policy().Max() != 1800 {
		t.Errorf("Platform.Policy: got (%d,%d,%d), want (30,600,1800)",
			got.Platform.Policy().Min(), got.Platform.Policy().Default(), got.Platform.Policy().Max())
	}
}

// TestUpdateNode_AdvancesUpdatedAt pins the store half of design.md R3:
// the optimistic-lock precondition on PUT /v1/nodes/:id is "the row's
// updated_at is still what the client read", so an update that fails to
// advance the column would let every writer win forever.
//
// The assertion deliberately avoids sleeping. datetime('now') has
// one-second resolution, so backdating the row to 2001 and then updating
// once tells the two apart without making the suite wait for a tick.
func TestUpdateNode_AdvancesUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := mkRegistrationNode(t, "34020000001320000012", false)

	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	backdateNode(t, s, in.ID)

	updated := in
	updated.Addr = "10.96.1.141:5060"
	if err := s.UpdateNode(ctx, in.ID, updated); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	if got := readUpdatedAt(t, s, in.ID); got <= backdateStamp {
		t.Errorf("nodes.updated_at: got %q, want strictly after %q", got, backdateStamp)
	}
}

// TestUpdateCascade_AdvancesUpdatedAtWithoutTouchingChildren covers the
// other writer of that column. The cascade-only PUT takes the cheap path,
// so it must still stamp nodes.updated_at — and must leave the
// registration and platform rows alone, because their timestamps are not
// part of the version the client read.
func TestUpdateCascade_AdvancesUpdatedAtWithoutTouchingChildren(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := mkRegistrationNode(t, "34020000001320000013", false)
	in.Platform = mustPlatform(t, false)
	in.HasPlatform = true
	in.CatalogPollIntervalMs = 15000

	if err := s.CreateNode(ctx, in); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	regBefore := readChildUpdatedAt(t, s, "node_registrations", in.ID)
	platBefore := readChildUpdatedAt(t, s, "node_platforms", in.ID)
	backdateNode(t, s, in.ID)

	if err := s.UpdateCascade(ctx, in.ID, "34020000002000000002", []string{"34020000001320000009"}); err != nil {
		t.Fatalf("UpdateCascade: %v", err)
	}

	if got := readUpdatedAt(t, s, in.ID); got <= backdateStamp {
		t.Errorf("nodes.updated_at: got %q, want strictly after %q", got, backdateStamp)
	}
	if got := readChildUpdatedAt(t, s, "node_registrations", in.ID); got != regBefore {
		t.Errorf("node_registrations.updated_at: got %q, want unchanged %q", got, regBefore)
	}
	if got := readChildUpdatedAt(t, s, "node_platforms", in.ID); got != platBefore {
		t.Errorf("node_platforms.updated_at: got %q, want unchanged %q", got, platBefore)
	}

	got, err := s.GetNode(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.CascadeParent != "34020000002000000002" {
		t.Errorf("CascadeParent: got %q, want %q", got.CascadeParent, "34020000002000000002")
	}
	if len(got.CascadeChildren) != 1 || got.CascadeChildren[0] != "34020000001320000009" {
		t.Errorf("CascadeChildren: got %v, want [34020000001320000009]", got.CascadeChildren)
	}
	if !got.HasRegistration || !got.HasPlatform {
		t.Errorf("child flags after cascade-only update: got reg=%v plat=%v, want both true",
			got.HasRegistration, got.HasPlatform)
	}
}

// backdateStamp is the timestamp the timestamp tests backdate a row to.
// It sits in 2001 so any real write lands after it.
const backdateStamp = "2001-01-01 00:00:00"

// backdateNode rewrites the node row's updated_at to backdateStamp so the
// next write is observable without sleeping across a second boundary.
// getNodeSQL deliberately does not expose the column — it is not part of
// the domain model — so the test reads and writes it through the raw DB.
func backdateNode(t *testing.T, s *Store, id model.NodeID) {
	t.Helper()
	res, err := s.db.Exec(`UPDATE nodes SET updated_at = ? WHERE id = ?`, backdateStamp, id.String())
	if err != nil {
		t.Fatalf("backdate nodes.updated_at: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("backdate nodes.updated_at: rows affected %d, want 1", n)
	}
}

func readUpdatedAt(t *testing.T, s *Store, id model.NodeID) string {
	t.Helper()
	var got string
	err := s.db.QueryRow(`SELECT updated_at FROM nodes WHERE id = ?`, id.String()).Scan(&got)
	if err != nil {
		t.Fatalf("read nodes.updated_at: %v", err)
	}
	return got
}

// readChildUpdatedAt reads updated_at out of a child table for one node.
// Child tables (node_registrations, node_platforms) key on node_id, so
// this helper hard-wires that column; it is not for the parent table.
func readChildUpdatedAt(t *testing.T, s *Store, table string, id model.NodeID) string {
	t.Helper()
	var got string
	err := s.db.QueryRow(`SELECT updated_at FROM `+table+` WHERE node_id = ?`, id.String()).Scan(&got)
	if err != nil {
		t.Fatalf("read %s.updated_at: %v", table, err)
	}
	return got
}

// mustPlatform builds a PlatformServing distinct from the schema defaults
// so an assertion on the round-trip can tell persistence from
// "I just read the column default".
func mustPlatform(t *testing.T, allowNoAuth bool) model.PlatformServing {
	t.Helper()
	plat, err := model.NewPlatformServingWithNoAuth("34020000", mustPolicy(t), allowNoAuth)
	if err != nil {
		t.Fatalf("NewPlatformServingWithNoAuth: %v", err)
	}
	return plat
}

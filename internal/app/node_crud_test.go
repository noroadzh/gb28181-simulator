// Unit tests for the node-configuration CRUD paths (Change
// runtime-node-configuration, tasks 2.1–2.6).
//
// The store is a fake with injectable failures: the interesting behaviour in
// this file is what happens when the two halves — memory and store — disagree,
// and a real database would make those failures hard to arrange. The registry
// is the package's own fakeCatalogue, so the transition table stays the real
// one.
package app

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/platform/events"
)

const (
	crudDeviceID = "34020000001310000001"
	crudLargeID  = "34020000002000000001"
	crudSmallID  = "34020000002160000001"
)

// fakeNodeStore is an in-memory NodeConfigurationStore whose failures can be
// armed per operation.
type fakeNodeStore struct {
	mu     sync.Mutex
	rows   map[string]model.StoredNode
	failOn map[string]error
	calls  []string
	list   []model.StoredNode // overrides rows when set (LoadFromStore tests)
}

func newFakeNodeStore() *fakeNodeStore {
	return &fakeNodeStore{rows: map[string]model.StoredNode{}, failOn: map[string]error{}}
}

// fail arms op (list/get/create/update/delete/cascade) to return err.
func (f *fakeNodeStore) fail(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOn[op] = err
}

func (f *fakeNodeStore) check(op string) error {
	if err, ok := f.failOn[op]; ok {
		return err
	}
	return nil
}

func (f *fakeNodeStore) ListNodes(context.Context) ([]model.StoredNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check("list"); err != nil {
		return nil, err
	}
	if f.list != nil {
		return append([]model.StoredNode(nil), f.list...), nil
	}
	out := make([]model.StoredNode, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeNodeStore) GetNode(_ context.Context, id model.NodeID) (model.StoredNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check("get"); err != nil {
		return model.StoredNode{}, err
	}
	row, ok := f.rows[id.String()]
	if !ok {
		return model.StoredNode{}, port.ErrNodeNotFound
	}
	return row, nil
}

func (f *fakeNodeStore) CreateNode(_ context.Context, in model.StoredNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create:"+in.ID.String())
	if err := f.check("create"); err != nil {
		return err
	}
	if _, ok := f.rows[in.ID.String()]; ok {
		return port.ErrNodeAlreadyExists
	}
	f.rows[in.ID.String()] = in
	return nil
}

func (f *fakeNodeStore) UpdateNode(_ context.Context, id model.NodeID, in model.StoredNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "update:"+id.String())
	if err := f.check("update"); err != nil {
		return err
	}
	if _, ok := f.rows[id.String()]; !ok {
		return port.ErrNodeNotFound
	}
	f.rows[id.String()] = in
	return nil
}

func (f *fakeNodeStore) DeleteNode(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "delete:"+id.String())
	if err := f.check("delete"); err != nil {
		return err
	}
	delete(f.rows, id.String())
	return nil
}

func (f *fakeNodeStore) UpdateCascade(_ context.Context, id model.NodeID, parent string, children []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "cascade:"+id.String())
	if err := f.check("cascade"); err != nil {
		return err
	}
	row, ok := f.rows[id.String()]
	if !ok {
		return port.ErrNodeNotFound
	}
	row.CascadeParent = parent
	row.CascadeChildren = append([]string(nil), children...)
	f.rows[id.String()] = row
	return nil
}

func (f *fakeNodeStore) UpsertRegistration(context.Context, model.NodeID, model.Registration) error {
	return nil
}

func (f *fakeNodeStore) UpsertPlatform(context.Context, model.NodeID, model.PlatformServing, int) error {
	return nil
}

func (f *fakeNodeStore) row(t *testing.T, id string) model.StoredNode {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		t.Fatalf("store has no row for %s", id)
	}
	return row
}

func (f *fakeNodeStore) has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.rows[id]
	return ok
}

// newCRUDHarness wires a service with the fake store and a real event hub.
func newCRUDHarness(t *testing.T) (*NodeService, *fakeCatalogue, *fakeNodeStore, *events.Hub) {
	t.Helper()
	svc, cat, _, _ := newTestService(t)
	store := newFakeNodeStore()
	if _, err := svc.WithNodeStore(store); err != nil {
		t.Fatalf("WithNodeStore: %v", err)
	}
	hub := events.NewHub(32)
	if _, err := svc.WithEvents(hub); err != nil {
		t.Fatalf("WithEvents: %v", err)
	}
	return svc, cat, store, hub
}

// storedNodeFor builds the definition a caller would post for id at addr.
func storedNodeFor(t *testing.T, id, addr string) model.StoredNode {
	t.Helper()
	prof := testProfile(t, id, addr)
	return model.StoredNode{
		ID:     prof.ID(),
		Kind:   prof.Kind(),
		Domain: prof.Domain(),
		Addr:   prof.Addr(),
		Vendor: prof.Vendor(),
	}
}

// forceOnline walks a node through the legal transitions to online, so tests
// can exercise the "not quiescent" refusals without starting a transport.
func forceOnline(t *testing.T, cat *fakeCatalogue, id model.NodeID) {
	t.Helper()
	ctx := context.Background()
	for _, st := range []model.Status{model.StatusRegistering, model.StatusRegistered, model.StatusOnline} {
		if _, err := cat.Advance(ctx, id, st); err != nil {
			t.Fatalf("advance %s to %s: %v", id, st, err)
		}
	}
}

// expectEvent waits for the next event of typ (and, when status is not
// empty, of that status), skipping the unrelated ones a create/update also
// emits. CreateNode stamps a status change before it publishes node_created,
// so callers cannot assume a single event per call.
func expectEvent(t *testing.T, sub *events.Subscriber, typ, status string) events.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-sub.Chan():
			if !ok {
				t.Fatal("event channel closed")
			}
			if ev.Type != typ {
				continue
			}
			if status != "" && ev.Status != status {
				continue
			}
			return ev
		case <-deadline:
			t.Fatalf("no %s event (status %q) was published", typ, status)
			return events.Event{}
		}
	}
}

// --- 2.1 create -----------------------------------------------------

func TestCreateNode_RegistersAndPersists(t *testing.T) {
	ctx := context.Background()
	svc, _, store, hub := newCRUDHarness(t)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	node, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061"))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if got := node.Status(); got != model.StatusIdle {
		t.Errorf("status = %v, want idle", got)
	}
	if !store.has(crudDeviceID) {
		t.Error("store has no row after CreateNode")
	}
	if ev := expectEvent(t, sub, events.TypeNodeCreated, ""); ev.ID != crudDeviceID {
		t.Errorf("event = %+v, want node_created for %s", ev, crudDeviceID)
	}
}

// The store is the source of truth: a definition that could not be persisted
// must not stay in memory, or it would vanish on the next boot.
func TestCreateNode_StoreFailureRollsBackMemory(t *testing.T) {
	ctx := context.Background()
	svc, cat, store, _ := newCRUDHarness(t)
	store.fail("create", errors.New("disk full"))

	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err == nil {
		t.Fatal("CreateNode succeeded despite a store failure")
	}
	if _, ok := cat.Get(ctx, mustNodeID(t, crudDeviceID)); ok {
		t.Error("node stayed in memory despite the store refusing the row")
	}
}

func TestCreateNode_DuplicateIsRefused(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newCRUDHarness(t)
	sn := storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")
	if _, err := svc.CreateNode(ctx, sn); err != nil {
		t.Fatalf("first CreateNode: %v", err)
	}
	if _, err := svc.CreateNode(ctx, sn); err == nil {
		t.Fatal("duplicate CreateNode succeeded")
	}
}

// --- 2.2 update -----------------------------------------------------

func TestUpdateNode_ReplacesProfileAndRow(t *testing.T) {
	ctx := context.Background()
	svc, cat, store, hub := newCRUDHarness(t)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	expectEvent(t, sub, events.TypeNodeCreated, "")

	updated := storedNodeFor(t, crudDeviceID, "127.0.0.1:15062")
	updated.Vendor = "other"
	if err := svc.UpdateNode(ctx, updated); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	node, ok := cat.Get(ctx, mustNodeID(t, crudDeviceID))
	if !ok {
		t.Fatal("node missing after update")
	}
	if got := node.Profile().Addr(); got != "127.0.0.1:15062" {
		t.Errorf("profile addr = %q, want 127.0.0.1:15062", got)
	}
	if got := store.row(t, crudDeviceID).Addr; got != "127.0.0.1:15062" {
		t.Errorf("stored addr = %q, want 127.0.0.1:15062", got)
	}
	if ev := expectEvent(t, sub, events.TypeNodeUpdated, ""); ev.ID != crudDeviceID {
		t.Errorf("event = %+v, want node_updated for %s", ev, crudDeviceID)
	}
}

func TestUpdateNode_StoreFailureRestoresProfile(t *testing.T) {
	ctx := context.Background()
	svc, cat, store, _ := newCRUDHarness(t)
	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	store.fail("update", errors.New("disk full"))

	if err := svc.UpdateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15062")); err == nil {
		t.Fatal("UpdateNode succeeded despite a store failure")
	}
	node, _ := cat.Get(ctx, mustNodeID(t, crudDeviceID))
	if got := node.Profile().Addr(); got != "127.0.0.1:15061" {
		t.Errorf("profile addr = %q after rollback, want the original 127.0.0.1:15061", got)
	}
}

func TestUpdateNode_RefusesLiveNode(t *testing.T) {
	ctx := context.Background()
	svc, cat, _, _ := newCRUDHarness(t)
	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	forceOnline(t, cat, mustNodeID(t, crudDeviceID))

	err := svc.UpdateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15062"))
	if !errors.Is(err, ErrNodeNotStoppable) {
		t.Fatalf("err = %v, want ErrNodeNotStoppable", err)
	}
}

// --- 2.3 delete -----------------------------------------------------

func TestDeleteNode_RemovesMemoryAndRow(t *testing.T) {
	ctx := context.Background()
	svc, cat, store, hub := newCRUDHarness(t)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	expectEvent(t, sub, events.TypeNodeCreated, "")

	if err := svc.DeleteNode(ctx, mustNodeID(t, crudDeviceID)); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	if _, ok := cat.Get(ctx, mustNodeID(t, crudDeviceID)); ok {
		t.Error("node still in memory after delete")
	}
	if store.has(crudDeviceID) {
		t.Error("row still in store after delete")
	}
	if ev := expectEvent(t, sub, events.TypeNodeDeleted, ""); ev.ID != crudDeviceID {
		t.Errorf("event = %+v, want node_deleted for %s", ev, crudDeviceID)
	}
}

func TestDeleteNode_RefusesLiveNode(t *testing.T) {
	ctx := context.Background()
	svc, cat, store, _ := newCRUDHarness(t)
	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	forceOnline(t, cat, mustNodeID(t, crudDeviceID))

	err := svc.DeleteNode(ctx, mustNodeID(t, crudDeviceID))
	if !errors.Is(err, ErrNodeNotStoppable) {
		t.Fatalf("err = %v, want ErrNodeNotStoppable", err)
	}
	if !store.has(crudDeviceID) {
		t.Error("row was removed from the store despite the refusal")
	}
}

// A store failure after the memory side went down puts the node back rather
// than leaving the process without a definition the store still holds.
func TestDeleteNode_StoreFailureRestoresNode(t *testing.T) {
	ctx := context.Background()
	svc, cat, store, _ := newCRUDHarness(t)
	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	store.fail("delete", errors.New("disk full"))

	if err := svc.DeleteNode(ctx, mustNodeID(t, crudDeviceID)); err == nil {
		t.Fatal("DeleteNode succeeded despite a store failure")
	}
	if _, ok := cat.Get(ctx, mustNodeID(t, crudDeviceID)); !ok {
		t.Error("node was not restored after the store refused the delete")
	}
}

// --- 2.4 cascade ----------------------------------------------------

// seedThree creates large <- small <- device and returns their ids.
func seedThree(t *testing.T, svc *NodeService) {
	t.Helper()
	ctx := context.Background()
	for _, n := range []struct{ id, addr string }{
		{crudLargeID, "127.0.0.1:15071"},
		{crudSmallID, "127.0.0.1:15072"},
		{crudDeviceID, "127.0.0.1:15073"},
	} {
		if _, err := svc.CreateNode(ctx, storedNodeFor(t, n.id, n.addr)); err != nil {
			t.Fatalf("CreateNode %s: %v", n.id, err)
		}
	}
}

func TestUpdateCascade_PersistsEdgesAndUpdatesParent(t *testing.T) {
	ctx := context.Background()
	svc, cat, store, _ := newCRUDHarness(t)
	seedThree(t, svc)

	if err := svc.UpdateCascade(ctx, mustNodeID(t, crudSmallID), crudLargeID, []string{crudDeviceID}); err != nil {
		t.Fatalf("UpdateCascade: %v", err)
	}

	small, _ := cat.Get(ctx, mustNodeID(t, crudSmallID))
	if got := small.Profile().CascadeParent(); got != crudLargeID {
		t.Errorf("small parent = %q, want %q", got, crudLargeID)
	}
	if got := small.Profile().CascadeChildren(); !reflect.DeepEqual(got, []string{crudDeviceID}) {
		t.Errorf("small children = %v, want [%s]", got, crudDeviceID)
	}
	row := store.row(t, crudSmallID)
	if row.CascadeParent != crudLargeID || !reflect.DeepEqual(row.CascadeChildren, []string{crudDeviceID}) {
		t.Errorf("stored edges = (%q, %v), want (%q, [%s])",
			row.CascadeParent, row.CascadeChildren, crudLargeID, crudDeviceID)
	}
	// The parent learns the child without a second call.
	large, _ := cat.Get(ctx, mustNodeID(t, crudLargeID))
	if got := large.Profile().CascadeChildren(); !reflect.DeepEqual(got, []string{crudSmallID}) {
		t.Errorf("large children = %v, want [%s]", got, crudSmallID)
	}
}

func TestUpdateCascade_RefusesSelfParent(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newCRUDHarness(t)
	seedThree(t, svc)

	err := svc.UpdateCascade(ctx, mustNodeID(t, crudSmallID), crudSmallID, nil)
	if !errors.Is(err, ErrCascadeSelfParent) {
		t.Fatalf("err = %v, want ErrCascadeSelfParent", err)
	}
}

func TestUpdateCascade_RefusesAncestorCycle(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newCRUDHarness(t)
	seedThree(t, svc)

	if err := svc.UpdateCascade(ctx, mustNodeID(t, crudSmallID), crudLargeID, nil); err != nil {
		t.Fatalf("set small parent: %v", err)
	}
	// large -> small -> large would close a loop.
	err := svc.UpdateCascade(ctx, mustNodeID(t, crudLargeID), crudSmallID, nil)
	if !errors.Is(err, ErrCascadeCycle) {
		t.Fatalf("err = %v, want ErrCascadeCycle", err)
	}
}

func TestUpdateCascade_RefusesUnknownEdges(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newCRUDHarness(t)
	seedThree(t, svc)

	const unknown = "34020000002000000009" // a valid id this process does not host

	if err := svc.UpdateCascade(ctx, mustNodeID(t, crudSmallID), unknown, nil); !errors.Is(err, ErrCascadeParentNotFound) {
		t.Errorf("unknown parent: err = %v, want ErrCascadeParentNotFound", err)
	}
	if err := svc.UpdateCascade(ctx, mustNodeID(t, crudSmallID), "", []string{unknown}); !errors.Is(err, ErrCascadeChildNotFound) {
		t.Errorf("unknown child: err = %v, want ErrCascadeChildNotFound", err)
	}
	if err := svc.UpdateCascade(ctx, mustNodeID(t, crudSmallID), "not-an-id", nil); !errors.Is(err, ErrCascadeInvalidID) {
		t.Errorf("malformed parent: err = %v, want ErrCascadeInvalidID", err)
	}
}

func TestUpdateCascade_RefusesLiveNode(t *testing.T) {
	ctx := context.Background()
	svc, cat, _, _ := newCRUDHarness(t)
	seedThree(t, svc)
	forceOnline(t, cat, mustNodeID(t, crudSmallID))

	if err := svc.UpdateCascade(ctx, mustNodeID(t, crudSmallID), crudLargeID, nil); !errors.Is(err, ErrNodeNotStoppable) {
		t.Fatalf("err = %v, want ErrNodeNotStoppable", err)
	}
}

// --- 2.5 events -----------------------------------------------------

// Status transitions are domain facts too: a dashboard that never sees them
// would show a stale card until the next poll.
func TestNodeStatusChangesArePublished(t *testing.T) {
	ctx := context.Background()
	svc, cat, _, hub := newCRUDHarness(t)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	expectEvent(t, sub, events.TypeNodeCreated, "")
	forceOnline(t, cat, mustNodeID(t, crudDeviceID))

	if err := svc.Stop(ctx, mustNodeID(t, crudDeviceID)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	ev := expectEvent(t, sub, events.TypeNodeStatusChanged, model.StatusOffline.String())
	if ev.ID != crudDeviceID {
		t.Errorf("event id = %q, want %q", ev.ID, crudDeviceID)
	}
}

// --- 2.6 one profile contract --------------------------------------

// The boot path and the API path must build the same profile from the same
// definition; otherwise a node changes shape when the process restarts.
func TestLoadFromStore_MatchesProgrammaticCreate(t *testing.T) {
	ctx := context.Background()

	// API path: CreateNode over a store.
	svcA, _, store, _ := newCRUDHarness(t)
	sn := storedNodeFor(t, crudSmallID, "127.0.0.1:15072")
	sn.CascadeParent = crudLargeID
	sn.CascadeChildren = []string{crudDeviceID}
	if _, err := svcA.CreateNode(ctx, sn); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	apiProfile := svcA.List(ctx)[0].Profile()

	// Boot path: the same row, loaded by a fresh service.
	svcB, _, _, _ := newTestServiceFromStore(t, store)
	if err := svcB.LoadFromStore(ctx); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	loaded := svcB.List(ctx)
	if len(loaded) != 1 {
		t.Fatalf("LoadFromStore registered %d nodes, want 1", len(loaded))
	}
	if !reflect.DeepEqual(apiProfile, loaded[0].Profile()) {
		t.Errorf("profiles differ:\n API  = %#v\n boot = %#v", apiProfile, loaded[0].Profile())
	}
}

// newTestServiceFromStore builds a second, independent service over an
// existing store, the way a restarted process would.
func newTestServiceFromStore(t *testing.T, store port.NodeConfigurationStore) (*NodeService, *fakeCatalogue, *fakeLifecycle, *fakeClock) {
	t.Helper()
	svc, cat, lc, clock := newTestService(t)
	if _, err := svc.WithNodeStore(store); err != nil {
		t.Fatalf("WithNodeStore: %v", err)
	}
	return svc, cat, lc, clock
}

func mustNodeID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

// Task 4.2: taking a node online is a domain fact the dashboard subscribes
// to, and it must be published exactly once — a duplicate would make the
// WebUI re-render the same card, a missing one would leave it stale.
func TestStartPublishesOnlineOnce(t *testing.T) {
	ctx := context.Background()
	svc, _, _, hub := newCRUDHarness(t)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	if _, err := svc.CreateNode(ctx, storedNodeFor(t, crudDeviceID, "127.0.0.1:15061")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	expectEvent(t, sub, events.TypeNodeCreated, "")

	if err := svc.Start(ctx, mustNodeID(t, crudDeviceID)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev := expectEvent(t, sub, events.TypeNodeStatusChanged, model.StatusOnline.String())
	if ev.ID != crudDeviceID {
		t.Errorf("event id = %q, want %q", ev.ID, crudDeviceID)
	}

	select {
	case extra := <-sub.Chan():
		if extra.Type == events.TypeNodeStatusChanged && extra.Status == model.StatusOnline.String() {
			t.Errorf("online status was published twice: %+v", extra)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

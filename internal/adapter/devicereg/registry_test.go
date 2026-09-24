package devicereg

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func mustNodeID(t *testing.T, id string) model.NodeID {
	t.Helper()
	nodeID, err := model.ParseNodeID(id)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", id, err)
	}
	return nodeID
}

func mustDevice(t *testing.T, id, addr string, at time.Time) model.DownstreamDevice {
	t.Helper()
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{DeviceID: id, Addr: addr, Now: at})
	if err != nil {
		t.Fatalf("NewDownstreamDevice(%q): %v", id, err)
	}
	return dev
}

// Two platform nodes must never see each other's devices: the table is
// what an operator reads to know who is actually talking to which node.
func TestRegistry_PartitionedByNode(t *testing.T) {
	ctx := context.Background()
	r := New()
	first := mustNodeID(t, "34020000002000000001")
	second := mustNodeID(t, "34020000002000000002")
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	if err := r.Upsert(ctx, first, mustDevice(t, "34020000011310000001", "127.0.0.1:15060", at)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := r.Upsert(ctx, second, mustDevice(t, "34020000011310000002", "127.0.0.1:15070", at)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if got := r.List(ctx, first); len(got) != 1 || got[0].DeviceID() != "34020000011310000001" {
		t.Errorf("node 1 sees %v, want only 34020000011310000001", got)
	}
	if got := r.List(ctx, second); len(got) != 1 || got[0].DeviceID() != "34020000011310000002" {
		t.Errorf("node 2 sees %v, want only 34020000011310000002", got)
	}
	if _, ok := r.Lookup(ctx, first, "34020000011310000002"); ok {
		t.Error("node 1 can look up node 2's device")
	}
}

// Re-registering refreshes the row in place — the device id is the key.
func TestRegistry_UpsertReplaces(t *testing.T) {
	ctx := context.Background()
	r := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	first := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	second := first.Add(30 * time.Minute)

	dev := mustDevice(t, "34020000011310000001", "127.0.0.1:15060", first)
	if err := r.Upsert(ctx, nodeID, dev); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := r.Upsert(ctx, nodeID, dev.WithGranted(3600, second)); err != nil {
		t.Fatalf("Upsert (refresh): %v", err)
	}
	got := r.List(ctx, nodeID)
	if len(got) != 1 {
		t.Fatalf("List = %d rows, want 1", len(got))
	}
	if got[0].GrantedExpiry() != 3600 || !got[0].RegisteredAt().Equal(second) {
		t.Errorf("row = %ds registered at %v, want 3600s at %v",
			got[0].GrantedExpiry(), got[0].RegisteredAt(), second)
	}
	if r.Count(nodeID) != 1 {
		t.Errorf("Count = %d, want 1", r.Count(nodeID))
	}
}

func TestRegistry_RemoveAndClear(t *testing.T) {
	ctx := context.Background()
	r := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	if err := r.Upsert(ctx, nodeID, mustDevice(t, "34020000011310000001", "127.0.0.1:15060", at)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Removing an absent device is a no-op, not an error.
	if err := r.Remove(ctx, nodeID, "34020000041310000099"); err != nil {
		t.Errorf("Remove(absent) = %v, want nil", err)
	}
	if err := r.Remove(ctx, nodeID, "34020000011310000001"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := r.Lookup(ctx, nodeID, "34020000011310000001"); ok {
		t.Error("device still present after Remove")
	}
	if err := r.Upsert(ctx, nodeID, mustDevice(t, "34020000011310000001", "127.0.0.1:15060", at)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := r.Clear(ctx, nodeID); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got := r.List(ctx, nodeID); len(got) != 0 {
		t.Errorf("List after Clear = %v, want empty", got)
	}
}

// List is ordered so HTTP responses and assertions are stable without
// sorting of their own.
func TestRegistry_ListOrdered(t *testing.T) {
	ctx := context.Background()
	r := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"34020000011310000003", "34020000011310000001", "34020000011310000002"} {
		if err := r.Upsert(ctx, nodeID, mustDevice(t, id, "127.0.0.1:15060", at)); err != nil {
			t.Fatalf("Upsert(%s): %v", id, err)
		}
	}
	got := r.List(ctx, nodeID)
	want := []string{"34020000011310000001", "34020000011310000002", "34020000011310000003"}
	if len(got) != len(want) {
		t.Fatalf("List = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].DeviceID() != want[i] {
			t.Errorf("row %d = %s, want %s", i, got[i].DeviceID(), want[i])
		}
	}
}

func TestRegistry_RejectsUnconstructedDevice(t *testing.T) {
	r := New()
	if err := r.Upsert(context.Background(), mustNodeID(t, "34020000002000000001"), model.DownstreamDevice{}); err == nil {
		t.Fatal("Upsert with an empty device: got nil, want an error")
	}
}

// The serving goroutine writes while HTTP readers read, so the table has to
// survive concurrent use (run with -race).
func TestRegistry_ConcurrentUse(t *testing.T) {
	ctx := context.Background()
	r := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "3402000001131000000" + string(rune('0'+i))
			if err := r.Upsert(ctx, nodeID, mustDevice(t, id, "127.0.0.1:15060", at)); err != nil {
				t.Errorf("Upsert: %v", err)
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.List(ctx, nodeID)
			_, _ = r.Lookup(ctx, nodeID, "34020000011310000001")
			_ = r.Count(nodeID)
		}()
	}
	wg.Wait()
	if got := r.Count(nodeID); got != 8 {
		t.Errorf("Count = %d, want 8", got)
	}
}

// A cancelled context stops writes: a shutdown must not keep mutating the
// table while it is being read for the last time.
func TestRegistry_HonoursContext(t *testing.T) {
	r := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Upsert(ctx, nodeID, mustDevice(t, "34020000011310000001", "127.0.0.1:15060", at)); err == nil {
		t.Fatal("Upsert with a cancelled context: got nil, want an error")
	}
	if got := r.List(ctx, nodeID); got != nil {
		t.Errorf("List with a cancelled context = %v, want nil", got)
	}
}

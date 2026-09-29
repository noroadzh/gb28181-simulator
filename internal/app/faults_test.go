package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

type fakeNodeRegistry struct {
	nodes map[model.NodeID]bool
}

func (r *fakeNodeRegistry) Get(_ context.Context, id model.NodeID) (model.Node, bool) {
	_, ok := r.nodes[id]
	return model.Node{}, ok
}

func faultNodeID(id string) model.NodeID {
	n, err := model.ParseNodeID(id)
	if err != nil {
		panic(err)
	}
	return n
}

func TestFaultStore_InstallUnknownNodeRejected(t *testing.T) {
	t.Parallel()
	reg := &fakeNodeRegistry{nodes: map[model.NodeID]bool{}}
	s := NewFaultStore(reg, nil)
	err := s.Install(context.Background(), faultNodeID("34020000001320000001"), model.FaultProfile{})
	if !errors.Is(err, model.ErrUnknownNode) {
		t.Fatalf("Install(unknown) = %v, want ErrUnknownNode", err)
	}
}

func TestFaultStore_InstallInvalidProfileRejected(t *testing.T) {
	t.Parallel()
	id := faultNodeID("34020000001320000001")
	reg := &fakeNodeRegistry{nodes: map[model.NodeID]bool{id: true}}
	s := NewFaultStore(reg, nil)
	err := s.Install(context.Background(), id, model.FaultProfile{Canned: map[string]int{"invite": 200}})
	if err == nil {
		t.Fatal("Install(invalid profile) = nil, want validation error")
	}
	if _, ok := s.Get(context.Background(), id); ok {
		t.Fatal("invalid profile must not be installed")
	}
}

func TestFaultStore_InstallGetClear(t *testing.T) {
	t.Parallel()
	id := faultNodeID("34020000001320000001")
	reg := &fakeNodeRegistry{nodes: map[model.NodeID]bool{id: true}}
	s := NewFaultStore(reg, nil)
	ctx := context.Background()

	// default: no profile
	if _, ok := s.Get(ctx, id); ok {
		t.Fatal("fresh node should have no profile")
	}

	p := model.FaultProfile{
		Canned: map[string]int{"REGISTER": 403},
		Drop:   0.25,
		Delay:  model.FaultDelayConfig{Base: 100 * time.Millisecond},
	}
	if err := s.Install(ctx, id, p); err != nil {
		t.Fatalf("Install = %v", err)
	}
	got, ok := s.Get(ctx, id)
	if !ok {
		t.Fatal("installed profile should be present")
	}
	if got.Canned["REGISTER"] != 403 || got.Drop != 0.25 {
		t.Fatalf("Get = %+v, want installed profile", got)
	}

	if err := s.Clear(ctx, id); err != nil {
		t.Fatalf("Clear = %v", err)
	}
	if _, ok := s.Get(ctx, id); ok {
		t.Fatal("cleared node should have no profile")
	}
	// clearing again is a no-op
	if err := s.Clear(ctx, id); err != nil {
		t.Fatalf("second Clear = %v, want nil", err)
	}
}

func TestFaultStore_InstallResetsCounters(t *testing.T) {
	t.Parallel()
	id := faultNodeID("34020000001320000001")
	reg := &fakeNodeRegistry{nodes: map[model.NodeID]bool{id: true}}
	s := NewFaultStore(reg, nil)
	ctx := context.Background()

	if err := s.Install(ctx, id, model.FaultProfile{Canned: map[string]int{"REGISTER": 403}}); err != nil {
		t.Fatal(err)
	}
	s.Record(id, model.FaultCannedResponse)
	if got := s.FaultCounters(id)[model.FaultCannedResponse]; got != 1 {
		t.Fatalf("counters after record = %d, want 1", got)
	}
	// reinstalling resets counters
	if err := s.Install(ctx, id, model.FaultProfile{Canned: map[string]int{"REGISTER": 501}}); err != nil {
		t.Fatal(err)
	}
	if got := s.FaultCounters(id)[model.FaultCannedResponse]; got != 0 {
		t.Fatalf("counters after reinstall = %d, want 0", got)
	}
}

func TestFaultStore_RecordAndCounters(t *testing.T) {
	t.Parallel()
	id := faultNodeID("34020000001320000001")
	s := NewFaultStore(&fakeNodeRegistry{nodes: map[model.NodeID]bool{id: true}}, nil)
	s.Record(id, model.FaultDrop)
	s.Record(id, model.FaultDrop)
	s.Record(id, model.FaultBlackhole)
	cs := s.FaultCounters(id)
	if cs[model.FaultDrop] != 2 {
		t.Fatalf("drop counter = %d, want 2", cs[model.FaultDrop])
	}
	if cs[model.FaultBlackhole] != 1 {
		t.Fatalf("blackhole counter = %d, want 1", cs[model.FaultBlackhole])
	}
	// Clear resets counters
	if err := s.Clear(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if got := s.FaultCounters(id)[model.FaultDrop]; got != 0 {
		t.Fatalf("counter after clear = %d, want 0", got)
	}
}

package scenario

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

type memFaultStore struct {
	profiles map[model.NodeID]model.FaultProfile
}

func newMemFaultStore() *memFaultStore {
	return &memFaultStore{profiles: make(map[model.NodeID]model.FaultProfile)}
}

func (m *memFaultStore) Install(_ context.Context, id model.NodeID, p model.FaultProfile) error {
	if p.Drop < 0 || p.Drop > 1 {
		return model.ErrUnknownNode
	}
	m.profiles[id] = p
	return nil
}

func (m *memFaultStore) Clear(_ context.Context, id model.NodeID) error {
	delete(m.profiles, id)
	return nil
}

func (m *memFaultStore) Get(_ context.Context, id model.NodeID) (model.FaultProfile, bool) {
	p, ok := m.profiles[id]
	return p, ok
}

func runFault(t *testing.T, params map[string]any) (model.StepStatus, string, *memFaultStore) {
	t.Helper()
	store := newMemFaultStore()
	e := NewExecutor()
	RegisterInjectFaultSteps(e, store)
	sc := model.Scenario{Name: "fault", Steps: []model.Step{{Type: "inject-fault", Timeout: time.Second, Params: params}}}
	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	return report.Total, report.Steps[0].Error, store
}

func mustNodeID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID = %v", err)
	}
	return id
}

func TestInjectFaultStep_ArmsCannedResponse(t *testing.T) {
	total, _, store := runFault(t, map[string]any{
		"node":   "34020000001310000001",
		"canned": map[string]any{"REGISTER": float64(503)},
	})
	if total != model.StepPassed {
		t.Fatalf("Total = %q, want passed", total)
	}
	p, ok := store.profiles[mustNodeID(t, "34020000001310000001")]
	if !ok {
		t.Fatal("profile not installed")
	}
	if p.Canned["REGISTER"] != 503 {
		t.Fatalf("Canned[REGISTER] = %d, want 503", p.Canned["REGISTER"])
	}
}

func TestInjectFaultStep_DropAndBlackhole(t *testing.T) {
	total, _, store := runFault(t, map[string]any{
		"node":      "34020000001310000001",
		"drop":      0.5,
		"blackhole": []any{"MESSAGE"},
	})
	if total != model.StepPassed {
		t.Fatalf("Total = %q, want passed", total)
	}
	p := store.profiles[mustNodeID(t, "34020000001310000001")]
	if p.Drop != 0.5 {
		t.Fatalf("Drop = %v, want 0.5", p.Drop)
	}
	if len(p.Blackhole) != 1 || p.Blackhole[0] != "MESSAGE" {
		t.Fatalf("Blackhole = %v, want [MESSAGE]", p.Blackhole)
	}
}

func TestInjectFaultStep_InvalidProfile(t *testing.T) {
	total, _, _ := runFault(t, map[string]any{
		"node": "34020000001310000001",
		"drop": 2.0, // out of [0,1]
	})
	if total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", total)
	}
}

func TestInjectFaultStep_MissingID(t *testing.T) {
	total, errText, _ := runFault(t, map[string]any{
		"canned": map[string]any{"REGISTER": float64(503)},
	})
	if total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", total)
	}
	if !strings.Contains(errText, "params.node is required") {
		t.Fatalf("error = %q, want params.node is required", errText)
	}
}

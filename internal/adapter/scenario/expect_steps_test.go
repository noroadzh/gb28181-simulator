package scenario

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// fakeQuery is a minimal ExpectQuery stub backed by a map.
type fakeQuery struct{ nodes map[model.NodeID]model.Node }

func (f fakeQuery) Get(_ context.Context, id model.NodeID) (model.Node, bool) {
	n, ok := f.nodes[id]
	return n, ok
}

func onlineNode(t *testing.T) model.Node {
	t.Helper()
	profile, err := model.NewNodeProfile("34020000001320000001", "127.0.0.1:15060", "3402000000", "TestVendor")
	if err != nil {
		t.Fatalf("NewNodeProfile = %v", err)
	}
	return model.NewNode(profile)
}

func runExpect(t *testing.T, q ExpectQuery, params map[string]any) (model.StepStatus, string) {
	t.Helper()
	e := NewExecutor()
	RegisterExpectSteps(e, q)
	sc := model.Scenario{Name: "expect", Steps: []model.Step{{Type: "expect", Timeout: time.Second, Params: params}}}
	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	return report.Total, report.Steps[0].Error
}

func TestExpectStep_EqPasses(t *testing.T) {
	n := onlineNode(t)
	total, errText := runExpect(t, fakeQuery{nodes: map[model.NodeID]model.Node{n.ID(): n}}, map[string]any{
		"target": "node.34020000001320000001.status",
		"op":     "eq",
		"value":  "idle",
	})
	if total != model.StepPassed {
		t.Fatalf("Total = %q err=%q, want passed", total, errText)
	}
}

func TestExpectStep_EqFails(t *testing.T) {
	n := onlineNode(t)
	total, errText := runExpect(t, fakeQuery{nodes: map[model.NodeID]model.Node{n.ID(): n}}, map[string]any{
		"target": "node.34020000001320000001.status",
		"op":     "eq",
		"value":  "online",
	})
	if total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", total)
	}
	if !containsAll(errText, `= "idle"`, `"online"`) {
		t.Fatalf("error = %q, want got/want pair", errText)
	}
}

func TestExpectStep_Contains(t *testing.T) {
	n := onlineNode(t)
	total, _ := runExpect(t, fakeQuery{nodes: map[model.NodeID]model.Node{n.ID(): n}}, map[string]any{
		"target": "node.34020000001320000001.status",
		"op":     "contains",
		"value":  "idl",
	})
	if total != model.StepPassed {
		t.Fatalf("Total = %q, want passed", total)
	}
}

func TestExpectStep_UnknownNode(t *testing.T) {
	total, errText := runExpect(t, fakeQuery{nodes: map[model.NodeID]model.Node{}}, map[string]any{
		"target": "node.34020000001320000001.status",
		"op":     "eq",
		"value":  "idle",
	})
	if total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", total)
	}
	if !contains(errText, "not found") {
		t.Fatalf("error = %q, want not-found text", errText)
	}
}

func TestExpectStep_UnsupportedTargetRejected(t *testing.T) {
	n := onlineNode(t)
	total, errText := runExpect(t, fakeQuery{nodes: map[model.NodeID]model.Node{n.ID(): n}}, map[string]any{
		"target": "run.steps.0.status",
		"op":     "eq",
		"value":  "passed",
	})
	if total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", total)
	}
	if !contains(errText, "unsupported target") {
		t.Fatalf("error = %q, want unsupported-target text", errText)
	}
}

func TestExpectStep_UnsupportedOpRejected(t *testing.T) {
	n := onlineNode(t)
	total, errText := runExpect(t, fakeQuery{nodes: map[model.NodeID]model.Node{n.ID(): n}}, map[string]any{
		"target": "node.34020000001320000001.status",
		"op":     "regex",
		"value":  ".*",
	})
	if total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", total)
	}
	if !contains(errText, "unsupported op") {
		t.Fatalf("error = %q, want unsupported-op text", errText)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

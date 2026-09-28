package scenario

import (
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func TestRenderGolden(t *testing.T) {
	r := model.RunReport{
		ScenarioName: "register-keepalive-catalog",
		Total:        model.StepPassed,
		StartedAt:    time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
		FinishedAt:   time.Date(2026, 9, 27, 10, 0, 5, 0, time.UTC),
		Steps: []model.StepResult{
			{Type: "create-node", Status: model.StepPassed, DurationMS: 1200},
			{Type: "start-node", Status: model.StepPassed, DurationMS: 800},
			{Type: "wait", Status: model.StepPassed, DurationMS: 3000},
			{Type: "expect", Status: model.StepPassed, DurationMS: 50},
		},
	}

	jsonBytes, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"scenario_name": "register-keepalive-catalog"`) {
		t.Fatalf("unexpected JSON: %s", jsonBytes)
	}
	if !strings.Contains(string(jsonBytes), `"total": "passed"`) {
		t.Fatalf("missing total in JSON: %s", jsonBytes)
	}
	if !strings.Contains(string(jsonBytes), `"type": "create-node"`) {
		t.Fatalf("missing step type in JSON: %s", jsonBytes)
	}

	md := RenderMarkdown(r)
	for _, s := range []string{"register-keepalive-catalog", "passed", "create-node", "wait", "expect"} {
		if !strings.Contains(md, s) {
			t.Fatalf("Markdown missing %q:\n%s", s, md)
		}
	}
	if !strings.Contains(md, "| # | Type | Status | DurationMS | Error |") {
		t.Fatalf("Markdown missing table header:\n%s", md)
	}
}

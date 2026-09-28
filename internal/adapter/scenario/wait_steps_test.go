package scenario

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func TestWaitStep_SleepsAndCompletes(t *testing.T) {
	e := NewExecutor()
	RegisterWaitSteps(e)

	sc := model.Scenario{Name: "wait", Steps: []model.Step{
		{Type: "wait", Timeout: 3 * time.Second, Params: map[string]any{"seconds": 2}},
	}}
	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepPassed {
		t.Fatalf("Total = %q, want passed", report.Total)
	}
	if report.Steps[0].Status != model.StepPassed {
		t.Fatalf("step = %q, want passed", report.Steps[0].Status)
	}
	if report.Steps[0].DurationMS < 1500 {
		t.Fatalf("duration = %dms, want at least 1500ms", report.Steps[0].DurationMS)
	}
}

func TestWaitStep_TimeoutExceededFails(t *testing.T) {
	e := NewExecutor()
	RegisterWaitSteps(e)

	sc := model.Scenario{Name: "wait", Steps: []model.Step{
		{Type: "wait", Timeout: 500 * time.Millisecond, Params: map[string]any{"seconds": 10}},
	}}
	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
	if report.Steps[0].Status != model.StepFailed {
		t.Fatalf("step = %q, want failed", report.Steps[0].Status)
	}
	if !strings.Contains(report.Steps[0].Error, "wait:") {
		t.Fatalf("error = %q, want wait: prefix", report.Steps[0].Error)
	}
	if report.Steps[0].DurationMS > 1500 {
		t.Fatalf("duration = %dms, want near 500ms (timeout)", report.Steps[0].DurationMS)
	}
}

func TestWaitStep_MissingSeconds(t *testing.T) {
	e := NewExecutor()
	RegisterWaitSteps(e)

	sc := model.Scenario{Name: "wait", Steps: []model.Step{{Type: "wait", Timeout: time.Second}}}
	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
	if !strings.Contains(report.Steps[0].Error, "seconds must be a positive integer") {
		t.Fatalf("error = %q, want positive-seconds text", report.Steps[0].Error)
	}
}

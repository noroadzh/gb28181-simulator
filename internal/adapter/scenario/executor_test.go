package scenario

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// okHandler builds a no-op StepHandler for the given step type.
func okHandler(t string) handlerFunc {
	return handlerFunc(func(ctx context.Context, step model.Step) error { return nil })
}

func TestExecutor_Run_AllPassed(t *testing.T) {
	e := NewExecutor()
	e.Register("wait", okHandler("wait"))
	e.Register("create-node", okHandler("create-node"))

	sc := model.Scenario{Name: "drill", Steps: []model.Step{
		{Type: "create-node"},
		{Type: "wait", Timeout: time.Second},
	}}

	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepPassed {
		t.Fatalf("Total = %q, want passed", report.Total)
	}
	if len(report.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(report.Steps))
	}
	for i, s := range report.Steps {
		if s.Status != model.StepPassed {
			t.Fatalf("step %d status = %q, want passed", i, s.Status)
		}
	}
	if report.FinishedAt.Before(report.StartedAt) {
		t.Fatalf("FinishedAt %v before StartedAt %v", report.FinishedAt, report.StartedAt)
	}
}

func TestExecutor_Run_FailureStopsAndSkipsRest(t *testing.T) {
	e := NewExecutor()
	e.Register("create-node", okHandler("create-node"))
	e.Register("expect", handlerFunc(func(ctx context.Context, step model.Step) error {
		return errors.New("assertion failed: node never came online")
	}))
	e.Register("stop-node", okHandler("stop-node"))

	sc := model.Scenario{Name: "drill", Steps: []model.Step{
		{Type: "create-node"},
		{Type: "expect"},
		{Type: "stop-node"},
	}}

	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
	if report.Steps[0].Status != model.StepPassed {
		t.Fatalf("step0 = %q, want passed", report.Steps[0].Status)
	}
	if report.Steps[1].Status != model.StepFailed {
		t.Fatalf("step1 = %q, want failed", report.Steps[1].Status)
	}
	if !strings.Contains(report.Steps[1].Error, "node never came online") {
		t.Fatalf("step1 error = %q, want assertion text", report.Steps[1].Error)
	}
	if report.Steps[2].Status != model.StepSkipped {
		t.Fatalf("step2 = %q, want skipped", report.Steps[2].Status)
	}
	if len(report.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want 3 (skipped entries recorded)", len(report.Steps))
	}
}

func TestExecutor_Run_StepTimeout(t *testing.T) {
	e := NewExecutor()
	e.Register("wait", handlerFunc(func(ctx context.Context, step model.Step) error {
		select {
		case <-time.After(5 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))

	sc := model.Scenario{Name: "drill", Steps: []model.Step{
		{Type: "wait", Timeout: 100 * time.Millisecond},
	}}

	start := time.Now()
	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
	if report.Steps[0].Status != model.StepFailed {
		t.Fatalf("step0 = %q, want failed", report.Steps[0].Status)
	}
	if report.Steps[0].Error == "" {
		t.Fatalf("step0 error empty, want timeout text")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Run took %v, timeout did not fire", elapsed)
	}
}

func TestExecutor_Run_UnknownStepTypeFails(t *testing.T) {
	e := NewExecutor()
	sc := model.Scenario{Name: "drill", Steps: []model.Step{{Type: "teleport"}}}

	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
	if !strings.Contains(report.Steps[0].Error, "no handler registered") {
		t.Fatalf("step0 error = %q, want missing-handler text", report.Steps[0].Error)
	}
}

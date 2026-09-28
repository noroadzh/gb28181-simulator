package scenario

import (
	"context"
	"errors"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

type fakeBehaviorController struct {
	alarms map[model.NodeID][]app.AlarmInput
}

func newFakeBehaviorController() *fakeBehaviorController {
	return &fakeBehaviorController{alarms: make(map[model.NodeID][]app.AlarmInput)}
}

func (f *fakeBehaviorController) TriggerAlarm(_ context.Context, id model.NodeID, in app.AlarmInput) (model.AlarmSnapshot, error) {
	f.alarms[id] = append(f.alarms[id], in)
	return model.AlarmSnapshot{}, nil
}

func TestCommandSteps_SendAlarm_Success(t *testing.T) {
	ctrl := newFakeBehaviorController()
	e := NewExecutor()
	RegisterCommandSteps(e, ctrl)

	sc := model.Scenario{Name: "drill", Steps: []model.Step{
		{Type: "send-command", Params: map[string]any{
			"node":        "34020000001320000001",
			"action":      "alarm",
			"description": "motion",
		}},
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
}

func TestCommandSteps_SendAlarm_UnknownAction(t *testing.T) {
	e := NewExecutor()
	RegisterCommandSteps(e, nil)

	sc := model.Scenario{Name: "drill", Steps: []model.Step{
		{Type: "send-command", Params: map[string]any{"action": "teleport"}},
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
	if !errors.Is(err, nil) && report.Steps[0].Error == "" {
		t.Fatalf("error empty")
	}
}

func TestCommandSteps_SendAlarm_MissingAction(t *testing.T) {
	e := NewExecutor()
	RegisterCommandSteps(e, nil)

	sc := model.Scenario{Name: "drill", Steps: []model.Step{
		{Type: "send-command", Params: map[string]any{}},
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
	if report.Steps[0].Error == "" {
		t.Fatalf("error empty")
	}
}

func TestCommandSteps_Integration_TriggerAlarmViaAppService(t *testing.T) {
	// 集成测试：使用真实 nodereg + app.NodeService 触发 Alarm。
	reg := nodereg.New()
	factory := func(addr string, _ model.NodeID) (port.SIPTransport, error) {
		return nil, errors.New("no transport required for alarm-only test")
	}
	lifecycle := nodereg.NewLifecycle(reg, factory)
	svc, _ := app.NewNodeService(reg, lifecycle, reg, factory, nil)

	ctx := context.Background()
	profile, _ := model.NewNodeProfile("34020000001320000001", "127.0.0.1:0", "3402000000", "acme")
	if _, err := svc.Create(ctx, profile); err != nil {
		t.Fatalf("Create = error %v", err)
	}

	ctrl := &struct {
		BehaviorController
	}{BehaviorController: svc}

	e := NewExecutor()
	RegisterCommandSteps(e, ctrl)
	sc := model.Scenario{Name: "drill", Steps: []model.Step{
		{Type: "send-command", Params: map[string]any{
			"node":        "34020000001320000001",
			"action":      "alarm",
			"priority":    0,
			"method":      0,
			"description": "motion",
		}},
	}}
	report, err := e.Run(ctx, sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepPassed {
		t.Fatalf("Total = %q, want passed; steps=%+v", report.Total, report.Steps)
	}
	if report.Steps[0].Status != model.StepPassed {
		t.Fatalf("step0 = %q, want passed", report.Steps[0].Status)
	}
}

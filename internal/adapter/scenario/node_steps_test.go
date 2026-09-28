package scenario

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	nodereg "github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// fakeNodeController records create/start/stop outcomes so the handler
// chain can be asserted end-to-end.
type fakeNodeController struct {
	mu      sync.Mutex
	nodes   map[string]model.Node
	started map[string]bool
}

func newFakeNodeController() *fakeNodeController {
	return &fakeNodeController{
		nodes:   make(map[string]model.Node),
		started: make(map[string]bool),
	}
}

func (f *fakeNodeController) Create(_ context.Context, p model.NodeProfile) (model.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := p.ID().String()
	if _, ok := f.nodes[id]; ok {
		return model.Node{}, errors.New("node already exists")
	}
	f.nodes[id] = model.NewNode(p)
	return f.nodes[id], nil
}

func (f *fakeNodeController) Start(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := id.String()
	if _, ok := f.nodes[key]; !ok {
		return errors.New("unknown node")
	}
	f.started[key] = true
	return nil
}

func (f *fakeNodeController) Stop(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := id.String()
	if _, ok := f.nodes[key]; !ok {
		return errors.New("unknown node")
	}
	f.started[key] = false
	return nil
}

func (f *fakeNodeController) getStarted(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started[id]
}

func TestNodeSteps_Integration_CreateStartStopDevice(t *testing.T) {
	ctrl := newFakeNodeController()
	e := NewExecutor()
	RegisterNodeSteps(e, ctrl, nil)

	sc := model.Scenario{Name: "device-lifecycle", Steps: []model.Step{
		{Type: "create-node", Params: map[string]any{"id": "34020000001320000001", "addr": "127.0.0.1:5061", "domain": "3402000000", "vendor": "acme", "profile": "device"}},
		{Type: "start-node", Params: map[string]any{"id": "34020000001320000001"}},
		{Type: "stop-node", Params: map[string]any{"id": "34020000001320000001"}},
	}}

	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepPassed {
		t.Fatalf("Total = %q, want passed; steps=%+v", report.Total, report.Steps)
	}
	for i, want := range []string{"create-node", "start-node", "stop-node"} {
		if got := report.Steps[i].Type; got != want {
			t.Fatalf("step %d type = %q, want %q", i, got, want)
		}
		if report.Steps[i].Status != model.StepPassed {
			t.Fatalf("step %d (%s) = %q, want passed", i, want, report.Steps[i].Status)
		}
	}
	if started := ctrl.getStarted("34020000001320000001"); started {
		t.Fatalf("node was started at end of run, want stopped")
	}
}

func TestNodeSteps_Integration_MissingID(t *testing.T) {
	ctrl := newFakeNodeController()
	e := NewExecutor()
	RegisterNodeSteps(e, ctrl, nil)

	sc := model.Scenario{Name: "missing", Steps: []model.Step{
		{Type: "start-node", Params: map[string]any{}},
	}}

	report, err := e.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
	if report.Steps[0].Error == "" {
		t.Fatalf("step0 error empty, want missing-id text")
	}
}

func TestNodeSteps_Integration_StartUnknownNode(t *testing.T) {
	ctrl := newFakeNodeController()
	e := NewExecutor()
	RegisterNodeSteps(e, ctrl, nil)

	sc := model.Scenario{Name: "missing", Steps: []model.Step{
		{Type: "start-node", Params: map[string]any{"id": "34020000001320000001"}},
	}}

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
		t.Fatalf("step0 error empty")
	}
}

// TestNodeSteps_Integration_StopViaAppService 使用真实 nodereg +
// app.NodeService：create → start → stop 全链路，再验证重复 stop 失败。
func TestNodeSteps_Integration_StopViaAppService(t *testing.T) {
	reg := nodereg.New()
	factory := func(addr string, _ model.NodeID) (port.SIPTransport, error) {
		tr, err := siptransport.New("udp://" + addr)
		if err != nil {
			return nil, err
		}
		return siptransport.NewPortAdapter(tr), nil
	}
	lifecycle := nodereg.NewLifecycle(reg, factory)
	svc, _ := app.NewNodeService(reg, lifecycle, reg, factory, nil)

	ctx := context.Background()
	profile, _ := model.NewNodeProfile("34020000001320000001", "127.0.0.1:0", "3402000000", "acme")
	if _, err := svc.Create(ctx, profile); err != nil {
		t.Fatalf("Create = error %v", err)
	}
	if err := svc.Start(ctx, mustNodeID(t, "34020000001320000001")); err != nil {
		t.Fatalf("Start = error %v", err)
	}

	e := NewExecutor()
	RegisterNodeSteps(e, svc, nil)

	sc := model.Scenario{Name: "stop", Steps: []model.Step{
		{Type: "stop-node", Params: map[string]any{"id": "34020000001320000001"}},
	}}
	report, err := e.Run(ctx, sc)
	if err != nil {
		t.Fatalf("Run = error %v", err)
	}
	if report.Total != model.StepPassed {
		t.Fatalf("Total = %q, want passed", report.Total)
	}

	// 重复 stop：节点已停止，步骤必须失败并给出错误文本。
	report, err = e.Run(ctx, model.Scenario{Name: "stop-twice", Steps: []model.Step{
		{Type: "stop-node", Params: map[string]any{"id": "34020000001320000001"}},
	}})
	if err != nil {
		t.Fatalf("Run twice = error %v", err)
	}
	if report.Total != model.StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
	if !strings.Contains(report.Steps[0].Error, "stop-node") {
		t.Fatalf("error = %q, want stop-node", report.Steps[0].Error)
	}
}

package app

import (
	"errors"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func TestScenarioService_RunSavesLastRun(t *testing.T) {
	runner := &stubRunner{
		metas:   []model.ScenarioMeta{{Name: "pkg-a"}},
		reports: map[string]model.RunReport{"pkg-a": makeStubReport("pkg-a", model.StepPassed)},
	}
	svc := NewScenarioService(runner)

	r, err := svc.Run("pkg-a")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if r.ScenarioName != "pkg-a" {
		t.Fatalf("unexpected scenario: %s", r.ScenarioName)
	}

	last, ok := svc.LastRun()
	if !ok {
		t.Fatal("expected last run to exist after Run")
	}
	if last.ScenarioName != "pkg-a" {
		t.Fatalf("unexpected last run: %s", last.ScenarioName)
	}
	if last.Total != model.StepPassed {
		t.Fatalf("unexpected last run status: %s", last.Total)
	}
}

func TestScenarioService_LastRun_BeforeAnyRun(t *testing.T) {
	svc := NewScenarioService(&stubRunner{})
	if _, ok := svc.LastRun(); ok {
		t.Fatal("expected no last run before any execution")
	}
}

func TestScenarioService_RunUnknownName_NoLastRun(t *testing.T) {
	runner := &stubRunner{
		metas:   []model.ScenarioMeta{{Name: "pkg-a"}},
		reports: map[string]model.RunReport{"pkg-a": makeStubReport("pkg-a", model.StepPassed)},
		err:     errors.New("unknown scenario"),
	}
	svc := NewScenarioService(runner)

	_, err := svc.Run("missing")
	if err == nil {
		t.Fatal("expected error for unknown name")
	}

	if _, ok := svc.LastRun(); ok {
		t.Fatal("failed run must not be saved as last run")
	}
}

func TestScenarioService_List_Delegates(t *testing.T) {
	runner := &stubRunner{
		metas: []model.ScenarioMeta{{Name: "pkg-a"}, {Name: "pkg-b"}},
	}
	svc := NewScenarioService(runner)

	list := svc.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 metas, got %d", len(list))
	}
	if list[0].Name != "pkg-a" || list[1].Name != "pkg-b" {
		t.Fatalf("unexpected list: %v", list)
	}
}

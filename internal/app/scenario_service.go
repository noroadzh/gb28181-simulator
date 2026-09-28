package app

import (
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// ScenarioService is the application-level façade for the scenario engine.
// It MUST be safe for concurrent use.
type ScenarioService struct {
	mu       sync.RWMutex
	runner   port.ScenarioRunner
	lastRun  *model.RunReport
	executed bool
}

// NewScenarioService builds a ScenarioService backed by the given runner.
func NewScenarioService(runner port.ScenarioRunner) *ScenarioService {
	return &ScenarioService{runner: runner}
}

// List returns the loadable scenario meta sorted by name.
func (s *ScenarioService) List() []model.ScenarioMeta {
	return s.runner.List()
}

// Run synchronously executes the named scenario, saves the report as the
// last run, and returns it.
func (s *ScenarioService) Run(name string) (model.RunReport, error) {
	report, err := s.runner.Run(name)
	if err != nil {
		return model.RunReport{}, err
	}
	s.mu.Lock()
	s.lastRun = &report
	s.executed = true
	s.mu.Unlock()
	return report, nil
}

// LastRun returns the most recent completed run report and whether one exists.
func (s *ScenarioService) LastRun() (model.RunReport, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.executed || s.lastRun == nil {
		return model.RunReport{}, false
	}
	return *s.lastRun, true
}

// stubRunner is a fake port.ScenarioRunner for unit tests.
type stubRunner struct {
	metas   []model.ScenarioMeta
	reports map[string]model.RunReport
	err     error
}

func (r *stubRunner) List() []model.ScenarioMeta { return r.metas }
func (r *stubRunner) Run(name string) (model.RunReport, error) {
	if r.err != nil {
		return model.RunReport{}, r.err
	}
	return r.reports[name], nil
}
func (r *stubRunner) LastRun() (model.RunReport, bool) { return model.RunReport{}, false }

func makeStubReport(name string, status model.StepStatus) model.RunReport {
	now := time.Now()
	return model.RunReport{
		ScenarioName: name,
		Total:        status,
		StartedAt:    now,
		FinishedAt:   now,
		Steps:        []model.StepResult{},
	}
}

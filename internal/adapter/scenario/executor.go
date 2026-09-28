// Package scenario — step executor skeleton.
//
// The executor is a small dispatcher: a runnable scenario is turned into
// a run report by walking steps in declaration order, invoking the
// registered handler for the step type, and honouring a per-step timeout.
// Any failure stops the run; remaining steps are recorded as skipped.
package scenario

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// StepHandler executes one step of a scenario. Implementations must treat
// ctx as carrying a deadline they should respect so the executor can
// enforce the per-step timeout.
type StepHandler interface {
	Execute(ctx context.Context, step model.Step) error
}

// Executor runs scenarios by dispatching to a registered handler per step
// type. It is safe for concurrent use and reusable across runs.
type Executor struct {
	mu       sync.RWMutex
	handlers map[string]StepHandler
}

// NewExecutor creates an empty dispatcher.
func NewExecutor() *Executor {
	return &Executor{handlers: make(map[string]StepHandler)}
}

// Register wires a handler for a step type.
func (e *Executor) Register(t string, h StepHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[t] = h
}

// Run executes the scenario sequentially and returns a run report. An
// unknown step type is treated as a failure so broken scenario packages
// do not silently succeed.
func (e *Executor) Run(ctx context.Context, sc model.Scenario) (model.RunReport, error) {
	now := time.Now()
	report := model.RunReport{ScenarioName: sc.Name, StartedAt: now}

	for i, step := range sc.Steps {
		started := time.Now()
		stepCtx, cancel := context.WithTimeout(ctx, step.EffectiveTimeout())
		stepErr := e.dispatch(stepCtx, step)
		cancel()

		duration := time.Since(started)
		report.Steps = append(report.Steps, model.StepResult{
			Index:      i,
			Type:       step.Type,
			Status:     stepStatus(stepErr),
			DurationMS: duration.Milliseconds(),
			Error:      errorString(stepErr),
		})
		if stepErr != nil {
			for j := i + 1; j < len(sc.Steps); j++ {
				report.Steps = append(report.Steps, model.StepResult{
					Index:  j,
					Type:   sc.Steps[j].Type,
					Status: model.StepSkipped,
				})
			}
			break
		}
	}
	report.Finish(time.Now())
	return report, nil
}

func (e *Executor) dispatch(ctx context.Context, step model.Step) error {
	e.mu.RLock()
	h, ok := e.handlers[step.Type]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("scenario: no handler registered for step type %q", step.Type)
	}
	if err := h.Execute(ctx, step); err != nil {
		return fmt.Errorf("scenario: step %s failed: %w", step.Type, err)
	}
	return nil
}

// handlerFunc adapts a plain function to the StepHandler interface.
type handlerFunc func(ctx context.Context, step model.Step) error

// Execute satisfies StepHandler.
func (f handlerFunc) Execute(ctx context.Context, step model.Step) error {
	return f(ctx, step)
}

func stepStatus(err error) model.StepStatus {
	if err == nil {
		return model.StepPassed
	}
	return model.StepFailed
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

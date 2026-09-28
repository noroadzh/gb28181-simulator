// Package model — YAML scenario domain model.
//
// A Scenario is a declarative, ordered list of Steps executed by the
// scenario engine. The model carries no protocol behavior: step handlers
// in the adapter layer interpret Params against existing domain services.
package model

import (
	"time"
)

// DefaultStepTimeout applies when a step declares no explicit timeout.
const DefaultStepTimeout = 30 * time.Second

// StepStatus enumerates step-level outcomes inside a run report.
type StepStatus string

const (
	// StepPassed marks a step that finished successfully.
	StepPassed StepStatus = "passed"
	// StepFailed marks a step that errored, asserted false or timed out.
	StepFailed StepStatus = "failed"
	// StepSkipped marks a step never executed because an earlier step failed.
	StepSkipped StepStatus = "skipped"
)

// Step is one declarative action inside a scenario.
type Step struct {
	// Type selects the step handler, e.g. "create-node", "expect".
	Type string
	// Timeout bounds a single step execution; zero means DefaultStepTimeout.
	Timeout time.Duration
	// Description is an optional human-readable hint.
	Description string
	// Params carries the handler-specific arguments.
	Params map[string]any
}

// EffectiveTimeout returns the declared timeout or DefaultStepTimeout.
func (s Step) EffectiveTimeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return DefaultStepTimeout
}

// Scenario is a named, ordered composition of steps.
type Scenario struct {
	Name        string
	Description string
	Steps       []Step
}

// ScenarioMeta is the listable projection of a Scenario.
type ScenarioMeta struct {
	Name        string
	Description string
}

// StepResult records the outcome of one executed step.
type StepResult struct {
	Index      int        `json:"index"`
	Type       string     `json:"type"`
	Status     StepStatus `json:"status"`
	DurationMS int64      `json:"duration_ms"`
	Error      string     `json:"error,omitempty"`
}

// RunReport is the structured outcome of one scenario run.
type RunReport struct {
	ScenarioName string       `json:"scenario_name"`
	StartedAt    time.Time    `json:"started_at"`
	FinishedAt   time.Time    `json:"finished_at"`
	Total        StepStatus   `json:"total"`
	Steps        []StepResult `json:"steps"`
}

// DeriveTotal computes the overall run status: any failed step fails the
// run; otherwise the run passed.
func DeriveTotal(steps []StepResult) StepStatus {
	for _, s := range steps {
		if s.Status == StepFailed {
			return StepFailed
		}
	}
	return StepPassed
}

// Finish stamps the finish time and derives the total status in place.
func (r *RunReport) Finish(finishedAt time.Time) {
	r.FinishedAt = finishedAt
	r.Total = DeriveTotal(r.Steps)
}

// Meta projects the scenario to its listable form.
func (s Scenario) Meta() ScenarioMeta {
	return ScenarioMeta{Name: s.Name, Description: s.Description}
}

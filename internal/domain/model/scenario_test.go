package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStep_EffectiveTimeout(t *testing.T) {
	cases := []struct {
		name     string
		step     Step
		expected time.Duration
	}{
		{"default", Step{}, DefaultStepTimeout},
		{"explicit", Step{Timeout: 5 * time.Second}, 5 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.step.EffectiveTimeout(); got != tc.expected {
				t.Fatalf("EffectiveTimeout() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestDeriveTotal(t *testing.T) {
	cases := []struct {
		name     string
		steps    []StepResult
		expected StepStatus
	}{
		{"all passed", []StepResult{
			{Status: StepPassed}, {Status: StepPassed},
		}, StepPassed},
		{"mixed with failed", []StepResult{
			{Status: StepPassed}, {Status: StepFailed}, {Status: StepSkipped},
		}, StepFailed},
		{"only skipped", []StepResult{
			{Status: StepSkipped},
		}, StepPassed},
		{"empty", nil, StepPassed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveTotal(tc.steps); got != tc.expected {
				t.Fatalf("DeriveTotal() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestRunReport_JSON_Serialization(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	report := RunReport{
		ScenarioName: "register-keepalive-catalog",
		StartedAt:    now,
		FinishedAt:   now.Add(5 * time.Second),
		Total:        StepFailed,
		Steps: []StepResult{
			{Index: 0, Type: "create-node", Status: StepPassed, DurationMS: 200, Error: ""},
			{Index: 1, Type: "expect", Status: StepFailed, DurationMS: 50, Error: "node never came online"},
		},
	}

	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal = error %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal = error %v", err)
	}

	if decoded["scenario_name"] != "register-keepalive-catalog" {
		t.Fatalf("scenario_name = %v, want register-keepalive-catalog", decoded["scenario_name"])
	}
	if decoded["total"] != "failed" {
		t.Fatalf("total = %v, want failed", decoded["total"])
	}
	if _, ok := decoded["error"]; ok {
		t.Fatalf("unexpected error field on passed step")
	}

	steps := decoded["steps"].([]any)
	step0 := steps[0].(map[string]any)
	if step0["error"] != nil {
		t.Fatalf("passed step should omit empty error")
	}
	step1 := steps[1].(map[string]any)
	if step1["error"] != "node never came online" {
		t.Fatalf("step1 error = %v, want 'node never came online'", step1["error"])
	}
}

func TestReport_Finish(t *testing.T) {
	started := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	finished := started.Add(3 * time.Second)
	report := RunReport{StartedAt: started}
	report.Finish(finished)

	if !report.FinishedAt.Equal(finished) {
		t.Fatalf("FinishedAt = %v, want %v", report.FinishedAt, finished)
	}
	if report.Total != StepPassed {
		t.Fatalf("Total = %q, want passed for empty steps", report.Total)
	}

	report.Steps = []StepResult{{Status: StepFailed}}
	report.Finish(finished)
	if report.Total != StepFailed {
		t.Fatalf("Total = %q, want failed", report.Total)
	}
}

func TestScenario_Meta(t *testing.T) {
	s := Scenario{Name: "alarm-capture", Description: "drill"}
	m := s.Meta()
	if m.Name != "alarm-capture" || m.Description != "drill" {
		t.Fatalf("Meta() = %+v, want name=alarm-capture description=drill", m)
	}
}

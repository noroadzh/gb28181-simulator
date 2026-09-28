// Package scenario — YAML scenario-package loader.
//
// The loader is the only place that touches YAML: it parses package files,
// validates their structure and converts them to domain Scenarios. All
// errors carry the source filename and the offending field so a broken
// package can be fixed without reading the engine internals.
package scenario

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// KnownStepTypes is the set of step handlers the engine understands.
var KnownStepTypes = map[string]bool{
	"create-node":  true,
	"start-node":   true,
	"stop-node":    true,
	"wait":         true,
	"send-command": true,
	"expect":       true,
	"inject-fault": true,
}

// scenarioFile mirrors the YAML package shape.
type scenarioFile struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Steps       []stepFile `yaml:"steps"`
}

// stepFile mirrors one step entry in the YAML package.
type stepFile struct {
	Type        string         `yaml:"type"`
	Timeout     int            `yaml:"timeout"` // seconds
	Description string         `yaml:"description"`
	Params      map[string]any `yaml:"params"`
}

// LoadScenarioFile loads and validates a YAML scenario package from disk.
func LoadScenarioFile(path string) (model.Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Scenario{}, fmt.Errorf("scenario: %s: %w", path, err)
	}
	return LoadScenarioBytes(path, data)
}

// LoadScenarioBytes parses and validates YAML scenario package content.
// The filename argument is used only for error messages.
func LoadScenarioBytes(filename string, data []byte) (model.Scenario, error) {
	var f scenarioFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return model.Scenario{}, fmt.Errorf("scenario: %s: invalid yaml: %w", filename, err)
	}
	if strings.TrimSpace(f.Name) == "" {
		return model.Scenario{}, fmt.Errorf("scenario: %s: name: field is required", filename)
	}
	if len(f.Steps) == 0 {
		return model.Scenario{}, fmt.Errorf("scenario: %s: steps: at least one step is required", filename)
	}

	steps := make([]model.Step, 0, len(f.Steps))
	for i, sf := range f.Steps {
		field := fmt.Sprintf("steps[%d]", i)
		stepType := strings.TrimSpace(sf.Type)
		if stepType == "" {
			return model.Scenario{}, fmt.Errorf("scenario: %s: %s.type: field is required", filename, field)
		}
		if !KnownStepTypes[stepType] {
			return model.Scenario{}, fmt.Errorf("scenario: %s: %s.type: unknown step type %q", filename, field, stepType)
		}
		steps = append(steps, model.Step{
			Type:        stepType,
			Timeout:     time.Duration(sf.Timeout) * time.Second,
			Description: sf.Description,
			Params:      sf.Params,
		})
	}
	return model.Scenario{Name: f.Name, Description: f.Description, Steps: steps}, nil
}

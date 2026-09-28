// Package port — ScenarioRunner: application-level runtime for scenarios.
package port

import (
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ScenarioRunner lists and executes YAML scenario packages against the
// running simulator. Implementations MUST be safe for concurrent use so
// the HTTP API and any future scheduler can share one runner.
type ScenarioRunner interface {
	// List returns the loadable scenario meta sorted by name.
	List() []model.ScenarioMeta
	// Run synchronously executes the named scenario and returns the run
	// report. An unknown name yields an error.
	Run(name string) (model.RunReport, error)
	// LastRun returns the most recent completed run report and whether
	// one exists.
	LastRun() (model.RunReport, bool)
}

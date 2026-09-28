// Package scenario — adapter runner: store + executor bound as one port.ScenarioRunner.
package scenario

import (
	"context"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Runner adapts Store + Executor into a port.ScenarioRunner.
// LastRun is stateless at this layer; the app-level ScenarioService decorator
// records the last completed run.
type Runner struct {
	ctx   context.Context
	store *Store
	exec  *Executor
}

// Compile-time check: Runner satisfies the domain port.
var _ port.ScenarioRunner = (*Runner)(nil)

// NewRunner creates a stateless base runner.
func NewRunner(ctx context.Context, store *Store, exec *Executor) *Runner {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Runner{ctx: ctx, store: store, exec: exec}
}

// List delegates to the store catalogue.
func (r *Runner) List() []model.ScenarioMeta {
	return r.store.List()
}

// Run looks up the named package and executes it sequentially.
func (r *Runner) Run(name string) (model.RunReport, error) {
	sc, ok := r.store.Get(name)
	if !ok {
		return model.RunReport{}, fmt.Errorf("scenario: %q not found", name)
	}
	return r.exec.Run(r.ctx, sc)
}

// LastRun is always empty at the adapter level: the app-level ScenarioService
// decorator tracks the most recent completed run.
func (r *Runner) LastRun() (model.RunReport, bool) {
	return model.RunReport{}, false
}

// Package scenario — wait step: bounded sleep.
package scenario

import (
	"context"
	"fmt"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// RegisterWaitSteps wires the wait handler. The step sleeps params.seconds,
// honouring the step's effective timeout: when the sleep would exceed the
// timeout the executor's context is cancelled and the step fails with a
// timeout error rather than running past its allowed window.
func RegisterWaitSteps(e *Executor) {
	e.Register("wait", handlerFunc(waitStep))
}

func waitStep(ctx context.Context, step model.Step) error {
	seconds := intParam(step.Params, "seconds", 0)
	if seconds <= 0 {
		return fmt.Errorf("wait: params.seconds must be a positive integer")
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait: %w", ctx.Err())
	}
}

// intParam reads an integer parameter; YAML numbers may decode as float64.
func intParam(params map[string]any, key string, fallback int) int {
	switch v := params[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	default:
		return fallback
	}
}

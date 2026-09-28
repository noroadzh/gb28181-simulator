// Package scenario — inject-fault step: arms runtime fault profiles.
package scenario

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// FaultController is the narrow slice of the application fault store the
// inject-fault step drives. *app.FaultStoreAdapter satisfies it.
type FaultController interface {
	Install(ctx context.Context, nodeID model.NodeID, profile model.FaultProfile) error
	Clear(ctx context.Context, nodeID model.NodeID) error
}

// RegisterInjectFaultSteps wires the inject-fault handler.
func RegisterInjectFaultSteps(e *Executor, ctrl FaultController) {
	c := &faultSteps{ctrl: ctrl}
	e.Register("inject-fault", handlerFunc(c.inject))
}

type faultSteps struct {
	ctrl FaultController
}

func (f *faultSteps) inject(ctx context.Context, step model.Step) error {
	id, err := nodeIDParam(step.Params)
	if err != nil {
		return err
	}
	profile, err := faultProfileParams(step.Params)
	if err != nil {
		return err
	}
	if err := f.ctrl.Install(ctx, id, profile); err != nil {
		return err
	}
	return nil
}

func faultProfileParams(params map[string]any) (model.FaultProfile, error) {
	var p model.FaultProfile
	if v, ok := params["canned"].(map[string]any); ok {
		p.Canned = make(map[string]int, len(v))
		for method, status := range v {
			if s, ok := status.(float64); ok {
				p.Canned[method] = int(s)
			}
		}
	}
	if v, ok := params["drop"].(float64); ok {
		p.Drop = v
	}
	if v, ok := params["blackhole"].([]any); ok {
		p.Blackhole = make([]string, len(v))
		for i, m := range v {
			if s, ok := m.(string); ok {
				p.Blackhole[i] = s
			}
		}
	}
	if v, ok := params["unsupported_method"].(float64); ok {
		p.UnsupportedMethod = int(v)
	}
	if err := p.Validate(); err != nil {
		return model.FaultProfile{}, err
	}
	return p, nil
}

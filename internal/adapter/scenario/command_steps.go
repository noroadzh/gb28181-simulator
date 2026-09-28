// Package scenario — send-command step: triggers runtime behavior APIs.
package scenario

import (
	"context"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// BehaviorController is the slice of the application service the
// send-command step drives. *app.NodeService satisfies it.
type BehaviorController interface {
	TriggerAlarm(ctx context.Context, id model.NodeID, in app.AlarmInput) (model.AlarmSnapshot, error)
}

// RegisterCommandSteps wires the send-command handler.
func RegisterCommandSteps(e *Executor, ctrl BehaviorController) {
	c := &commandSteps{ctrl: ctrl}
	e.Register("send-command", handlerFunc(c.send))
}

type commandSteps struct {
	ctrl BehaviorController
}

func (c *commandSteps) send(ctx context.Context, step model.Step) error {
	id, err := nodeIDParam(step.Params)
	if err != nil {
		return fmt.Errorf("send-command: %w", err)
	}
	action := stringParam(step.Params, "action", "")
	switch action {
	case "alarm":
		in := app.AlarmInput{
			Priority:    intParam(step.Params, "priority", 0),
			Method:      intParam(step.Params, "method", 0),
			Description: stringParam(step.Params, "description", ""),
			ChannelID:   stringParam(step.Params, "channel_id", ""),
		}
		if _, err := c.ctrl.TriggerAlarm(ctx, id, in); err != nil {
			return fmt.Errorf("send-command alarm: %w", err)
		}
		return nil
	case "":
		return fmt.Errorf("send-command: params.action is required")
	default:
		return fmt.Errorf("send-command: unsupported action %q", action)
	}
}

// nodeIDParam reads the target node from params: send-command uses "node"
// per the scenario package schema, falling back to "id" for tolerance.
func nodeIDParam(params map[string]any) (model.NodeID, error) {
	raw := stringParam(params, "node", "")
	if raw == "" {
		raw = stringParam(params, "id", "")
	}
	if raw == "" {
		return model.NodeID{}, fmt.Errorf("params.node is required")
	}
	return model.ParseNodeID(raw)
}

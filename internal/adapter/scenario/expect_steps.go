// Package scenario — expect step: asserts runtime state such as a node's
// lifecycle status.
package scenario

import (
	"context"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ExpectQuery is the slice of the node registry the expect step reads.
// *nodereg.Registry satisfies it.
type ExpectQuery interface {
	Get(ctx context.Context, id model.NodeID) (model.Node, bool)
}

// RegisterExpectSteps wires the expect handler.
func RegisterExpectSteps(e *Executor, q ExpectQuery) {
	e.Register("expect", handlerFunc(expectStep(q)))
}

func expectStep(q ExpectQuery) handlerFunc {
	return func(ctx context.Context, step model.Step) error {
		target := stringParam(step.Params, "target", "")
		op := stringParam(step.Params, "op", "")
		want := stringParam(step.Params, "value", "")
		if target == "" {
			return fmt.Errorf("expect: params.target is required")
		}
		if op == "" {
			return fmt.Errorf("expect: params.op is required")
		}

		value, err := resolveTarget(ctx, q, target)
		if err != nil {
			return fmt.Errorf("expect: %w", err)
		}

		switch op {
		case "eq":
			if value != want {
				return fmt.Errorf("expect: %s = %q, want %q", target, value, want)
			}
		case "contains":
			if !strings.Contains(value, want) {
				return fmt.Errorf("expect: %s = %q, want it to contain %q", target, value, want)
			}
		default:
			return fmt.Errorf("expect: unsupported op %q (want eq or contains)", op)
		}
		return nil
	}
}

// resolveTarget supports node.<id>.status this change; other targets
// (for example run-scoped outputs) are rejected so they cannot pass silently.
func resolveTarget(ctx context.Context, q ExpectQuery, target string) (string, error) {
	parts := strings.SplitN(target, ".", 3)
	if len(parts) != 3 || parts[0] != "node" || parts[2] != "status" {
		return "", fmt.Errorf("unsupported target %q (want node.<id>.status)", target)
	}
	id, err := model.ParseNodeID(parts[1])
	if err != nil {
		return "", err
	}
	n, ok := q.Get(ctx, id)
	if !ok {
		return "", fmt.Errorf("node %s not found", id)
	}
	return n.Status().String(), nil
}

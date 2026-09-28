// Package scenario — node lifecycle steps (create/start/stop).
package scenario

import (
	"context"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// NodeController is the slice of node management the node steps need. The
// composition root injects the real application service; tests may supply
// a controlled fake.
type NodeController interface {
	Create(ctx context.Context, profile model.NodeProfile) (model.Node, error)
	Start(ctx context.Context, id model.NodeID) error
	Stop(ctx context.Context, id model.NodeID) error
}

// AccountStore is the slice of the credential store a create-node step
// needs when the created node is a platform that accepts registrations.
// *credstore.Store satisfies it.
type AccountStore interface {
	Add(nodeID model.NodeID, cred model.Credentials) error
}

// nodeSteps adapts NodeController calls to StepHandler functions.
type nodeSteps struct {
	ctrl     NodeController
	accounts AccountStore
}

// RegisterNodeSteps wires the create-node / start-node / stop-node handlers
// into the executor. accounts may be nil — a scenario without platform
// accounts never touches it.
func RegisterNodeSteps(e *Executor, ctrl NodeController, accounts AccountStore) {
	n := &nodeSteps{ctrl: ctrl, accounts: accounts}
	e.Register("create-node", handlerFunc(n.create))
	e.Register("start-node", handlerFunc(n.start))
	e.Register("stop-node", handlerFunc(n.stop))
}

func (n *nodeSteps) create(ctx context.Context, step model.Step) error {
	id, ok := step.Params["id"].(string)
	if id == "" || !ok {
		return fmt.Errorf("create-node: params.id is required")
	}
	addr, ok := step.Params["addr"].(string)
	if addr == "" || !ok {
		return fmt.Errorf("create-node: params.addr is required")
	}
	domain := stringParam(step.Params, "domain", "")
	vendor := stringParam(step.Params, "vendor", "")

	profile, err := model.NewNodeProfile(id, addr, domain, vendor)
	if err != nil {
		return fmt.Errorf("create-node: %w", err)
	}
	if declared := stringParam(step.Params, "profile", ""); declared != "" {
		kind, err := model.ParseNodeKind(declared)
		if err != nil {
			return fmt.Errorf("create-node: %w", err)
		}
		if got, want := profile.Kind(), kind; got != want {
			return fmt.Errorf("create-node: id %s implies kind %s but profile declares %s", id, got, want)
		}
	}
	if reg, err := registrationParams(id, domain, step.Params); err != nil {
		return fmt.Errorf("create-node: %w", err)
	} else if reg != nil {
		profile, err = profile.WithRegistration(*reg)
		if err != nil {
			return fmt.Errorf("create-node: %w", err)
		}
	}
	profile, err = n.platformParams(profile, step.Params)
	if err != nil {
		return fmt.Errorf("create-node: %w", err)
	}
	if _, err := n.ctrl.Create(ctx, profile); err != nil {
		return fmt.Errorf("create-node: %w", err)
	}
	return nil
}

// registrationParams reads the optional `registration:` sub-map. A missing
// or empty section yields (nil, nil) — the node simply does not register.
func registrationParams(id, domain string, params map[string]any) (*model.Registration, error) {
	raw, ok := params["registration"].(map[string]any)
	if !ok {
		return nil, nil
	}
	server := stringParam(raw, "server", "")
	if server == "" {
		return nil, fmt.Errorf("registration.server is required")
	}
	password := stringParam(raw, "password", "")
	if password == "" {
		return nil, fmt.Errorf("registration.password is required for node %s", id)
	}
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:    server,
		ServerID:  stringParam(raw, "server-id", ""),
		Username:  stringParam(raw, "username", ""),
		Password:  password,
		GBVersion: stringParam(raw, "gb_version", ""),
		Expires:   uintParam(raw, "expires"),
	})
	if err != nil {
		return nil, err
	}
	return &reg, nil
}

// platformParams reads the optional `platform:` sub-map: the challenge realm
// and the accounts the created platform accepts. Accounts are added to the
// shared credential store under the node's own id; a scenario without the
// accounts store (tests) may still declare a realm. The returned profile
// carries the serving description — profiles are values, so the caller must
// use it.
func (n *nodeSteps) platformParams(profile model.NodeProfile, params map[string]any) (model.NodeProfile, error) {
	raw, ok := params["platform"].(map[string]any)
	if !ok {
		return profile, nil
	}
	realm := stringParam(raw, "realm", profile.Domain())
	serving, err := model.NewPlatformServing(realm, model.ExpiresPolicy{})
	if err != nil {
		return profile, err
	}
	profile, err = profile.WithPlatformServing(serving)
	if err != nil {
		return profile, err
	}
	accounts, ok := raw["accounts"].([]any)
	if !ok {
		return profile, nil
	}
	if n.accounts == nil {
		return profile, fmt.Errorf("platform.accounts declared but no account store is wired")
	}
	for i, a := range accounts {
		acc, ok := a.(map[string]any)
		if !ok {
			return profile, fmt.Errorf("platform.accounts[%d]: not a mapping", i)
		}
		username := stringParam(acc, "username", "")
		password := stringParam(acc, "password", "")
		if username == "" || password == "" {
			return profile, fmt.Errorf("platform.accounts[%d]: username and password are required", i)
		}
		cred, err := model.NewCredentials(username, realm, password)
		if err != nil {
			return profile, fmt.Errorf("platform.accounts[%d]: %w", i, err)
		}
		if err := n.accounts.Add(profile.ID(), cred); err != nil {
			return profile, fmt.Errorf("platform.accounts[%d]: %w", i, err)
		}
	}
	return profile, nil
}

func (n *nodeSteps) start(ctx context.Context, step model.Step) error {
	id, err := idFromParams(step.Params)
	if err != nil {
		return fmt.Errorf("start-node: %w", err)
	}
	if err := n.ctrl.Start(ctx, id); err != nil {
		return fmt.Errorf("start-node: %w", err)
	}
	return nil
}

func (n *nodeSteps) stop(ctx context.Context, step model.Step) error {
	id, err := idFromParams(step.Params)
	if err != nil {
		return fmt.Errorf("stop-node: %w", err)
	}
	if err := n.ctrl.Stop(ctx, id); err != nil {
		return fmt.Errorf("stop-node: %w", err)
	}
	return nil
}

func idFromParams(params map[string]any) (model.NodeID, error) {
	raw, ok := params["id"].(string)
	if raw == "" || !ok {
		return model.NodeID{}, fmt.Errorf("params.id is required")
	}
	return model.ParseNodeID(raw)
}

func stringParam(params map[string]any, key, fallback string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// uintParam reads an unsigned integer written as a YAML scalar. YAML
// decodes numbers as float64, so the conversion goes through it; a missing
// or non-numeric value yields 0 (the model default).
func uintParam(params map[string]any, key string) uint32 {
	if v, ok := params[key].(float64); ok && v >= 0 {
		return uint32(v)
	}
	return 0
}

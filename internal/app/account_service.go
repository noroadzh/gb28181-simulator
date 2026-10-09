// Package app — AccountService.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// AccountService orchestrates the account management use cases on top of a
// platform node's durable account store.
//
// It depends only on ports: the AccountAdmin (the sqlite-backed adapter the
// composition root injects) and the NodeRegistry (to tell a platform node
// from a device one). It never imports an adapter package (design D6).
type AccountService struct {
	admin    port.AccountAdmin
	registry port.NodeRegistry
	log      *slog.Logger
}

// NewAccountService builds the service over the injected ports. admin and
// registry are required; logger defaults to slog.Default().
func NewAccountService(admin port.AccountAdmin, registry port.NodeRegistry, log *slog.Logger) (*AccountService, error) {
	if admin == nil {
		return nil, fmt.Errorf("app: AccountService requires an AccountAdmin")
	}
	if registry == nil {
		return nil, fmt.Errorf("app: AccountService requires a NodeRegistry")
	}
	if log == nil {
		log = slog.Default()
	}
	return &AccountService{admin: admin, registry: registry, log: log}, nil
}

// Admin exposes the underlying AccountAdmin to the HTTP management layer.
// The service adds no behaviour around the CRUD calls yet — the HTTP layer
// calls the port directly — so the accessor is the seam for later policy
// (audit, rate limits) without changing the wiring.
func (s *AccountService) Admin() port.AccountAdmin { return s.admin }

// SeedFromConfig idempotently seeds the platform node's accounts from the
// YAML configuration. An account that already exists in the store (seeded on
// an earlier run, or mutated at runtime through AccountAdmin) is left
// untouched, so runtime changes survive restarts and YAML wins only on the
// first run.
//
// Only platform nodes accept accounts: seeding a device is a caller bug and
// reports an error. Passwords never reach the log.
func (s *AccountService) SeedFromConfig(ctx context.Context, nodeID model.NodeID, accounts []model.Credentials) error {
	node, ok := s.registry.Get(ctx, nodeID)
	if !ok {
		return fmt.Errorf("app: seed accounts for node %s: %w", nodeID, model.ErrUnknownNode)
	}
	switch node.ID().Kind() {
	case model.NodeKindPlatformLarge, model.NodeKindPlatformSmall:
	default:
		return fmt.Errorf("app: seed accounts for node %s: not a platform node (kind %s)",
			nodeID, node.ID().Kind())
	}
	for _, cred := range accounts {
		username := cred.Username()
		if err := s.admin.AddAccount(nodeID, username, cred.Password()); err != nil {
			if errors.Is(err, port.ErrAccountExists) {
				// Already there from an earlier run or a runtime
				// change: the store wins, YAML is the seed only.
				continue
			}
			return fmt.Errorf("app: seed account %q for node %s: %w", username, nodeID, err)
		}
	}
	s.log.Info("platform accounts seeded",
		"node_id", nodeID.String(), "accounts", len(accounts))
	return nil
}

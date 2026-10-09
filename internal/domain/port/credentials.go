// Package port — CredentialStore.
package port

import (
	"errors"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Sentinel errors the HTTP management layer maps to status codes. They are
// deliberately about the username, never the password: no error body should
// ever carry a secret.
var (
	// ErrAccountExists means the (node, username) pair is already
	// registered; the HTTP layer answers 409 Conflict.
	ErrAccountExists = errors.New("account already exists")
	// ErrAccountNotFound means the (node, username) pair is absent; the
	// HTTP layer answers 404 Not Found.
	ErrAccountNotFound = errors.New("account not found")
)

// CredentialStore answers the only question a platform needs of its
// accounts: which secret goes with this username, on this node?
//
// The store is partitioned by node so two platforms can accept different
// accounts for the same device id, and it never exposes a listing — a store
// that could be enumerated would turn "which accounts exist" into an oracle.
//
// Implementations MUST NOT log or return passwords in errors.
type CredentialStore interface {
	// Lookup returns the credentials configured for username under
	// nodeID. The second result is false when the platform has no such
	// account, which the caller answers with 403 rather than a fresh
	// challenge.
	Lookup(nodeID model.NodeID, username string) (model.Credentials, bool)
}

// AccountInfo is the non-sensitive view of one account, returned by List.
// It MUST NOT carry the password.
type AccountInfo struct {
	Username  string
	CreatedAt string
}

// AccountAdmin is the management interface for a platform node's downstream
// accounts. Unlike CredentialStore (which only answers Lookup for the
// acceptor), AccountAdmin is used by the HTTP management layer to list,
// add, remove, and reset passwords at runtime.
//
// Implementations MUST persist changes so they survive restart, and MUST NOT
// log or return passwords.
type AccountAdmin interface {
	ListAccounts(nodeID model.NodeID) ([]AccountInfo, error)
	AddAccount(nodeID model.NodeID, username, password string) error
	RemoveAccount(nodeID model.NodeID, username string) error
	SetAccountPassword(nodeID model.NodeID, username, password string) error
}

// AccountSeeder is the startup path for YAML-configured accounts. It
// installs a credential (realm + no-auth flag included) idempotently so
// the runtime Lookup can reconstitute exactly what the operator declared.
type AccountSeeder interface {
	Seed(nodeID model.NodeID, cred model.Credentials) error
}

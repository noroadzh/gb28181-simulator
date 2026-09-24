// Package port — CredentialStore.
package port

import (
	"github.com/your-org/gb28181-simulator/internal/domain/model"
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

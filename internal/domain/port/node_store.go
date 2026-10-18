// Package port: NodeConfigurationStore is the persistence contract for
// runtime node configuration. It backs the HTTP CRUD exposed to operators
// and the boot path that hydrates NodeService from the database instead of
// from the legacy YAML nodes section.
//
// The shape of the data the store keeps is whatever the domain models
// already speak — model.StoredNode wrapping model.Registration and
// model.PlatformServing — so the store does not invent parallel DTOs that
// would have to be kept in sync with the models.
package port

import (
	"context"
	"errors"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ErrNodeNotFound is returned by the read, update and delete paths when no
// row carries the requested id. The HTTP layer maps it to 404.
var ErrNodeNotFound = errors.New("port: node not found")

// ErrNodeAlreadyExists is returned by Create when the id is already taken.
// The HTTP layer maps it to 409.
var ErrNodeAlreadyExists = errors.New("port: node already exists")

// ErrAddrClaimed is returned by the registry when a node's listen address
// is already held by another node. The HTTP layer maps it to 409 — the
// request is fine but conflicts with live state.
var ErrAddrClaimed = errors.New("port: node address claimed")

// NodeConfigurationStore is the persistence contract for runtime node
// configuration.
//
// All methods accept a context for cancellation. Implementations must
// return errors wrapping ErrNodeNotFound or ErrNodeAlreadyExists where the
// doc comment below says so, so the HTTP layer can pick a status code
// without sniffing an error string.
type NodeConfigurationStore interface {
	// ListNodes returns every node definition, ordered by id for a
	// stable output. Nodes without a registration or platform row are
	// still returned, with the matching Has* flag left false.
	//
	// The returned slice is owned by the caller; the store will not
	// retain or mutate it.
	ListNodes(ctx context.Context) ([]model.StoredNode, error)

	// GetNode returns one node, or ErrNodeNotFound when the id is absent.
	GetNode(ctx context.Context, id model.NodeID) (model.StoredNode, error)

	// CreateNode writes a new node plus its optional registration and
	// platform rows in one transaction. It returns ErrNodeAlreadyExists
	// when the id is taken; the caller should not retry with a fresh id
	// on the caller's behalf.
	CreateNode(ctx context.Context, in model.StoredNode) error

	// UpdateNode overwrites the node row and replaces its optional child
	// rows, so a node that drops a registration does not keep a stale
	// one. It returns ErrNodeNotFound when the id is absent.
	UpdateNode(ctx context.Context, id model.NodeID, in model.StoredNode) error

	// DeleteNode removes the node and, through the schema's
	// ON DELETE CASCADE, its registration and platform rows. It returns
	// ErrNodeNotFound when the id is absent; callers are expected to
	// treat that as a no-op for the user-visible result.
	DeleteNode(ctx context.Context, id model.NodeID) error

	// UpdateCascade rewrites only the cascade parent and children, so a
	// caller editing only the routing does not have to resend the whole
	// node row. Every other persisted field is left untouched. It
	// returns ErrNodeNotFound when the id is absent.
	//
	// Cycle detection is not the store's job: it belongs to the
	// orchestration layer, which owns the in-memory graph.
	UpdateCascade(ctx context.Context, id model.NodeID, parent string, children []string) error

	// UpsertRegistration writes the registration row for nodeID,
	// replacing any existing one. A missing parent node row is a foreign
	// key error, not a silently created orphan.
	UpsertRegistration(ctx context.Context, nodeID model.NodeID, reg model.Registration) error

	// UpsertPlatform writes the platform-serving row for nodeID,
	// replacing any existing one. pollMs is the catalog query interval
	// in milliseconds.
	UpsertPlatform(ctx context.Context, nodeID model.NodeID, plat model.PlatformServing, pollMs int) error
}

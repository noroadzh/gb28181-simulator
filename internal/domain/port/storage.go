// Package port defines the interfaces (ports) that the domain layer expects
// from external dependencies. These interfaces are implemented by adapters
// in the adapter layer.
package port

import (
	"context"
	"io"
)

// Storage defines the persistence interface for domain entities.
// Implementations must implement io.Closer for proper lifecycle management.
type Storage interface {
	// CRUD performs create, read, update, or delete operations on the given entity.
	// The entity type and operation are determined by the concrete implementation.
	CRUD(ctx context.Context, entity interface{}) error

	// List returns entities matching the query criteria.
	// The query type and return value are determined by the concrete implementation.
	List(ctx context.Context, query interface{}) (interface{}, error)

	// Close releases any resources held by the storage implementation.
	io.Closer
}
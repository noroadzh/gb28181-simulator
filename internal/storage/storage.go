// Package storage wires the SQLite database used by the simulator. Change 1
// keeps this package intentionally small: it opens the file, sets pragmas,
// runs the bootstrap SQL and exposes the *sql.DB. Business schemas are
// added in Change 5+.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"

	_ "modernc.org/sqlite"
)

var _ port.Storage = (*Store)(nil)

//go:embed schema.sql
var schemaSQL string

// Bootstrap opens the SQLite database at the supplied path, creating the file
// and parent directories as needed. Returns an *sql.DB ready for use.
// The call is idempotent: schema.sql uses IF NOT EXISTS and the meta row is
// inserted with OR IGNORE.
func Bootstrap(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("storage: empty path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("storage: mkdir %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: open: %w", err)
	}
	// SQLite tolerates only one writer per file; we are a single process so
	// cap at 1 to keep semantics explicit.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("storage: %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage: exec schema: %w", err)
	}
	return db, nil
}

// DefaultDBPath resolves a Config's storage.path, falling back to the
// per-user default when storage.path is unset.
//
// Note: there is a function with the same name in platform/config that
// ignores cfg.Storage.Path entirely. This wrapper preserves the
// "explicit path wins" semantics historically used by storage callers.
func DefaultDBPath(cfg platformconfig.Config) string {
	if cfg.Storage.Path != "" {
		return cfg.Storage.Path
	}
	return platformconfig.DefaultDBPath(cfg)
}

// EnsureClosed closes the database. Safe to call with a nil receiver.
func EnsureClosed(db *sql.DB) error {
	if db == nil {
		return nil
	}
	return db.Close()
}

// Store wraps *sql.DB and implements the domain port.Storage interface.
// It provides a concrete adapter for persistence operations.
type Store struct {
	db *sql.DB
}

// NewStore opens a SQLite database at the supplied path and returns a Store.
func NewStore(path string) (*Store, error) {
	db, err := Bootstrap(path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// CRUD performs create, read, update, or delete operations.
// Currently a no-op placeholder; domain entities use their own repositories.
func (s *Store) CRUD(ctx context.Context, entity interface{}) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("storage: store not initialised")
	}
	return nil
}

// List returns entities matching the query.
// Currently a no-op placeholder; domain entities use their own repositories.
func (s *Store) List(ctx context.Context, query interface{}) (interface{}, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("storage: store not initialised")
	}
	return nil, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB returns the underlying *sql.DB for direct use by repositories.
func (s *Store) DB() *sql.DB {
	return s.db
}

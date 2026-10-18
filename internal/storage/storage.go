// Package storage wires the SQLite database used by the simulator. Change 1
// keeps this package intentionally small: it opens the file, sets pragmas,
// runs the bootstrap SQL and exposes the *sql.DB. Business schemas are
// added in Change 5+.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"

	_ "modernc.org/sqlite"
)

var _ port.Storage = (*Store)(nil)

//go:embed schema.sql
var schemaSQL string

// migrationsFS holds the numbered migration files applied after schema.sql.
// Each file is named NNN_description.sql; they run in lexicographic order,
// which is numeric order for the zero-padded prefix the convention requires.
// A file that has already been recorded in schema_meta is skipped, so adding
// one is the whole of "change the schema".
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migration is one embedded file paired with its schema_meta version key.
type migration struct {
	version string
	sql     string
}

// loadMigrations reads the embedded migration files in apply order. A
// directory with no files is not an error: the schema may legitimately stop
// at schema.sql, and the caller already applied that.
func loadMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("storage: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([]migration, 0, len(names))
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("storage: read migration %s: %w", name, err)
		}
		out = append(out, migration{version: name, sql: string(body)})
	}
	return out, nil
}

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
	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// applyMigrations runs every embedded migration whose version key is not yet
// recorded in schema_meta. Each migration runs in one transaction so a
// failing file leaves the database unmodified. After each migration commits,
// the version is recorded in schema_meta so a restart doesn't re-apply it.
func applyMigrations(db *sql.DB) error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedMigrations(db)
	if err != nil {
		return err
	}
	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("storage: begin migration %s: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("storage: apply migration %s: %w", m.version, err)
		}
		// Record this migration as applied.
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO schema_meta (id, version, applied_at) VALUES (?, ?, datetime('now'))",
			0, m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("storage: record migration %s: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("storage: commit migration %s: %w", m.version, err)
		}
	}
	return nil
}

// appliedMigrations returns the set of versions already recorded in
// schema_meta.
func appliedMigrations(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_meta")
	if err != nil {
		return nil, fmt.Errorf("storage: read schema_meta: %w", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("storage: scan schema_meta: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate schema_meta: %w", err)
	}
	return out, nil
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

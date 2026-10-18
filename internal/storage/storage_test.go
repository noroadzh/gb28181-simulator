package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
)

func TestBootstrap_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "data.db")

	db, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap first call: %v", err)
	}
	_ = EnsureClosed(db)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected db file at %s: %v", path, err)
	}
	db2, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap second call: %v", err)
	}
	defer func() { _ = EnsureClosed(db2) }()
	// A version row is written for schema.sql's seed and for each applied
	// migration, and it is written with INSERT OR IGNORE, so re-bootstrapping
	// an existing file must leave the recorded versions exactly as they were.
	// Assert the invariant (each version recorded once, set unchanged by a
	// second bootstrap) rather than a fixed row count, which the migrations
	// feature made intentionally open-ended.
	first := versions(t, db2)
	third, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap third call: %v", err)
	}
	defer func() { _ = EnsureClosed(third) }()
	second := versions(t, third)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("schema_meta changed across bootstraps: %v -> %v", first, second)
	}
	if len(second) == 0 {
		t.Fatal("schema_meta has no version rows after Bootstrap")
	}
}

// versions returns the sorted set of version keys recorded in schema_meta,
// failing the test if any version was recorded more than once.
func versions(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT version, COUNT(*) FROM schema_meta GROUP BY version`)
	if err != nil {
		t.Fatalf("query schema_meta: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			t.Fatalf("scan schema_meta: %v", err)
		}
		if n != 1 {
			t.Fatalf("version %q recorded %d times, want 1", v, n)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema_meta: %v", err)
	}
	sort.Strings(out)
	return out
}

func TestDefaultDBPath_Explicit(t *testing.T) {
	cfg := platformconfig.Config{Storage: platformconfig.StorageConfig{Path: "/tmp/explicit.db"}}
	if got, want := DefaultDBPath(cfg), "/tmp/explicit.db"; got != want {
		t.Fatalf("explicit path = %q want %q", got, want)
	}
}

func TestDefaultDBPath_Fallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", `C:\Users\test\AppData\Roaming`)
	} else {
		t.Setenv("XDG_STATE_HOME", "/tmp/state")
	}
	cfg := platformconfig.Config{}
	got := DefaultDBPath(cfg)
	if runtime.GOOS == "windows" {
		if got == "" {
			t.Fatalf("windows fallback path empty")
		}
		return
	}
	if want := "/tmp/state/gb28181-simulator/data.db"; got != want {
		t.Fatalf("xdg fallback = %q want %q", got, want)
	}
}

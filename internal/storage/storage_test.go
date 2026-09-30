package storage

import (
	"os"
	"path/filepath"
	"runtime"
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
	// Verify the meta table has exactly one row.
	var n int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM schema_meta`).Scan(&n); err != nil {
		t.Fatalf("count schema_meta: %v", err)
	}
	if n != 1 {
		t.Fatalf("schema_meta rows = %d, want 1", n)
	}
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

// Package scenario — package store: embedded examples + disk directory.
//
// The store owns the validated scenario catalogue. Built-in packages are
// compiled in via embed.FS so a cold binary works with zero disk
// dependencies; an optional configured directory is scanned at startup and
// merges by name, disk entries winning over built-ins. Invalid disk
// packages are skipped with a startup warning instead of failing the
// process — one broken file must not take the simulator down; a missing
// name at run time surfaces as a 404 from the HTTP layer.
package scenario

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// EmbeddedFS carries the built-in scenario packages (examples/*.yaml).
//
//go:embed examples/*.yaml
var EmbeddedFS embed.FS

// embeddedRoot is the directory inside EmbeddedFS.
const embeddedRoot = "examples"

// Store holds validated scenarios keyed by unique name. Safe for
// concurrent use.
type Store struct {
	mu     sync.RWMutex
	byName map[string]model.Scenario
	logger *slog.Logger
}

// NewStore creates an empty store. A nil logger falls back to slog.Default.
func NewStore(logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		byName: make(map[string]model.Scenario),
		logger: logger,
	}
}

// LoadEmbedded parses every *.yaml package compiled into the binary.
// A malformed built-in package is skipped with a warning; the unit test
// in this package keeps that from ever happening silently.
func (s *Store) LoadEmbedded() error {
	return s.LoadFS(EmbeddedFS, embeddedRoot)
}

// LoadFS loads every *.yaml package under root of the supplied filesystem.
// Later entries overwrite earlier ones with the same name.
func (s *Store) LoadFS(fsys fs.FS, root string) error {
	entries, err := fs.Glob(fsys, root+"/*.yaml")
	if err != nil {
		return fmt.Errorf("scenario store: glob %s: %w", root, err)
	}
	for _, entry := range entries {
		data, err := fs.ReadFile(fsys, entry)
		if err != nil {
			s.logger.Warn("scenario: skip unreadable package", "file", entry, "error", err)
			continue
		}
		s.load(entry, data)
	}
	return nil
}

// LoadDir scans dir for *.yaml packages. Disk entries win over previously
// loaded ones with the same name (design: disk overrides embedded).
func (s *Store) LoadDir(dir string) error {
	entries, err := fs.Glob(os.DirFS(dir), "*.yaml")
	if err != nil {
		return fmt.Errorf("scenario store: glob %s: %w", dir, err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry))
		if err != nil {
			s.logger.Warn("scenario: skip unreadable disk package", "file", entry, "error", err)
			continue
		}
		s.load(entry, data)
	}
	return nil
}

// load parses one package and upserts it; parse failures are logged and
// skipped rather than propagated.
func (s *Store) load(filename string, data []byte) {
	sc, err := LoadScenarioBytes(filename, data)
	if err != nil {
		s.logger.Warn("scenario: skip invalid package", "file", filename, "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byName[sc.Name] = sc
}

// List returns the catalogue sorted by name.
func (s *Store) List() []model.ScenarioMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ScenarioMeta, 0, len(s.byName))
	for _, sc := range s.byName {
		out = append(out, sc.Meta())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the scenario registered under name.
func (s *Store) Get(name string) (model.Scenario, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc, ok := s.byName[name]
	return sc, ok
}

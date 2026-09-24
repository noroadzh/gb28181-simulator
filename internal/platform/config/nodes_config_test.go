package platformconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes body to a temporary YAML file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestLoad_NodesDeclared asserts a `nodes:` list is parsed into the typed
// slice (task 7.1).
func TestLoad_NodesDeclared(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `
nodes:
  - id: "34020000011310000001"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:5060"
    vendor: acme
  - id: "34020000012000000001"
    kind: platform-large
    domain: "3402000000"
    addr: "127.0.0.1:5061"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Nodes) != 2 {
		t.Fatalf("len(Nodes) = %d, want 2", len(cfg.Nodes))
	}
	if got := cfg.Nodes[0].ID; got != "34020000011310000001" {
		t.Errorf("Nodes[0].ID = %q", got)
	}
	if got := cfg.Nodes[0].Vendor; got != "acme" {
		t.Errorf("Nodes[0].Vendor = %q, want acme", got)
	}
	if got := cfg.Nodes[1].Kind; got != "platform-large" {
		t.Errorf("Nodes[1].Kind = %q, want platform-large", got)
	}
	if err := cfg.ValidateNodes(); err != nil {
		t.Errorf("ValidateNodes on a valid list = %v", err)
	}
}

// TestLoad_NodesAbsent asserts omitting the section yields zero nodes and an
// empty (never nil) slice, so callers can iterate without a nil check
// (task 7.1, 7.2).
func TestLoad_NodesAbsent(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, "http:\n  port: 18080\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Nodes == nil {
		t.Fatal("Nodes is nil, want an empty non-nil slice")
	}
	if len(cfg.Nodes) != 0 {
		t.Errorf("len(Nodes) = %d, want 0", len(cfg.Nodes))
	}
	if err := cfg.ValidateNodes(); err != nil {
		t.Errorf("ValidateNodes with no nodes = %v", err)
	}
}

// TestLoad_IllegalNodeIDFails asserts an entry whose id is not a 20-digit
// code fails loading and names the entry index and field (task 7.3).
func TestLoad_IllegalNodeIDFails(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `
nodes:
  - id: "34020000011310000001"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:5060"
  - id: "1234"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:5061"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded with an illegal node id, want error")
	}
	if !strings.Contains(err.Error(), "nodes[1].id") {
		t.Errorf("error = %q, want it to name nodes[1].id", err.Error())
	}
}

// TestLoad_IllegalNodeKindFails asserts an unknown kind fails loading and
// names the entry index and field (task 7.3).
func TestLoad_IllegalNodeKindFails(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `
nodes:
  - id: "34020000011310000001"
    kind: camera
    domain: "3402000000"
    addr: "127.0.0.1:5060"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded with an unknown node kind, want error")
	}
	if !strings.Contains(err.Error(), "nodes[0].kind") {
		t.Errorf("error = %q, want it to name nodes[0].kind", err.Error())
	}
}

// TestLoad_NodesNotSilentlySkipped asserts a bad entry aborts the whole
// load: the process must not start with fewer nodes than configured.
func TestLoad_NodesNotSilentlySkipped(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `
nodes:
  - id: "34020000011310000001"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:5060"
  - id: "34020000011310000002"
    kind: nope
    domain: "3402000000"
    addr: "127.0.0.1:5061"
`)
	cfg, err := Load(path)
	if err == nil {
		t.Fatalf("Load succeeded, want failure; got %d nodes", len(cfg.Nodes))
	}
}

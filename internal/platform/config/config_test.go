package platformconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestConfig_Defaults verifies the seed defaults surface when no file or
// environment variables are present.
func TestConfig_Defaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTP.Addr() != "127.0.0.1:18080" {
		t.Fatalf("http.addr default = %q", cfg.HTTP.Addr())
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("log.level default = %q", cfg.Log.Level)
	}
}

// TestConfig_YAML_Override ensures YAML values flow through viper.
func TestConfig_YAML_Override(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
http:
  host: 0.0.0.0
  port: 19090
log:
  level: debug
  redact_keys: [password]
`), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTP.Addr() != "0.0.0.0:19090" {
		t.Fatalf("http.addr = %q", cfg.HTTP.Addr())
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("log.level = %q", cfg.Log.Level)
	}
	if len(cfg.Log.RedactKeys) != 1 || cfg.Log.RedactKeys[0] != "password" {
		t.Fatalf("log.redact_keys = %v", cfg.Log.RedactKeys)
	}
}

// TestConfig_EnvOverride ensures env var precedence over YAML.
func TestConfig_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("http:\n  port: 18080\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	t.Setenv("GB28181_SIMULATOR_HTTP_PORT", "18081")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTP.Port != 18081 {
		t.Fatalf("env did not override yaml: %q", cfg.HTTP.Addr())
	}
}

// TestConfig_ScenarioDir covers the optional scenario.dir key (Change 15):
// absent by default, honoured when declared, and overridable via env.
func TestConfig_ScenarioDir(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Scenario.Dir != "" {
		t.Fatalf("scenario.dir default = %q, want empty", cfg.Scenario.Dir)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("scenario:\n  dir: /data/scenarios\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Scenario.Dir != "/data/scenarios" {
		t.Fatalf("scenario.dir = %q, want /data/scenarios", cfg.Scenario.Dir)
	}

	t.Setenv("GB28181_SIMULATOR_SCENARIO_DIR", "/env/scenarios")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load with env: %v", err)
	}
	if cfg.Scenario.Dir != "/env/scenarios" {
		t.Fatalf("env scenario.dir = %q, want /env/scenarios", cfg.Scenario.Dir)
	}
}

// TestDefaultConfigPath_XDG ensures XDG_CONFIG_HOME is honoured on
// Linux/macOS and a sensible default is returned when unset.
func TestDefaultConfigPath_XDG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("xdg path applies only on linux/darwin")
	}
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	if got, want := DefaultConfigPath(), "/custom/xdg/gb28181-simulator/config.yaml"; got != want {
		t.Fatalf("with XDG set, want %q got %q", want, got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	if got, want := DefaultConfigPath(), "/tmp/home/.config/gb28181-simulator/config.yaml"; got != want {
		t.Fatalf("with HOME only, want %q got %q", want, got)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GB28181_SIMULATOR_HTTP_HOST",
		"GB28181_SIMULATOR_HTTP_PORT",
		"GB28181_SIMULATOR_HTTP_ADDR",
		"GB28181_SIMULATOR_LOG_LEVEL",
		"GB28181_SIMULATOR_LOG_FILE",
	} {
		_ = os.Unsetenv(k)
	}
}

// TestConfig_NodeMediaConfig_Decode verifies the media: sub-section is
// decoded into NodeMediaConfig and surfaces on NodeConfig.Media. Other
// tests in this package ensure Load rejects bad id/kind; here we only
// need to confirm the YAML shape is wired.
func TestConfig_NodeMediaConfig_Decode(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(`
http:
  host: 127.0.0.1
  port: 8080
nodes:
  - id: "34020000001110000001"
    kind: "device"
    domain: "3402000000"
    addr: "127.0.0.1:5060"
    media:
      kind: "synthetic"
      fps: 30
      mtu: 1300
      clock: 90000
  - id: "34020000001110000002"
    kind: "device"
    domain: "3402000000"
    addr: "127.0.0.1:5061"
    media:
      kind: "file"
      path: "/tmp/clip.mp4"
      loop: true
`), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(cfg.Nodes))
	}
	if cfg.Nodes[0].Media == nil {
		t.Fatalf("node[0] media missing")
	}
	if cfg.Nodes[0].Media.Kind != "synthetic" || cfg.Nodes[0].Media.FPS != 30 || cfg.Nodes[0].Media.MTU != 1300 {
		t.Errorf("synthetic media decode wrong: %+v", cfg.Nodes[0].Media)
	}
	if cfg.Nodes[1].Media == nil {
		t.Fatalf("node[1] media missing")
	}
	if cfg.Nodes[1].Media.Kind != "file" || cfg.Nodes[1].Media.Path != "/tmp/clip.mp4" || !cfg.Nodes[1].Media.Loop {
		t.Errorf("file media decode wrong: %+v", cfg.Nodes[1].Media)
	}
}

// TestConfig_NodeMediaConfig_Absent covers the case where nodes: exists
// but the optional media: sub-section is absent — the loader must not
// synthesise a media struct.
func TestConfig_NodeMediaConfig_Absent(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(`
http:
  host: 127.0.0.1
  port: 8080
nodes:
  - id: "34020000001110000003"
    kind: "device"
    domain: "3402000000"
    addr: "127.0.0.1:5062"
`), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Nodes) != 1 || cfg.Nodes[0].Media != nil {
		t.Fatalf("absent media should be nil, got %+v", cfg.Nodes[0].Media)
	}
}

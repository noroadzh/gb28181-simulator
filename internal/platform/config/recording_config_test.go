package platformconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeRecordingYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	return path
}

// Recording is on by default: a pull is an explicit action, and keeping it
// is what a platform is for.
func TestRecordingConfig_Defaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Recording.Enabled {
		t.Error("recording.enabled default = false, want true")
	}
	if got := cfg.Recording.SegmentDuration(); got != 5*time.Minute {
		t.Errorf("segment duration = %s, want 5m", got)
	}
	if got := cfg.Recording.DirOrDefault("/data/app.db"); got != filepath.Join("/data", "recordings") {
		t.Errorf("default dir = %q", got)
	}
}

func TestRecordingConfig_Override(t *testing.T) {
	clearEnv(t)
	cfg, err := Load(writeRecordingYAML(t, `
recording:
  enabled: false
  dir: /tmp/rec
  segment_seconds: 42
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Recording.Enabled {
		t.Error("enabled = true, want false")
	}
	if cfg.Recording.DirOrDefault("/data/app.db") != "/tmp/rec" {
		t.Errorf("dir = %q", cfg.Recording.DirOrDefault("/data/app.db"))
	}
	if got := cfg.Recording.SegmentDuration(); got != 42*time.Second {
		t.Errorf("segment = %s, want 42s", got)
	}
}

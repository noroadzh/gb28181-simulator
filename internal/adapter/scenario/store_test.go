package scenario

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func embeddedPayload(t *testing.T, steps string) []byte {
	t.Helper()
	return []byte("name: dup\nsteps:\n" + steps + "\n")
}

func TestStore_LoadFS_DiskWinsOverEmbedded(t *testing.T) {
	s := NewStore(nil)
	fsys := fstest.MapFS{
		"examples/dup.yaml": &fstest.MapFile{Data: embeddedPayload(t, "- type: wait\n  params:\n    seconds: 1")},
	}
	if err := s.LoadFS(fsys, "examples"); err != nil {
		t.Fatalf("LoadFS = error %v", err)
	}

	dir := t.TempDir()
	disk := "name: dup\nsteps:\n- type: wait\n  timeout: 2\n  params:\n    seconds: 2\n"
	if err := os.WriteFile(filepath.Join(dir, "dup.yaml"), []byte(disk), 0o644); err != nil {
		t.Fatalf("write disk package: %v", err)
	}
	if err := s.LoadDir(dir); err != nil {
		t.Fatalf("LoadDir = error %v", err)
	}

	sc, ok := s.Get("dup")
	if !ok {
		t.Fatalf("Get(dup) = missing, want present")
	}
	if len(sc.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(sc.Steps))
	}
	if got := sc.Steps[0].EffectiveTimeout(); got != 2*time.Second {
		t.Fatalf("timeout = %v, want 2s (disk wins)", got)
	}
}

func TestStore_LoadDir_SkipsInvalidPackages(t *testing.T) {
	s := NewStore(nil)

	dir := t.TempDir()
	good := "name: good-package\nsteps:\n- type: wait\n  params:\n    seconds: 1\n"
	if err := os.WriteFile(filepath.Join(dir, "good.yaml"), []byte(good), 0o644); err != nil {
		t.Fatalf("write good package: %v", err)
	}
	broken := "name: broken-package\nsteps:\n- type: teleport\n"
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatalf("write broken package: %v", err)
	}
	if err := s.LoadDir(dir); err != nil {
		t.Fatalf("LoadDir = error %v", err)
	}

	if _, ok := s.Get("good-package"); !ok {
		t.Fatalf("Get(good-package) = missing, want present")
	}
	if _, ok := s.Get("broken-package"); ok {
		t.Fatalf("Get(broken-package) = present, want skipped")
	}
	if got := len(s.List()); got != 1 {
		t.Fatalf("List() = %d entries, want 1", got)
	}
}

func TestStore_List_Sorted(t *testing.T) {
	s := NewStore(nil)
	for _, name := range []string{"zebra", "alpha", "mid"} {
		fsys := fstest.MapFS{
			"examples/x.yaml": &fstest.MapFile{Data: []byte("name: " + name + "\nsteps:\n- type: wait\n")},
		}
		if err := s.LoadFS(fsys, "examples"); err != nil {
			t.Fatalf("LoadFS(%s) = error %v", name, err)
		}
	}
	names := []string{}
	for _, m := range s.List() {
		names = append(names, m.Name)
	}
	want := []string{"alpha", "mid", "zebra"}
	if len(names) != len(want) {
		t.Fatalf("List() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("List() = %v, want sorted %v", names, want)
		}
	}
}

func TestStore_LoadEmbedded_AtLeastThreeValidPackages(t *testing.T) {
	s := NewStore(nil)
	if err := s.LoadEmbedded(); err != nil {
		t.Fatalf("LoadEmbedded = error %v", err)
	}
	if got := len(s.List()); got < 3 {
		t.Fatalf("embedded packages = %d, want at least 3", got)
	}
	for _, m := range s.List() {
		sc, ok := s.Get(m.Name)
		if !ok {
			t.Fatalf("Get(%s) = missing right after List", m.Name)
		}
		if len(sc.Steps) == 0 {
			t.Fatalf("package %s has no steps", m.Name)
		}
		for i, step := range sc.Steps {
			if !KnownStepTypes[step.Type] {
				t.Fatalf("package %s step %d has unknown type %q", m.Name, i, step.Type)
			}
		}
	}
}

func TestStore_Get_Missing(t *testing.T) {
	s := NewStore(nil)
	if _, ok := s.Get("nope"); ok {
		t.Fatalf("Get(nope) = present, want missing")
	}
	if s.List() == nil {
		t.Fatalf("List() = nil, want empty slice")
	}
	var _ model.ScenarioMeta
}

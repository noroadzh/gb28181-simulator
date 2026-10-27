package platformconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// The GB 35114 block is off unless the operator turns it on: an existing
// deployment's YAML keeps parsing and keeping MD5 behaviour.
func TestConfig_SecurityGB35114_AbsentMeansDisabled(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Security.GB35114.Enabled {
		t.Error("security.gb35114.enabled default = true, want false")
	}
}

func TestConfig_SecurityGB35114_Decode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
security:
  gb35114:
    enabled: true
    note_integrity: true
    sm2_private_key_file: /etc/gb28181/sm2_private.pem
    sm2_public_key_file: /etc/gb28181/sm2_public.pem
`), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	g := cfg.Security.GB35114
	if !g.Enabled || !g.NoteIntegrity {
		t.Errorf("enabled=%v note_integrity=%v, want both true", g.Enabled, g.NoteIntegrity)
	}
	if g.SM2PrivateKeyFile != "/etc/gb28181/sm2_private.pem" {
		t.Errorf("sm2_private_key_file = %q", g.SM2PrivateKeyFile)
	}
	if g.SM2PublicKeyFile != "/etc/gb28181/sm2_public.pem" {
		t.Errorf("sm2_public_key_file = %q", g.SM2PublicKeyFile)
	}
}

func TestGB35114_IdentityDisabled(t *testing.T) {
	priv, pub, err := GB35114Config{}.Identity()
	if err != nil || priv != nil || pub != nil {
		t.Fatalf("Identity() = %x, %x, %v; want all nil", priv, pub, err)
	}
}

// Enabled without both paths cannot boot: a half identity would only surface
// later, once per registration.
func TestGB35114_IdentityRequiresBothPaths(t *testing.T) {
	for name, cfg := range map[string]GB35114Config{
		"no paths":     {Enabled: true},
		"private only": {Enabled: true, SM2PrivateKeyFile: "/tmp/priv"},
		"public only":  {Enabled: true, SM2PublicKeyFile: "/tmp/pub"},
		"blank paths":  {Enabled: true, SM2PrivateKeyFile: "  ", SM2PublicKeyFile: "  "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := cfg.Identity(); err == nil {
				t.Fatal("Identity() accepted an incomplete identity")
			}
		})
	}
}

func TestGB35114_IdentityReadsFiles(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "priv")
	pubPath := filepath.Join(dir, "pub")
	if err := os.WriteFile(privPath, []byte("private-bytes"), 0o600); err != nil {
		t.Fatalf("write private: %v", err)
	}
	if err := os.WriteFile(pubPath, []byte("public-bytes"), 0o644); err != nil {
		t.Fatalf("write public: %v", err)
	}
	priv, pub, err := GB35114Config{
		Enabled:           true,
		SM2PrivateKeyFile: privPath,
		SM2PublicKeyFile:  pubPath,
	}.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if string(priv) != "private-bytes" || string(pub) != "public-bytes" {
		t.Errorf("Identity() = %q, %q", priv, pub)
	}
}

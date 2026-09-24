package platformconfig

import (
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// nodeYAML wraps a registration block into a one-node configuration.
func nodeYAML(t *testing.T, registration string) string {
	t.Helper()
	return `
nodes:
  - id: "34020000011310000001"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:5060"
` + registration
}

// TestLoad_NodeRegistrationParsed asserts the optional `registration:`
// block is decoded into the typed sub-structure (task 5.1).
func TestLoad_NodeRegistrationParsed(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, `    registration:
      server: "127.0.0.1:15060"
      server_id: "34020000002000000001"
      username: "34020000011310000001"
      password: "secret"
      gb_version: "2022"
      expires: 600
      transport: tcp
      timeout: 3s
`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Nodes[0].Registration
	if got == nil {
		t.Fatal("Registration is nil, want the decoded block")
	}
	if got.Server != "127.0.0.1:15060" {
		t.Errorf("server = %q", got.Server)
	}
	if got.ServerID != "34020000002000000001" {
		t.Errorf("server_id = %q", got.ServerID)
	}
	if got.Username != "34020000011310000001" {
		t.Errorf("username = %q", got.Username)
	}
	if got.GBVersion != "2022" {
		t.Errorf("gb_version = %q", got.GBVersion)
	}
	if got.Expires != 600 {
		t.Errorf("expires = %d, want 600", got.Expires)
	}
	if got.Transport != "tcp" {
		t.Errorf("transport = %q, want tcp", got.Transport)
	}
	if got.Timeout != 3*time.Second {
		t.Errorf("timeout = %v, want 3s", got.Timeout)
	}
}

// TestLoad_NodeRegistrationDefaults asserts that omitting the optional
// fields leaves the model's defaults to apply, and that such a node is
// still valid (task 5.1).
func TestLoad_NodeRegistrationDefaults(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, `    registration:
      server: "127.0.0.1:15060"
      password: "secret"
`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Nodes[0].Registration
	if got.Expires != 0 || got.Transport != "" || got.Timeout != 0 {
		t.Errorf("unset fields must stay zero: expires=%d transport=%q timeout=%v",
			got.Expires, got.Transport, got.Timeout)
	}
	// The model fills them in at construction time.
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:   got.Server,
		Password: got.Password,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	if reg.Expires() != model.DefaultExpires || reg.Transport() != model.DefaultTransport ||
		reg.Timeout() != model.DefaultTimeout {
		t.Errorf("defaults not applied: expires=%d transport=%q timeout=%v",
			reg.Expires(), reg.Transport(), reg.Timeout())
	}
}

// TestLoad_NodeRegistrationAbsent asserts a node without the block loads
// exactly as it did before the block existed (task 5.1: backwards
// compatible).
func TestLoad_NodeRegistrationAbsent(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, ""))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Nodes[0].Registration != nil {
		t.Errorf("Registration = %+v, want nil", cfg.Nodes[0].Registration)
	}
}

// Every way of writing an unusable registration must fail the load and name
// the entry index and field (task 5.2).
func TestLoad_IllegalRegistrationFails(t *testing.T) {
	tests := []struct {
		name         string
		registration string
	}{
		{
			name: "server without port",
			registration: `    registration:
      server: "127.0.0.1"
      password: "secret"
`,
		},
		{
			name: "no password",
			registration: `    registration:
      server: "127.0.0.1:15060"
`,
		},
		{
			name: "unknown transport",
			registration: `    registration:
      server: "127.0.0.1:15060"
      password: "secret"
      transport: sctp
`,
		},
		{
			name: "illegal server id",
			registration: `    registration:
      server: "127.0.0.1:15060"
      password: "secret"
      server_id: "123"
`,
		},
		{
			name: "empty server",
			registration: `    registration:
      password: "secret"
`,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			path := writeConfig(t, nodeYAML(t, tc.registration))
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load succeeded, want failure")
			}
			if !strings.Contains(err.Error(), "nodes[0].registration") {
				t.Errorf("error = %q, want it to name nodes[0].registration", err.Error())
			}
		})
	}
}

// A misconfigured credential must never be echoed back — the error is
// rendered to the operator and written to the log (task 5.2).
func TestLoad_RegistrationErrorNeverEchoesPassword(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, `    registration:
      server: "127.0.0.1"
      password: "super-secret"
`))
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want failure")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("error leaks the password: %v", err)
	}
}

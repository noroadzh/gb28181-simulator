package platformconfig

import (
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// The keepalive that holds a registration open is configured next to the
// registration itself, and stays optional.
func TestLoad_NodeHeartbeatParsed(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, `    registration:
      server: "127.0.0.1:15060"
      password: "secret"
      heartbeat_interval: 30s
      heartbeat_timeout: 2s
      heartbeat_max_failures: 5
`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Nodes[0].Registration
	if got == nil {
		t.Fatal("Registration is nil, want the decoded block")
	}
	if got.HeartbeatInterval != 30*time.Second {
		t.Errorf("heartbeat_interval = %v, want 30s", got.HeartbeatInterval)
	}
	if got.HeartbeatTimeout != 2*time.Second {
		t.Errorf("heartbeat_timeout = %v, want 2s", got.HeartbeatTimeout)
	}
	if got.HeartbeatMaxFailures != 5 {
		t.Errorf("heartbeat_max_failures = %d, want 5", got.HeartbeatMaxFailures)
	}
}

// Leaving the heartbeat out is legal and means "use the GB/T 28181
// defaults": a minute between beats, seconds to answer, three tolerated
// misses.
func TestLoad_NodeHeartbeatDefaults(t *testing.T) {
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
	if got.HeartbeatInterval != 0 || got.HeartbeatTimeout != 0 || got.HeartbeatMaxFailures != 0 {
		t.Fatalf("unset heartbeat keys decoded as %v/%v/%d, want zero values",
			got.HeartbeatInterval, got.HeartbeatTimeout, got.HeartbeatMaxFailures)
	}
	// The zero values are what tells the model to apply its defaults.
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:   got.Server,
		Password: got.Password,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	if reg.HeartbeatInterval() != model.DefaultHeartbeatInterval ||
		reg.HeartbeatTimeout() != model.DefaultHeartbeatTimeout ||
		reg.MaxHeartbeatFailures() != model.DefaultHeartbeatMaxFailures {
		t.Errorf("defaults = %v/%v/%d, want %v/%v/%d",
			reg.HeartbeatInterval(), reg.HeartbeatTimeout(), reg.MaxHeartbeatFailures(),
			model.DefaultHeartbeatInterval, model.DefaultHeartbeatTimeout,
			model.DefaultHeartbeatMaxFailures)
	}
}

// Writing an explicit zero is the same as leaving the key out: the
// registration keeps the GB/T 28181 defaults. Only impossible or negative
// values are configuration errors.
func TestLoad_NodeHeartbeatExplicitZeroTakesDefaults(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, `    registration:
      server: "127.0.0.1:15060"
      password: "secret"
      heartbeat_interval: 0s
      heartbeat_timeout: 0s
      heartbeat_max_failures: 0
`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Nodes[0].Registration
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:               got.Server,
		Password:             got.Password,
		HeartbeatInterval:    got.HeartbeatInterval,
		HeartbeatTimeout:     got.HeartbeatTimeout,
		HeartbeatMaxFailures: got.HeartbeatMaxFailures,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	if reg.HeartbeatInterval() != model.DefaultHeartbeatInterval ||
		reg.HeartbeatTimeout() != model.DefaultHeartbeatTimeout ||
		reg.MaxHeartbeatFailures() != model.DefaultHeartbeatMaxFailures {
		t.Errorf("defaults = %v/%v/%d, want %v/%v/%d",
			reg.HeartbeatInterval(), reg.HeartbeatTimeout(), reg.MaxHeartbeatFailures(),
			model.DefaultHeartbeatInterval, model.DefaultHeartbeatTimeout,
			model.DefaultHeartbeatMaxFailures)
	}
}

// An impossible schedule is a configuration error naming the entry and the
// field, not something to repair silently.
func TestLoad_NodeHeartbeatInvalid(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			"negative interval",
			"    registration:\n      server: \"127.0.0.1:15060\"\n      password: \"secret\"\n      heartbeat_interval: -30s\n",
			"heartbeat",
		},
		{
			"timeout not shorter than interval",
			"    registration:\n      server: \"127.0.0.1:15060\"\n      password: \"secret\"\n      heartbeat_interval: 5s\n      heartbeat_timeout: 10s\n",
			"heartbeat",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			path := writeConfig(t, nodeYAML(t, tc.yaml))
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected a configuration error")
			}
			msg := err.Error()
			if !strings.Contains(msg, "nodes[0]") || !strings.Contains(msg, tc.want) {
				t.Errorf("error = %q, want it to name nodes[0] and the heartbeat field", msg)
			}
		})
	}
}

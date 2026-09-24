package platformconfig

import (
	"strings"
	"testing"
)

// smallYAML wraps body in a platform-small entry — the identity that carries
// both halves of a cascade.
func smallYAML(body string) string {
	return `
nodes:
  - id: "34020000002160000001"
    kind: platform-small
    domain: "3402000000"
    addr: "127.0.0.1:5062"
` + body
}

// A platform-small may declare both halves at once: `platform:` is how it
// serves the downstreams below it, `registration:` how it registers with the
// platform above. Neither section is reserved for another kind.
func TestLoad_PlatformSmallBothHalves(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, smallYAML(`    platform:
      realm: "3402000000"
      accounts:
        - username: "34020000011310000002"
          password: "secret-two"
      min_expires: 60
      default_expires: 3600
      max_expires: 86400
    registration:
      server: "127.0.0.1:5061"
      server_id: "34020000002000000001"
      password: "secret-one"
      expires: 3600
`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.ValidateNodes(); err != nil {
		t.Fatalf("ValidateNodes: %v", err)
	}
	if len(cfg.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1", len(cfg.Nodes))
	}
	node := cfg.Nodes[0]
	if node.Kind != "platform-small" {
		t.Errorf("kind = %q, want platform-small", node.Kind)
	}
	if node.Platform == nil || len(node.Platform.Accounts) != 1 {
		t.Fatalf("platform = %+v, want one account", node.Platform)
	}
	if node.Registration == nil || node.Registration.Server != "127.0.0.1:5061" {
		t.Fatalf("registration = %+v, want the upstream parsed", node.Registration)
	}
}

// Either half alone is a complete platform-small: one that serves nobody's
// subordinate, or one that serves with the defaults while being a
// subordinate itself.
func TestLoad_PlatformSmallOneHalfIsEnough(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			"serving only",
			"    platform:\n      realm: \"3402000000\"\n",
		},
		{
			"registering only",
			"    registration:\n      server: \"127.0.0.1:5061\"\n      password: \"secret-one\"\n",
		},
		{
			"neither",
			"",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			cfg, err := Load(writeConfig(t, smallYAML(tc.yaml)))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if err := cfg.ValidateNodes(); err != nil {
				t.Errorf("ValidateNodes: %v", err)
			}
		})
	}
}

// Both halves are open to a platform-small, but what is malformed in them is
// still malformed: the validation is per section, not per kind.
func TestLoad_PlatformSmallInvalid(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			"blank realm",
			"    platform:\n      realm: \"\"\n",
			"realm",
		},
		{
			"duplicate account username",
			"    platform:\n      accounts:\n        - username: \"34020000011310000002\"\n          password: \"a\"\n        - username: \"34020000011310000002\"\n          password: \"b\"\n",
			"username",
		},
		{
			"empty account password",
			"    platform:\n      accounts:\n        - username: \"34020000011310000002\"\n          password: \"\"\n",
			"password",
		},
		{
			"window not monotonic",
			"    platform:\n      min_expires: 600\n      default_expires: 60\n      max_expires: 7200\n",
			"platform",
		},
		{
			"registration without a server",
			"    registration:\n      password: \"secret-one\"\n",
			"server",
		},
		{
			"heartbeat timeout longer than its interval",
			"    registration:\n      server: \"127.0.0.1:5061\"\n      password: \"secret-one\"\n      heartbeat_interval: 5s\n      heartbeat_timeout: 60s\n",
			"heartbeat",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			_, err := Load(writeConfig(t, smallYAML(tc.yaml)))
			if err == nil {
				t.Fatal("expected a configuration error")
			}
			msg := err.Error()
			if !strings.Contains(msg, "nodes[0]") || !strings.Contains(msg, tc.want) {
				t.Errorf("error = %q, want it to name nodes[0] and %q", msg, tc.want)
			}
			// Passwords travel through this path, so no error may echo
			// one back.
			if strings.Contains(msg, "secret-one") || strings.Contains(msg, "secret-two") {
				t.Errorf("error leaks a password: %q", msg)
			}
		})
	}
}

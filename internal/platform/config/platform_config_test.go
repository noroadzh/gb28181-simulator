package platformconfig

import (
	"strings"
	"testing"
	"time"
)

// A platform's section decides who may register with it, so it is parsed
// into values the composition root can hand straight to the domain.
func TestLoad_NodePlatformParsed(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, `    platform:
      realm: "3402000000"
      accounts:
        - username: "34020000011310000001"
          password: "secret-one"
        - username: "34020000011310000002"
          password: "secret-two"
      min_expires: 60
      default_expires: 1800
      max_expires: 7200
`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Nodes[0].Platform
	if got == nil {
		t.Fatal("Platform is nil, want the decoded block")
	}
	if got.Realm == nil || *got.Realm != "3402000000" {
		t.Errorf("realm = %v, want 3402000000", got.Realm)
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(got.Accounts))
	}
	if got.Accounts[0].Username != "34020000011310000001" || got.Accounts[0].Password != "secret-one" {
		t.Errorf("account 0 = %+v", got.Accounts[0])
	}
	if got.Min != 60 || got.Default != 1800 || got.Max != 7200 {
		t.Errorf("window = %d/%d/%d, want 60/1800/7200", got.Min, got.Default, got.Max)
	}
}

// An absent `platform:` section is legal: the node then serves in its own
// domain with the documented window.
func TestLoad_NodePlatformAbsent(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, ""))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Nodes[0].Platform != nil {
		t.Fatalf("Platform = %+v, want nil", cfg.Nodes[0].Platform)
	}
}

// Declaring the section but leaving the keys out is the same as leaving the
// section out: everything takes its default.
func TestLoad_NodePlatformDefaults(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, "    platform: {}\n"))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Nodes[0].Platform
	if got == nil {
		t.Fatal("Platform is nil, want an empty block")
	}
	if got.Realm != nil {
		t.Errorf("realm = %v, want nil so the node's domain applies", *got.Realm)
	}
	if len(got.Accounts) != 0 {
		t.Errorf("accounts = %d, want 0", len(got.Accounts))
	}
	if got.Min != 0 || got.Default != 0 || got.Max != 0 {
		t.Errorf("window = %d/%d/%d, want zero values so the model defaults apply",
			got.Min, got.Default, got.Max)
	}
}

// Every invalid section is a load failure naming the entry and the field —
// never a silently ignored account.
func TestLoad_NodePlatformInvalid(t *testing.T) {
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
			"duplicate username",
			"    platform:\n      accounts:\n        - username: \"34020000011310000001\"\n          password: \"a\"\n        - username: \"34020000011310000001\"\n          password: \"b\"\n",
			"username",
		},
		{
			"empty password",
			"    platform:\n      accounts:\n        - username: \"34020000011310000001\"\n          password: \"\"\n",
			"password",
		},
		{
			"empty username",
			"    platform:\n      accounts:\n        - username: \"\"\n          password: \"a\"\n",
			"username",
		},
		{
			"window not monotonic",
			"    platform:\n      min_expires: 600\n      default_expires: 60\n      max_expires: 7200\n",
			"platform",
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
				t.Errorf("error = %q, want it to name nodes[0] and %q", msg, tc.want)
			}
			// Passwords are configured through this path, so no error
			// may echo one.
			if strings.Contains(msg, "secret-one") {
				t.Errorf("error leaks a password: %q", msg)
			}
		})
	}
}

// A platform section sits next to a device's registration section without
// disturbing it: the two are independent.
func TestLoad_NodePlatformAlongsideRegistration(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, nodeYAML(t, `    registration:
      server: "127.0.0.1:15060"
      password: "secret"
      heartbeat_interval: 30s
    platform:
      realm: "3402000000"
      accounts:
        - username: "34020000011310000001"
          password: "secret"
`))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	node := cfg.Nodes[0]
	if node.Registration == nil || node.Registration.HeartbeatInterval != 30*time.Second {
		t.Errorf("registration = %+v, want it parsed with a 30s heartbeat", node.Registration)
	}
	if node.Platform == nil || len(node.Platform.Accounts) != 1 {
		t.Errorf("platform = %+v, want one account", node.Platform)
	}
}

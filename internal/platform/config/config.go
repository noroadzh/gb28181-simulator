// Package config loads the simulator's YAML configuration and exposes it as a
// strongly-typed structure. Environment variables prefixed with
// GB28181_SIMULATOR_ override any value found in the file (dots are mapped to
// underscores, e.g. http.addr → HTTP_ADDR). Default paths follow XDG on
// Linux/macOS and %AppData% on Windows.
package platformconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Config is the top-level configuration tree. Other internal packages should
// accept this struct as a value so future fields are added centrally.
type Config struct {
	HTTP    HTTPConfig    `mapstructure:"http"`
	Log     LogConfig     `mapstructure:"log"`
	Storage StorageConfig `mapstructure:"storage"`
	Tracing TracingConfig `mapstructure:"tracing"`
	Nodes   []NodeConfig  `mapstructure:"nodes"`
}

// NodeConfig is one entry of the optional `nodes:` list. Every field except
// Vendor is mandatory: an entry with an illegal id or an unknown kind fails
// configuration loading rather than being skipped silently (design D7).
type NodeConfig struct {
	ID     string `mapstructure:"id"`
	Kind   string `mapstructure:"kind"`
	Domain string `mapstructure:"domain"`
	Addr   string `mapstructure:"addr"`
	Vendor string `mapstructure:"vendor"`

	// Registration is optional. When it is absent the node does not
	// register with an upstream: a device stays at `registering` — the
	// behaviour it had before registration existed — and a platform-small
	// serves its own downstreams and nobody's subordinate.
	Registration *NodeRegistrationConfig `mapstructure:"registration"`

	// Platform is optional and meaningful for either platform kind —
	// platform-large and platform-small: how the node serves its own
	// downstreams. Without it a platform serves in its own domain with
	// the default lifetime window; being a platform is what makes a node
	// serve, so there is no way to say "do not serve".
	Platform *NodePlatformConfig `mapstructure:"platform"`
}

// NodePlatformConfig is the `platform:` sub-section of a node entry: the
// realm the platform challenges in, the accounts it accepts, and the
// lifetime window it grants. It applies to a platform-small exactly as it
// does to a platform-large — the middle of a cascade serves its
// downstreams the same way a centre does. Every field is optional; a
// platform without accounts accepts nobody, which is explicit rather than
// accidental.
type NodePlatformConfig struct {
	// Realm is a pointer so that "declared but blank" — a configuration
	// mistake worth reporting — is distinguishable from "not declared",
	// which simply means "use the node's home domain".
	Realm    *string               `mapstructure:"realm"`
	Accounts []NodePlatformAccount `mapstructure:"accounts"`
	Min      uint32                `mapstructure:"min_expires"`
	Default  uint32                `mapstructure:"default_expires"`
	Max      uint32                `mapstructure:"max_expires"`
}

// NodePlatformAccount is one account a platform accepts: a downstream's
// username and the password it must prove. Passwords never reach a log, an
// error body or an HTTP response.
type NodePlatformAccount struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

// NodeRegistrationConfig is the `registration:` sub-section of a node entry:
// where the node registers and how it holds that registration open. It is
// what a device is, and it is also what makes a platform-small a link in a
// cascade rather than a platform that stands alone — a platform-small with
// both this and `platform:` serves its own downstreams and registers with
// its upstream. Server is required (it is what makes the node register at
// all); everything else falls back to model defaults.
type NodeRegistrationConfig struct {
	Server    string        `mapstructure:"server"`
	ServerID  string        `mapstructure:"server_id"`
	Username  string        `mapstructure:"username"`
	Password  string        `mapstructure:"password"`
	GBVersion string        `mapstructure:"gb_version"`
	Expires   uint32        `mapstructure:"expires"`
	Timeout   time.Duration `mapstructure:"timeout"`
	Transport string        `mapstructure:"transport"`

	// How the node holds the registration open once it is online. All
	// three are optional and fall back to the GB/T 28181 practice of a
	// heartbeat a minute, answered within seconds, tolerated thrice.
	HeartbeatInterval    time.Duration `mapstructure:"heartbeat_interval"`
	HeartbeatTimeout     time.Duration `mapstructure:"heartbeat_timeout"`
	HeartbeatMaxFailures uint32        `mapstructure:"heartbeat_max_failures"`
}

// ValidateNodes checks every `nodes:` entry and returns an error naming the
// entry index and the offending field, so an operator can fix the file
// without guessing. An absent list is valid and means "zero nodes".
func (c Config) ValidateNodes() error {
	for i, n := range c.Nodes {
		if _, err := model.ParseNodeID(n.ID); err != nil {
			return fmt.Errorf("config: nodes[%d].id: %w", i, err)
		}
		if _, err := model.ParseNodeKind(n.Kind); err != nil {
			return fmt.Errorf("config: nodes[%d].kind: %w", i, err)
		}
		if n.Platform != nil {
			if err := validateNodePlatform(i, n.Platform); err != nil {
				return err
			}
		}
		if n.Registration != nil {
			r := n.Registration
			if _, err := model.NewRegistration(model.RegistrationParams{
				Server:    r.Server,
				ServerID:  r.ServerID,
				Username:  r.Username,
				Password:  r.Password,
				GBVersion: r.GBVersion,
				Expires:   r.Expires,
				Timeout:   r.Timeout,
				Transport: r.Transport,

				HeartbeatInterval:    r.HeartbeatInterval,
				HeartbeatTimeout:     r.HeartbeatTimeout,
				HeartbeatMaxFailures: r.HeartbeatMaxFailures,
			}); err != nil {
				// The model never echoes the password, so this message
				// cannot leak it either.
				return fmt.Errorf("config: nodes[%d].registration: %w", i, err)
			}
		}
	}
	return nil
}

// validateNodePlatform checks one `platform:` sub-section. A platform's
// accounts are how it decides who may join, so a section that declares one
// badly is a configuration error — never a silently ignored account.
func validateNodePlatform(index int, p *NodePlatformConfig) error {
	if p.Realm != nil && strings.TrimSpace(*p.Realm) == "" {
		return fmt.Errorf("config: nodes[%d].platform.realm: empty realm", index)
	}
	seen := make(map[string]bool, len(p.Accounts))
	for j, acc := range p.Accounts {
		if strings.TrimSpace(acc.Username) == "" {
			return fmt.Errorf("config: nodes[%d].platform.accounts[%d].username: empty username", index, j)
		}
		if acc.Password == "" {
			// Never echo the value: say which field is wrong, not what
			// it holds.
			return fmt.Errorf("config: nodes[%d].platform.accounts[%d].password: empty password", index, j)
		}
		if seen[acc.Username] {
			return fmt.Errorf("config: nodes[%d].platform.accounts[%d].username: duplicate account %q",
				index, j, acc.Username)
		}
		seen[acc.Username] = true
	}
	if _, err := model.NewExpiresPolicy(p.Min, p.Default, p.Max); err != nil {
		return fmt.Errorf("config: nodes[%d].platform: %w", index, err)
	}
	return nil
}

// TracingConfig configures the OpenTelemetry trace pipeline. Added in
// Change 3 §3.2. When Enabled is false the trace provider is still
// constructed but no exporter is wired; cmd/main.go decides whether to
// honour the flag or short-circuit.
type TracingConfig struct {
	Enabled      bool    `mapstructure:"enabled"`
	SampleRatio  float64 `mapstructure:"sample_ratio"`  // 0.0 .. 1.0; 0 disables sampling
	OTLPEndpoint string  `mapstructure:"otlp_endpoint"` // host:port for OTLP gRPC; empty = stdout only
	ServiceName  string  `mapstructure:"service_name"`  // defaults to "gb28181-simulator"
}

// HTTPConfig configures the Echo-backed HTTP server.
type HTTPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// Addr returns host:port suitable for net.Listen.
func (h HTTPConfig) Addr() string {
	return fmt.Sprintf("%s:%d", h.Host, h.Port)
}

// LogConfig configures the logger package.
type LogConfig struct {
	Level      Level    `mapstructure:"level"`
	File       string   `mapstructure:"file"`
	AddSource  bool     `mapstructure:"add_source"`
	RedactKeys []string `mapstructure:"redact_keys"`
}

// Level is the user-facing log severity for the YAML layer. We re-use
// logger.Level indirectly via SetLogLevel.
type Level string

// StorageConfig configures the SQLite-backed storage package.
type StorageConfig struct {
	Path string `mapstructure:"path"`
}

// Defaults returns a Config populated with safe defaults. Used when no file
// is provided (developer convenience) and as the seed before overrides.
func Defaults() Config {
	return Config{
		HTTP: HTTPConfig{
			Host: "127.0.0.1",
			Port: 18080,
		},
		Log: LogConfig{
			Level: "info",
		},
	}
}

// Load reads the YAML configuration file at the supplied path and merges it
// onto Defaults. Empty path falls back to DefaultConfigPath().
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	v := viper.New()
	v.SetEnvPrefix("GB28181_SIMULATOR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if path != "" {
		v.SetConfigFile(path)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			// A missing file with defaults is acceptable for first-run; we
			// surface every other read error.
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				if !os.IsNotExist(err) {
					return nil, fmt.Errorf("config: read %s: %w", path, err)
				}
			}
		}
	}

	// Seed defaults before Unmarshal so omitted keys still get values.
	dflt := Defaults()
	v.SetDefault("http.host", dflt.HTTP.Host)
	v.SetDefault("http.port", dflt.HTTP.Port)
	v.SetDefault("log.level", string(dflt.Log.Level))
	v.SetDefault("tracing.enabled", dflt.Tracing.Enabled)
	v.SetDefault("tracing.sample_ratio", dflt.Tracing.SampleRatio)
	v.SetDefault("tracing.service_name", dflt.Tracing.ServiceName)

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	if cfg.Storage.Path == "" {
		cfg.Storage.Path = DefaultDBPath(*cfg)
	}
	if cfg.Log.File == "" {
		cfg.Log.File = DefaultLogPath(*cfg)
	}
	if cfg.Nodes == nil {
		// Never leave a nil slice behind: callers iterate without a nil
		// check, and "no nodes" must be indistinguishable from "empty".
		cfg.Nodes = []NodeConfig{}
	}
	// An illegal node entry fails loading: silently skipping it would start
	// the process with fewer nodes than the operator asked for (design D7).
	if err := cfg.ValidateNodes(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// DefaultConfigPath returns the per-user configuration path. On Linux/macOS
// we honour XDG_CONFIG_HOME (falling back to $HOME/.config); on Windows we
// use %AppData%\gb28181-simulator\config.yaml.
func DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("AppData")
		if base == "" {
			base = filepath.Join(os.Getenv("HomeDrive"), os.Getenv("HomePath"), "AppData", "Roaming")
		}
		return filepath.Join(base, "gb28181-simulator", "config.yaml")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "gb28181-simulator", "config.yaml")
}

// DefaultDBPath returns the per-user SQLite file location. Honours
// XDG_STATE_HOME on Linux/macOS and %AppData% on Windows.
func DefaultDBPath(_ Config) string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("AppData")
		if base == "" {
			base = filepath.Join(os.Getenv("HomeDrive"), os.Getenv("HomePath"), "AppData", "Roaming")
		}
		return filepath.Join(base, "gb28181-simulator", "data.db")
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "gb28181-simulator", "data.db")
}

// DefaultLogPath returns the per-user log file location. Symmetric to
// DefaultDBPath but uses XDG_STATE_HOME/logs.
func DefaultLogPath(_ Config) string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("AppData")
		if base == "" {
			base = filepath.Join(os.Getenv("HomeDrive"), os.Getenv("HomePath"), "AppData", "Roaming")
		}
		return filepath.Join(base, "gb28181-simulator", "logs", "app.log")
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "gb28181-simulator", "logs", "app.log")
}

// EnsureDirs creates the parent directories of every configured file path.
// main calls this after Load so first-run users get a writable tree.
func EnsureDirs(cfg *Config) error {
	for _, p := range []string{filepath.Dir(cfg.Storage.Path), filepath.Dir(cfg.Log.File)} {
		if p == "" || p == "." {
			continue
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("config: mkdir %s: %w", p, err)
		}
	}
	return nil
}

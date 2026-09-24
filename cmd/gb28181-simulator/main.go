// Command gb28181-simulator is the entrypoint. Change 1 implements only the
// skeleton: load YAML config, initialise the logger, open SQLite, start the
// HTTP/WS server, and serve the embedded Dashboard.
//
// GB28181 protocol logic (SIP, MANSCDP+, PS/RTP, Node, etc.) is delivered in
// Change 2+.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	sipauth "github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	"github.com/your-org/gb28181-simulator/internal/platform/clock"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/tracing"
	"github.com/your-org/gb28181-simulator/internal/platform/servicectx"
	"github.com/your-org/gb28181-simulator/internal/sipprobe"
	"github.com/your-org/gb28181-simulator/internal/storage"
)

// Version metadata. Overridden at link time via -ldflags "-X main.version=..."
// for release builds; tests can poke them directly.
var (
	version   = "0.1.0-dev"
	commit    = "unknown"
	builtAt   = "unknown"
	startedAt = time.Now()
)

func main() {
	// Subcommand routing. `gb28181-simulator sipprobe ...` exposes the Change 2
	// diagnostic probe from this binary as well; scripts/smoke-sip.sh relies on
	// that invocation. Everything after the subcommand is handled by
	// sipprobe.RunCLI, which wires its own ServiceContext, so this stays a
	// pure dispatch and run() never sees these args.
	if len(os.Args) > 1 && os.Args[1] == "sipprobe" {
		os.Exit(sipprobe.RunCLI(os.Args[2:], sipprobe.Version{
			Version: version,
			Commit:  commit,
			BuiltAt: builtAt,
		}))
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ServiceContext keys for the main application. Storage uses the shared
// servicectx.StorageKey so the container stays free of adapter types
// (design D10).
var (
	configKey      = servicectx.NewKey[*platformconfig.Config]("config")
	loggerKey      = servicectx.NewKey[*logging.Hub]("logger")
	tracingKey     = servicectx.NewKey[*tracing.Provider]("tracing")
	nodeServiceKey = servicectx.NewKey[*app.NodeService]("node-service")
	serverKey      = servicectx.NewKey[*httpapi.Server]("server")
)

// bindTransport is the listener factory handed to the node lifecycle. It is
// the only place that knows how a signalling socket is built, so the domain
// and app layers stay transport-agnostic.
func bindTransport(addr string) (port.SIPTransport, error) {
	tr, err := siptransport.New(addr)
	if err != nil {
		return nil, err
	}
	return siptransport.NewPortAdapter(tr), nil
}

func run() error {
	cfgPath := flag.String("config", os.Getenv("GB28181_SIMULATOR_CONFIG"), "path to YAML config (defaults to XDG/AppData path)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("gb28181-simulator %s (commit %s, built %s)\n", version, commit, builtAt)
		return nil
	}

	// Build the dependency graph using ServiceContext container.
	// Note: servicectx.Provide build funcs cannot reference the container
	// itself (func() (any, error) signature), so each provider is
	// self-contained. Dependencies are resolved at Build time in declaration
	// order; MustGet is used after Build to retrieve instances.
	// Captured by the providers below: the container's build funcs cannot
	// reference the container itself, so cross-provider wiring goes through
	// these variables resolved in Provide order.
	var cfg *platformconfig.Config
	var nodeSvc *app.NodeService
	// Background work (keepalive, renewal) must not be tied to a request,
	// so it runs on the process context and is stopped explicitly on the
	// way out.
	processCtx := context.Background()
	var nodeKeeper *app.Keeper

	c := servicectx.NewContainer().
		Provide(configKey, func() (any, error) {
			var err error
			cfg, err = platformconfig.Load(*cfgPath)
			if err != nil {
				return nil, fmt.Errorf("load config: %w", err)
			}
			if err := platformconfig.EnsureDirs(cfg); err != nil {
				return nil, fmt.Errorf("ensure dirs: %w", err)
			}
			// The logger does not exist yet at this point, so this first line
			// goes to stdout directly. It establishes the "config loaded"
			// start of the spec 5.2 start-up log order.
			fmt.Printf("config loaded path=%s\n", *cfgPath)
			return cfg, nil
		}).
		Provide(loggerKey, func() (any, error) {
			if err := logging.Init(logging.Options{
				Level:      logging.ParseLevel(string(cfg.Log.Level)),
				File:       cfg.Log.File,
				AddSource:  cfg.Log.AddSource,
				RedactKeys: cfg.Log.RedactKeys,
			}); err != nil {
				return nil, fmt.Errorf("logger init: %w", err)
			}
			logging.L().Info("logger initialized", "level", string(cfg.Log.Level))
			return logging.DefaultHub(), nil
		}).
		Provide(tracingKey, func() (any, error) {
			trCfg := tracing.Config{
				Enabled:      cfg.Tracing.Enabled,
				SampleRatio:  cfg.Tracing.SampleRatio,
				OTLPEndpoint: cfg.Tracing.OTLPEndpoint,
				ServiceName:  cfg.Tracing.ServiceName,
			}
			ctx := context.Background()
			p, err := tracing.New(ctx, trCfg)
			if err != nil {
				return nil, fmt.Errorf("tracing init: %w", err)
			}
			logging.L().Info("tracing provider started",
				"enabled", trCfg.Enabled,
				"sample_ratio", trCfg.SampleRatio,
				"otlp_endpoint", trCfg.OTLPEndpoint)
			return p, nil
		}).
		Provide(servicectx.StorageKey, func() (any, error) {
			st, err := storage.NewStore(cfg.Storage.Path)
			if err != nil {
				return nil, fmt.Errorf("storage bootstrap: %w", err)
			}
			return st, nil
		}).
		Provide(nodeServiceKey, func() (any, error) {
			// The registry doubles as the advancer: it owns the nodes and
			// enforces the transition table when the status changes.
			registry := nodereg.New()
			lifecycle := nodereg.NewLifecycle(registry, bindTransport)
			svc, err := app.NewNodeService(registry, lifecycle, registry, bindTransport, clock.Real())
			if err != nil {
				return nil, err
			}
			// Device registration: the use case lives in app, the Digest
			// client behind it is an adapter, and the app sees only the
			// domain port.
			authorizer, err := sipauth.NewAuthorizerAdapter(sipauth.NewAuthorizer(nil))
			if err != nil {
				return nil, fmt.Errorf("authorizer: %w", err)
			}
			registrar, err := app.NewRegistrar(authorizer, clock.Real(), logging.L())
			if err != nil {
				return nil, fmt.Errorf("registrar: %w", err)
			}
			if _, err := svc.WithRegistrar(registrar); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}
			// Keepalive and renewal: same split — the notify body comes
			// from an adapter, the scheduling lives in app. The keeper is
			// provided under its own key so the container closes it on
			// shutdown and no heartbeat outlives the process.
			keeper, err := app.NewKeeper(processCtx, registry, lifecycle, registrar,
				manscdp.NewKeepaliveCodec(), clock.Real(), clock.RealTicker(), logging.L())
			if err != nil {
				return nil, fmt.Errorf("keeper: %w", err)
			}
			if _, err := svc.WithKeeper(keeper); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}
			nodeKeeper = keeper
			// Register every configured node; starting them is an explicit
			// operation (design D9), so an empty list costs nothing.
			for i, nc := range cfg.Nodes {
				profile, err := model.NewNodeProfile(nc.ID, nc.Addr, nc.Domain, nc.Vendor)
				if err != nil {
					return nil, fmt.Errorf("node[%d]: %w", i, err)
				}
				// A `registration:` section is what makes a node register
				// when it is started. Without one the node keeps its
				// pre-registration behaviour: bind the listener and stop
				// at `registering`.
				if nc.Registration != nil {
					r := nc.Registration
					reg, regErr := model.NewRegistration(model.RegistrationParams{
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
					})
					if regErr != nil {
						return nil, fmt.Errorf("node[%d].registration: %w", i, regErr)
					}
					profile, err = profile.WithRegistration(reg)
					if err != nil {
						return nil, fmt.Errorf("node[%d].registration: %w", i, err)
					}
				}
				if _, err := svc.Create(context.Background(), profile); err != nil {
					return nil, fmt.Errorf("register node[%d]: %w", i, err)
				}
			}
			if len(cfg.Nodes) > 0 {
				logging.L().Info("nodes registered", "count", len(cfg.Nodes))
			}
			nodeSvc = svc
			return svc, nil
		}).
		Provide(serverKey, func() (any, error) {
			hub := logging.DefaultHub()
			return httpapi.NewServer(*cfg, hub, httpapi.Version{
				Version: version,
				Commit:  commit,
				BuiltAt: builtAt,
			}, nodeSvc), nil
		})

	cancel, err := c.Build()
	if err != nil {
		return fmt.Errorf("build container: %w", err)
	}
	defer cancel.Close()
	// Registered after the container's own shutdown, so it runs first:
	// heartbeats stop before anything they talk through is torn down.
	defer func() {
		if nodeKeeper != nil {
			if err := nodeKeeper.Close(); err != nil {
				logging.L().Warn("keeper shutdown", "error", err.Error())
			}
		}
	}()

	// Retrieve built services via MustGet.
	_ = servicectx.MustGet[*logging.Hub](c, loggerKey)
	traceProvider := servicectx.MustGet[*tracing.Provider](c, tracingKey)
	// Storage is retrieved through its domain port: the container no longer
	// knows the concrete adapter type (design D10).
	store := servicectx.MustGet[port.Storage](c, servicectx.StorageKey)
	if store == nil {
		return fmt.Errorf("storage provider produced no store")
	}
	nodeService := servicectx.MustGet[*app.NodeService](c, nodeServiceKey)

	// Emit one start-up span so the stdout exporter has something to write.
	// With tracing.enabled=false this is a no-op tracer and prints nothing.
	_, startupSpan := traceProvider.Tracer("cmd/gb28181-simulator").Start(context.Background(), "startup")
	startupSpan.End()
	server := servicectx.MustGet[*httpapi.Server](c, serverKey)

	// Log startup with config details.
	logging.L().Info("starting gb28181-simulator",
		"version", version,
		"commit", commit,
		"config_path", *cfgPath,
		"http_addr", cfg.HTTP.Addr(),
		"db_path", cfg.Storage.Path,
		"log_file", cfg.Log.File,
		"nodes", len(nodeService.List(context.Background())),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start server in a goroutine so we can await signals in main.
	errCh := make(chan error, 1)
	go func() {
		logging.L().Info("http listening", "addr", cfg.HTTP.Addr())
		if err := server.Run(cfg.HTTP.Addr()); err != nil && err.Error() != "shutting down server" {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logging.L().Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	}

	shutdownCtx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	// Release every node listener on the way out so the ports are free for
	// the next start.
	for _, n := range nodeService.List(shutdownCtx) {
		if err := nodeService.Stop(shutdownCtx, n.ID()); err != nil {
			logging.L().Warn("node stop failed", "id", n.ID().String(), "error", err)
		}
	}
	return nil
}

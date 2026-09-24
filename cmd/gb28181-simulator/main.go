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

	"github.com/your-org/gb28181-simulator/internal/platform/servicectx"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/tracing"
	"github.com/your-org/gb28181-simulator/internal/sipprobe"
	"github.com/your-org/gb28181-simulator/internal/storage"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
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

// ServiceContext keys for the main application.
var (
	configKey  = servicectx.NewKey[*platformconfig.Config]("config")
	loggerKey  = servicectx.NewKey[*logging.Hub]("logger")
	tracingKey = servicectx.NewKey[*tracing.Provider]("tracing")
	storageKey = servicectx.NewKey[*storage.Store]("storage")
	serverKey  = servicectx.NewKey[*httpapi.Server]("server")
)

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
	var cfg *platformconfig.Config
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
		Provide(storageKey, func() (any, error) {
			st, err := storage.NewStore(cfg.Storage.Path)
			if err != nil {
				return nil, fmt.Errorf("storage bootstrap: %w", err)
			}
			return st, nil
		}).
		Provide(serverKey, func() (any, error) {
			hub := logging.DefaultHub()
			return httpapi.NewServer(*cfg, hub, httpapi.Version{
				Version: version,
				Commit:  commit,
				BuiltAt: builtAt,
			}), nil
		})

	cancel, err := c.Build()
	if err != nil {
		return fmt.Errorf("build container: %w", err)
	}
	defer cancel.Close()

	// Retrieve built services via MustGet.
	_ = servicectx.MustGet[*logging.Hub](c, loggerKey)
	traceProvider := servicectx.MustGet[*tracing.Provider](c, tracingKey)
	_ = servicectx.MustGet[*storage.Store](c, storageKey)

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
	return nil
}
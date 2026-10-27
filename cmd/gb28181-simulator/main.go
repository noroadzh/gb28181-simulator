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

	"github.com/your-org/gb28181-simulator/internal/adapter/accountsql"
	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
	sipauth "github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/adapter/capture"
	"github.com/your-org/gb28181-simulator/internal/adapter/cascade"
	"github.com/your-org/gb28181-simulator/internal/adapter/channelsql"
	"github.com/your-org/gb28181-simulator/internal/adapter/devicereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	mediastatus "github.com/your-org/gb28181-simulator/internal/adapter/media_status"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodesql"
	"github.com/your-org/gb28181-simulator/internal/adapter/playback"
	"github.com/your-org/gb28181-simulator/internal/adapter/recording"
	"github.com/your-org/gb28181-simulator/internal/adapter/scenario"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/adapter/subscribe"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/app/streaming"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	"github.com/your-org/gb28181-simulator/internal/platform/clock"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/events"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/tracing"
	"github.com/your-org/gb28181-simulator/internal/platform/servicectx"
	"github.com/your-org/gb28181-simulator/internal/sipprobe"
	"github.com/your-org/gb28181-simulator/internal/storage"
)

// Version metadata. Overridden at link time via -ldflags "-X main.version=..."
// for release builds; tests can poke them directly.
var (
	version = "0.1.0-dev"
	commit  = "unknown"
	builtAt = "unknown"
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
	recordingKey   = servicectx.NewKey[*recording.Store]("recording-store")
	scenarioKey    = servicectx.NewKey[port.ScenarioRunner]("scenario-runner")
	serverKey      = servicectx.NewKey[*httpapi.Server]("server")
)

// bindTransport is the listener factory handed to the node lifecycle. It is
// the only place that knows how a signalling socket is built, so the domain
// and app layers stay transport-agnostic. nodeID tags the socket so capture
// events (Change 13) can be attributed to the owning node.
func bindTransport(addr string, nodeID model.NodeID) (port.SIPTransport, error) {
	tr, err := siptransport.New(addr, siptransport.WithNodeID(nodeID.String()))
	if err != nil {
		return nil, err
	}
	return siptransport.NewPortAdapter(tr), nil
}

// applyPlatformSection turns one node's `platform:` section into the
// serving description carried on the profile plus the accounts the platform
// accepts. It is separate from the loop that calls it so the wiring — where
// a configured password becomes a credential — can be tested without
// starting a process.
// startupAccountStore is the slice of AccountAdmin the applyPlatformSection
// startup path needs: both Seed (YAML idempotent insert) and the management
// methods the HTTP plane uses later.
type startupAccountStore interface {
	port.AccountAdmin
	port.AccountSeeder
}

func applyPlatformSection(
	profile model.NodeProfile,
	pc *platformconfig.NodePlatformConfig,
	accounts startupAccountStore,
) (model.NodeProfile, error) {
	policy, err := model.NewExpiresPolicy(pc.Min, pc.Default, pc.Max)
	if err != nil {
		return model.NodeProfile{}, err
	}
	// The configured realm wins over the node's home domain: a platform
	// may serve a different administrative domain than the one it lives
	// in, and the digest credentials below must challenge in that realm.
	realm := profile.Domain()
	if pc.Realm != nil {
		realm = *pc.Realm
		if realm == "" {
			return model.NodeProfile{}, fmt.Errorf("platform.realm: empty realm")
		}
	}
	serving, err := model.NewPlatformServingWithNoAuth(realm, policy, pc.AllowNoAuth)
	if err != nil {
		return model.NodeProfile{}, err
	}
	profile, err = profile.WithPlatformServing(serving)
	if err != nil {
		return model.NodeProfile{}, err
	}
	if err := seedPlatformAccounts(profile, pc, accounts); err != nil {
		return model.NodeProfile{}, err
	}
	return profile, nil
}

// seedPlatformAccounts validates and inserts one platform's account list.
// Seed ignores duplicates, so the startup loop calls it on every boot for
// every YAML-declared platform — including nodes restored from the store
// rather than built from YAML.
func seedPlatformAccounts(
	profile model.NodeProfile,
	pc *platformconfig.NodePlatformConfig,
	accounts startupAccountStore,
) error {
	// Digest credentials bind to the realm the platform challenges in,
	// which may differ from the node's home domain; fall back to the
	// domain only when the profile carries no serving config at all.
	realm := profile.Domain()
	if serving, ok := profile.PlatformServing(); ok {
		realm = serving.Realm()
	}
	for j, acc := range pc.Accounts {
		// The store keeps the secret out of sight, so an empty one has
		// to be caught here: a platform that accepted it would be
		// accepting everybody — unless the operator opted the platform
		// into no-auth test/intranet mode via pc.AllowNoAuth.
		if acc.Password == "" && !pc.AllowNoAuth {
			return fmt.Errorf("accounts[%d]: empty password", j)
		}
		cred, err := model.NewCredentials(acc.Username, realm, acc.Password)
		if err != nil {
			return fmt.Errorf("accounts[%d]: %w", j, err)
		}
		if pc.AllowNoAuth {
			cred = cred.WithNoAuth()
		}
		if err := accounts.Seed(profile.ID(), cred); err != nil {
			return fmt.Errorf("accounts[%d]: %w", j, err)
		}
	}
	return nil
}

// seedYAMLAccounts re-seeds one YAML-declared node's platform accounts.
// On the store path the node's profile came from the DB, so this is where
// the YAML platform section still has an effect: its accounts are inserted
// idempotently beside the restored node. An account that has been dropped
// from YAML is deliberately not deleted — only the HTTP API removes
// accounts, so a config edit cannot silently revoke credentials.
func seedYAMLAccounts(
	profile model.NodeProfile,
	nc platformconfig.NodeConfig,
	accounts startupAccountStore,
) error {
	if nc.Platform == nil {
		return nil
	}
	return seedPlatformAccounts(profile, nc.Platform, accounts)
}

// importYAMLNodes copies YAML node definitions into the node store — the
// one-way handoff that makes the DB the single source of truth. It runs
// only when the store is empty, and after it the YAML section is dead
// weight that later boots ignore. Accounts are deliberately not part of
// the import: they live in their own table and are seeded by the per-node
// loop (seedPlatformAccounts), so a bad credential fails the boot with a
// clear per-account error instead of stranding half a node in the DB.
//
// The cascade graph is not imported either: YAML expresses topology only
// through registration targets, and the stored CascadeParent/Children
// columns are owned by the HTTP API from here on.
func importYAMLNodes(
	nodeStore *nodesql.Store,
	nodes []platformconfig.NodeConfig,
) error {
	ctx := context.Background()
	for i := range nodes {
		nc := nodes[i]
		id, err := model.ParseNodeID(nc.ID)
		if err != nil {
			return fmt.Errorf("node[%d].id: %w", i, err)
		}
		kind, err := model.ParseNodeKind(nc.Kind)
		if err != nil {
			return fmt.Errorf("node[%d].kind: %w", i, err)
		}
		// Kind from YAML must match what the ID's type code implies, or
		// it would be stored and then silently ignored (design D1).
		if err := model.CheckNodeKindConsistency(id, kind); err != nil {
			return fmt.Errorf("node[%d]: %w", i, err)
		}
		sn := model.StoredNode{
			ID:     id,
			Kind:   kind,
			Domain: nc.Domain,
			Addr:   nc.Addr,
			Vendor: nc.Vendor,
		}
		if nc.Registration != nil {
			r := nc.Registration
			reg, regErr := model.NewRegistration(model.RegistrationParams{
				Server:               r.Server,
				ServerID:             r.ServerID,
				Username:             r.Username,
				Password:             r.Password,
				AllowNoAuth:          r.AllowNoAuth,
				GBVersion:            r.GBVersion,
				Expires:              r.Expires,
				Timeout:              r.Timeout,
				Transport:            r.Transport,
				HeartbeatInterval:    r.HeartbeatInterval,
				HeartbeatTimeout:     r.HeartbeatTimeout,
				HeartbeatMaxFailures: r.HeartbeatMaxFailures,
			})
			if regErr != nil {
				return fmt.Errorf("node[%d].registration: %w", i, regErr)
			}
			sn.Registration = reg
			sn.HasRegistration = true
		}
		if nc.Platform != nil {
			pc := nc.Platform
			policy, polErr := model.NewExpiresPolicy(pc.Min, pc.Default, pc.Max)
			if polErr != nil {
				return fmt.Errorf("node[%d].platform: %w", i, polErr)
			}
			realm := nc.Domain
			if pc.Realm != nil {
				realm = *pc.Realm
			}
			serving, svErr := model.NewPlatformServingWithNoAuth(realm, policy, pc.AllowNoAuth)
			if svErr != nil {
				return fmt.Errorf("node[%d].platform: %w", i, svErr)
			}
			sn.Platform = serving
			sn.HasPlatform = true
			sn.CatalogPollIntervalMs = int(pc.CatalogPollInterval / time.Millisecond)
		}
		if err := nodeStore.CreateNode(ctx, sn); err != nil {
			return fmt.Errorf("node[%d] (%s): %w", i, nc.ID, err)
		}
	}
	return nil
}

// syncNodeStoreFromYAML performs the one-way handoff from the YAML section
// to the DB-backed store: node definitions come from the store when it has
// anything in it, and from YAML only on a cold database. That one-way
// handoff is what makes the DB the single source of truth after the first
// boot without stranding an operator who still keeps nodes in YAML:
// importing once, then ignoring it.
//
// The import below only fires when the table is empty, so deleting a node
// at runtime does not drag its YAML definition back on the next restart.
// A nil store means "YAML only" legacy mode and does nothing here.
func syncNodeStoreFromYAML(
	ctx context.Context,
	nodeStore *nodesql.Store,
	nodes []platformconfig.NodeConfig,
) error {
	if nodeStore == nil || len(nodes) == 0 {
		return nil
	}
	persisted, err := nodeStore.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("count persisted nodes: %w", err)
	}
	if len(persisted) == 0 {
		if err := importYAMLNodes(nodeStore, nodes); err != nil {
			return fmt.Errorf("import yaml nodes: %w", err)
		}
		logging.L().Info("imported node definitions from yaml into storage",
			"count", len(nodes))
		return nil
	}
	// Warn, not info: an operator who edits a node in the YAML and
	// restarts expects the edit to land. It will not — the store now owns
	// the definitions — so this line is the only hint they get before the
	// API or the dashboard becomes the only way to change a node.
	logging.L().Warn("ignoring deprecated nodes section in config: storage already holds node definitions",
		"storage_nodes", len(persisted), "yaml_nodes", len(nodes))
	return nil
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
	// capStore holds the per-node capture store when the `capture:` section
	// enables it; it is wired into the node service after the container is
	// built so the HTTP capture endpoints can read it.
	var capStore port.CaptureStore
	// eventHub carries node status transitions from the node service to the
	// WebSocket dashboard stream (/v1/nodes/stream). Created in the node
	// service provider, attached to the HTTP server in its own provider, and
	// closed with the container.
	var eventHub *events.Hub
	// Background work (keepalive, renewal) must not be tied to a request,
	// so it runs on the process context and is stopped explicitly on the
	// way out.
	processCtx := context.Background()
	var nodeKeeper *app.Keeper
	// Serving (accepting downstream registrations) is background work too,
	// and it is closed the same way.
	var nodeAcceptor *app.Acceptor
	// MediaService is the composition root for the four adapter media
	// sources and the PS/RTP pipeline. It lives for the process lifetime
	// and is closed after every node is stopped on shutdown.
	var mediaService *app.MediaService
	// faultStore is the shared fault profile store produced by the node
	// service provider; the scenario engine's inject-fault steps reuse it so
	// injected faults behave exactly like HTTP-installed ones.
	var faultStore *app.FaultStoreAdapter
	// accountAdmin is the sqlite-backed platform-account store: YAML seeds
	// it once, the HTTP management plane mutates it at runtime, and the
	// scenario engine reuses it for create-node steps.
	var accountAdmin *accountsql.Store
	// scenarioRunner is the Change 15 scenario engine: a store+executor
	// runner decorated by the app-level ScenarioService (last-run tracking).
	var scenarioRunner port.ScenarioRunner

	var c *servicectx.Container
	c = servicectx.NewContainer().
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
			modules := map[string]logging.Level{}
			for path, lvl := range cfg.Log.Modules {
				modules[path] = logging.ParseLevel(string(lvl))
			}
			if err := logging.Init(logging.Options{
				Level:      logging.ParseLevel(string(cfg.Log.Level)),
				File:       cfg.Log.File,
				AddSource:  cfg.Log.AddSource,
				RedactKeys: cfg.Log.RedactKeys,
				Modules:    modules,
			}); err != nil {
				return nil, fmt.Errorf("logger init: %w", err)
			}
			logging.L().Info("logger initialized", "level", string(cfg.Log.Level),
				"module_overrides", len(modules))
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
		Provide(recordingKey, func() (any, error) {
			if !cfg.Recording.Enabled {
				logging.L().Info("recording library disabled")
				return (*recording.Store)(nil), nil
			}
			dir := cfg.Recording.DirOrDefault(cfg.Storage.Path)
			st, err := recording.New(dir, cfg.Recording.SegmentDuration())
			if err != nil {
				return nil, fmt.Errorf("recording store: %w", err)
			}
			logging.L().Info("recording library ready",
				"dir", st.Dir(), "segment", cfg.Recording.SegmentDuration().String())
			return st, nil
		}).
		Provide(nodeServiceKey, func() (any, error) {
			// The registry doubles as the advancer: it owns the nodes and
			// enforces the transition table when the status changes.
			registry := nodereg.New()
			// Cascade forwarding: one handler whose topology view the
			// registry keeps in sync on every node join and leave. Both the
			// registry (cycle check at registration) and the acceptor
			// (outbound header injection) see the same handler.
			cascadeHandler := cascade.New(nil, logging.L())
			registry.WithCascadeHandler(cascadeHandler)
			lifecycle := nodereg.NewLifecycle(registry, bindTransport)
			svc, err := app.NewNodeService(registry, lifecycle, registry, bindTransport, clock.Real())
			if err != nil {
				return nil, err
			}
			// Domain events: status transitions flow from NodeService through
			// this Hub to the WebSocket dashboard stream. NewHub is drop-on-slow
			// by design (Publish never blocks the SIP path), so a stalled
			// subscriber cannot wedge the state machine that publishes here.
			eventHub = events.NewHub(0)
			if _, err := svc.WithEvents(eventHub); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}
			// Node lifecycle reporting — which half of a two-halved node
			// failed, what could not be undone on the way out — goes
			// through the process logger, so it is filtered and redacted
			// like every other line.
			if _, err := svc.WithLogger(logging.L()); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}
			// Device control (PTZ) renders its MANSCDP body through the same
			// codec the acceptor decodes with, so the two sides cannot drift.
			if _, err := svc.WithControlCodec(manscdp.NewMANSCDPCodec()); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}
			// Snapshots: the adapter draws the frames it can produce without
			// a video decoder; the service decides what a missing capability
			// means over HTTP.
			if _, err := svc.WithFrameRenderer(media.NewSyntheticRenderer()); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}
			// Device registration: the use case lives in app, the Digest
			// client behind it is an adapter, and the app sees only the
			// domain port.
			// GB 35114: one process-wide SM2 identity, loaded once and
			// attached to every digest exchange on both sides.
			gb35114Priv, gb35114Pub, err := loadGB35114Identity(cfg)
			if err != nil {
				return nil, err
			}
			authorizer, err := sipauth.NewAuthorizerAdapter(sipauth.NewAuthorizer(nil))
			if err != nil {
				return nil, fmt.Errorf("authorizer: %w", err)
			}
			registrar, err := app.NewRegistrar(authorizer, clock.Real(), logging.L())
			if err != nil {
				return nil, fmt.Errorf("registrar: %w", err)
			}
			if len(gb35114Priv) > 0 {
				registrar.WithGB35114(gb35114Priv, gb35114Pub)
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

			// Either platform kind — large or small: the same split again
			// — the online device table and the account store are
			// adapters, the UAS use case lives in app, and the app never
			// sees either concrete type.
			devices := devicereg.New()
			portSt := servicectx.MustGet[port.Storage](c, servicectx.StorageKey)
			rawSt, _ := portSt.(*storage.Store)
			if rawSt == nil {
				return nil, fmt.Errorf("storage provider produced no store")
			}
			accountSQL, err := accountsql.NewE(rawSt.DB())
			if err != nil {
				return nil, fmt.Errorf("account sqlite init: %w", err)
			}
			accountAdmin = accountSQL
			authenticator, err := sipauth.NewAuthenticatorAdapter(sipauth.NewResponder(nil))
			if err != nil {
				return nil, fmt.Errorf("authenticator: %w", err)
			}
			challengerOpts := make([]sipauth.ChallengerOption, 0, 2)
			if cfg.Security.GB35114.Enabled {
				challengerOpts = append(challengerOpts, sipauth.WithHashName("SM3"))
				if cfg.Security.GB35114.NoteIntegrity {
					challengerOpts = append(challengerOpts, sipauth.WithNoteHash())
				}
			}
			challenger, err := sipauth.NewChallengerAdapter(sipauth.NewChallenger(nil, challengerOpts...))
			if err != nil {
				return nil, fmt.Errorf("challenger: %w", err)
			}
			// The acceptor still needs a CredentialStore to answer auth
			// challenges. The sqlite adapter implements that interface, so we
			// pass it directly — the same store serves both real-time auth
			// and the management API.
			acceptor, err := app.NewAcceptor(processCtx, clock.Real(), challenger, authenticator,
				accountSQL, devices, manscdp.NewMANSCDPCodec(), clock.RealTicker(), logging.L())
			if err != nil {
				return nil, fmt.Errorf("acceptor: %w", err)
			}
			if len(gb35114Priv) > 0 {
				acceptor.WithGB35114(gb35114Priv, gb35114Pub)
			}
			// The acceptor is shared across every node in this process, so
			// it takes one catalog-poll interval for them all. Operators
			// set the value under the `registration:` section of any node
			// that runs a platform; the first non-zero value wins.
			if pollInterval := pickCatalogPollInterval(cfg.Nodes); pollInterval > 0 {
				acceptor.WithCatalogPollInterval(pollInterval)
				logging.L().Info("catalog polling enabled", "interval", pollInterval.String())
			}
			acceptor.WithCascadeHandler(cascadeHandler)
			if _, err := svc.WithAcceptor(acceptor); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}
			// The recording library writes what live pulls receive and
			// answers RecordInfo / playback INVITEs. It is optional: a
			// disabled library is a nil store and the capability reports
			// itself unsupported.
			if st, ok := servicectx.GetTyped(c, recordingKey); ok && st != nil {
				if _, err := svc.WithRecordings(st); err != nil {
					return nil, fmt.Errorf("recording library: %w", err)
				}
			}
			nodeAcceptor = acceptor
			// channelStore persists channel and media definitions for device
			// nodes; svc.WithChannelStore wires it into the node service so
			// runtime changes survive restarts.
			chSt := channelsql.New(rawSt.DB())
			if _, err := svc.WithChannelStore(chSt); err != nil {
				return nil, fmt.Errorf("channel store: %w", err)
			}

			// Runtime node definitions: the DB-backed store is the single
			// source of truth for node definitions, used by the CRUD API.
			// It is optional here — nil means "YAML only" (legacy mode),
			// which is what an operator gets when SQLite failed to open.
			var nodeStore *nodesql.Store
			if rawSt.DB() != nil {
				nodeStore = nodesql.New(rawSt.DB())
				if _, err := svc.WithNodeStore(nodeStore); err != nil {
					return nil, fmt.Errorf("node store: %w", err)
				}
				logging.L().Debug("runtime node storage enabled")
			}

			// Fault injection: one store shared by the acceptor (request
			// gate) and the node service (HTTP fault API).
			faults := app.NewFaultStore(registry, logging.L())
			acceptor.WithFaults(faults)
			if _, err := svc.WithFaults(faults); err != nil {
				return nil, fmt.Errorf("node service: %w", err)
			}

			// Wire platform-small supplementary capabilities onto the acceptor:
			// dialog tracking (INVITE/ACK/BYE), playback, subscription and
			// media-status ports. These are no-ops unless the node is a
			// platform-small that receives INVITE/SUBSCRIBE/OPTIONS.
			dialogMgr := app.NewDialogManager(app.DialogManagerConfig{})
			acceptor.WithDialogs(dialogMgr).
				WithPlayback(playback.NewPortAdapter()).
				WithSubscribe(subscribe.NewPortAdapter()).
				WithMediaStatus(mediastatus.NewPortAdapter()).
				WithNodeRegistry(registry)

				// Compose the four media source adapters behind a single factory
			// so the app layer never has to import internal/adapter directly.
			// The SSRC used here is a process-default; per-session SDP can
			// override it later when INVITE handling (#9) lands.
			const defaultSSRC uint32 = 0xABCDEF01

			// Media hot-loop error aggregator (openspec/changes/media-aggregator-integration):
			// shared across every PS/RTP packetizer created by the factories
			// below; a background ticker rolls the 60s window, and Flush() at
			// shutdown prevents losing the final burst.
			mediaAgg := media.NewErrorAggregator()
			defer mediaAgg.Flush()
			mediaAggCtx, mediaAggStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer mediaAggStop()
			go func() {
				ticker := time.NewTicker(media.DefaultWindow)
				defer ticker.Stop()
				for {
					select {
					case <-mediaAggCtx.Done():
						return
					case <-ticker.C:
						mediaAgg.Sweep()
					}
				}
			}()

			mediaService = app.NewMediaService(
				func(cfg model.MediaConfig) port.MediaSource {
					switch cfg.Kind {
					case model.SourceKindFile:
						return media.NewFileSource(cfg)
					case model.SourceKindRTSP:
						return media.NewRTSPSource(cfg)
					case model.SourceKindHLS:
						return media.NewHLSSource(cfg)
					case model.SourceKindSynthetic:
						return media.NewSyntheticSource(cfg)
					default:
						return nil
					}
				},
				func() port.PSPacketizer { return media.NewPSPacketizer(mediaAgg) },
				func(mtu int) port.RTPizer { return media.NewRTPizer(defaultSSRC, mtu, mediaAgg) },
				logging.L(),
			)
			// Wire the inbound (RTP-deizer + PS-depacketizer) factories so a
			// platform-small node can answer INVITE-based PS streams. Without
			// this, MediaService.NewInboundPipeline returns "inbound
			// factories not wired" and any incoming INVITE fails closed.
			mediaService.SetInboundFactories(
				func(ssrc uint32) port.RTPDeizer { return media.NewRTPDeizer(ssrc) },
				func() port.PSDepacketizer { return media.NewPSDepacketizer() },
			)
			// Attach the media service to the acceptor so handleInvite can
			// build an inbound pipeline for an INVITE/SDP the platform
			// answers. WithMediaService is optional and idempotent; we
			// ignore the return value because the method is fluent.
			acceptor.WithMediaService(mediaService)
			defer func() { _ = mediaService.Close() }()

			// Node definitions come from the store when the store has
			// anything in it, and from YAML only on a cold database. That
			// one-way handoff is what makes the DB the single source of
			// truth after the first boot without stranding an operator who
			// still keeps nodes in YAML: importing once, then ignoring it.
			//
			// The migration below only fires when the table is empty, so
			// deleting a node at runtime does not drag its YAML definition
			// back on the next restart.
			if err := syncNodeStoreFromYAML(processCtx, nodeStore, cfg.Nodes); err != nil {
				return nil, err
			}

			// Register every configured node; starting them is an explicit
			// operation (design D9), so an empty list costs nothing.
			//
			// With a store wired, definitions are loaded from it; without
			// one this loop stays the YAML-only path it has always been.
			var createdIDs []model.NodeID
			if nodeStore != nil {
				// LoadFromStore registers every persisted definition and
				// restores each node's channels, so the loop below must not
				// run again for the same rows.
				if err := svc.LoadFromStore(processCtx); err != nil {
					return nil, fmt.Errorf("load nodes from storage: %w", err)
				}
			}
			for i, nc := range cfg.Nodes {
				// Seed the profile's media slot from YAML when present.
				// platforms don't originate video, so we silently drop
				// media on platform kinds — the field is meaningful only
				// for devices. Validation in NodeMediaConfig rejects bad
				// kinds and missing paths, surfacing here as a config
				// load error (consistent with registration:).
				var seedMedia *model.MediaConfig
				if nc.Media != nil {
					converted, mcErr := model.ParseNodeMediaConfig(
						nc.Media.Kind,
						nc.Media.Path,
						nc.Media.Loop,
						nc.Media.MTU,
						nc.Media.SSRC,
						nc.Media.Clock,
						nc.Media.FPS,
					)
					if mcErr != nil {
						return nil, fmt.Errorf("node[%d].media: %w", i, mcErr)
					}
					if nc.Kind == "device" {
						seedMedia = &converted
					}
				}
				profile, err := model.NewNodeProfileWithMedia(nc.ID, nc.Addr, nc.Domain, nc.Vendor, seedMedia)
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
						Server:      r.Server,
						ServerID:    r.ServerID,
						Username:    r.Username,
						Password:    r.Password,
						AllowNoAuth: r.AllowNoAuth,
						GBVersion:   r.GBVersion,
						Expires:     r.Expires,
						Timeout:     r.Timeout,
						Transport:   r.Transport,

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
				// A `platform:` section is what makes a platform-large node
				// serve: the realm it challenges in, the lifetime window it
				// grants, and the accounts it accepts. Without one it still
				// serves, in its own domain with the default window.
				if nc.Platform != nil {
					profile, err = applyPlatformSection(profile, nc.Platform, accountAdmin)
					if err != nil {
						return nil, fmt.Errorf("node[%d].platform: %w", i, err)
					}
					logging.L().Info("platform accounts loaded", "node", profile.ID().String(),
						"accounts", len(nc.Platform.Accounts))
				}
				if nodeStore != nil {
					// Already registered by LoadFromStore above (and, on a
					// cold database, already persisted by the import).
					// Seeding accounts still has to happen: accounts live
					// beside the node row rather than inside it, and the
					// import path does not touch them.
					if err := seedYAMLAccounts(profile, nc, accountAdmin); err != nil {
						return nil, fmt.Errorf("node[%d].accounts: %w", i, err)
					}
					continue
				}
				if _, err := svc.Create(context.Background(), profile); err != nil {
					return nil, fmt.Errorf("register node[%d]: %w", i, err)
				}
				createdIDs = append(createdIDs, profile.ID())
			}
			if nodeStore == nil && len(cfg.Nodes) > 0 {
				logging.L().Info("nodes registered", "count", len(cfg.Nodes))
			}
			// Restore runtime-created channels and media definitions that
			// outlive the process. YAML accounts are re-seeded idempotently
			// by applyPlatformSection above (AddAccount ignores duplicates).
			for _, id := range createdIDs {
				if err := svc.RestorePersistedState(context.Background(), id); err != nil {
					return nil, fmt.Errorf("restore persisted state for node %s: %w", id.String(), err)
				}
			}
			nodeSvc = svc
			faultStore = faults

			// auto_start: bring the whole topology up right after the
			// persisted state is restored. One failed node is a warning,
			// never a reason to skip the rest or fail the process — the
			// operator retries it from the Web UI (design D3).
			if cfg.AutoStart {
				bg := context.Background()
				for _, n := range svc.List(bg) {
					if err := svc.Start(bg, n.ID()); err != nil {
						logging.L().Warn("auto-start failed, node left for manual retry",
							"node_id", n.ID().String(), "error", err.Error())
						continue
					}
				}
			}

			return svc, nil
		}).
		Provide(scenarioKey, func() (any, error) {
			store := scenario.NewStore(logging.L())
			if err := store.LoadEmbedded(); err != nil {
				return nil, fmt.Errorf("scenario store: %w", err)
			}
			if cfg.Scenario.Dir != "" {
				if err := store.LoadDir(cfg.Scenario.Dir); err != nil {
					return nil, fmt.Errorf("scenario store: %w", err)
				}
				logging.L().Info("scenario dir loaded", "dir", cfg.Scenario.Dir)
			}
			exec := scenario.NewExecutor()
			scenario.RegisterNodeSteps(exec, nodeSvc, accountAdmin)
			scenario.RegisterCommandSteps(exec, nodeSvc)
			scenario.RegisterWaitSteps(exec)
			scenario.RegisterExpectSteps(exec, nodeSvc)
			scenario.RegisterInjectFaultSteps(exec, faultStore)
			runner := scenario.NewRunner(processCtx, store, exec)
			scenarioRunner = app.NewScenarioService(runner)
			logging.L().Info("scenario engine ready", "packages", len(store.List()))
			return scenarioRunner, nil
		}).
		Provide(serverKey, func() (any, error) {
			hub := logging.DefaultHub()
			// Wire the HTTP-FLV streaming gateway: MediaServiceBridge adapts
			// *app.MediaService + NodeService to the gateway's supplier interface.
			var flvServer *httpapi.StreamingServer
			if mediaService != nil && nodeSvc != nil {
				// Live pulls turn RTP back into PS frames through the same
				// deizer the inbound pipelines use. Wiring it here, where
				// both the media service and the node service exist, keeps
				// the NodeService free of an adapter import.
				if _, err := nodeSvc.WithLiveDeizer(func(ssrc uint32) port.RTPDeizer {
					return media.NewRTPDeizer(ssrc)
				}); err != nil {
					return nil, err
				}
				bridge := streaming.NewMediaServiceBridge(mediaService, nodeSvc)
				gw := streaming.NewGateway(bridge, nodeSvc, logging.L()).
					WithLiveSource(nodeSvc).
					WithRecordings(nodeSvc)
				flvServer = httpapi.NewStreamingServer(gw, logging.L())
				logging.L().Info("streaming gateway ready")
			}
			return httpapi.NewServer(*cfg, hub, httpapi.Version{
				Version: version,
				Commit:  commit,
				BuiltAt: builtAt,
			}, nodeSvc, scenarioRunner, nodeSvc, accountAdmin, flvServer).WithEventsHub(eventHub), nil
		})

	cancel, err := c.Build()
	if err != nil {
		return fmt.Errorf("build container: %w", err)
	}

	// Capture wiring (design D4): when the `capture:` section enables it,
	// the process-global audit emitter becomes a bridge into the per-node
	// ring buffer store. With capture disabled the emitter stays the no-op
	// it was initialised to, so behaviour is byte-identical.
	if cfg.Capture.Enabled {
		capStore = capture.NewWithCapacity(cfg.Capture.Capacity, logging.L())
		audit.SetEmitter(capture.AuditBridge(capStore))
		logging.L().Info("capture enabled", "capacity", cfg.Capture.Capacity)
	}

	if nodeSvc != nil && capStore != nil {
		if _, err := nodeSvc.WithCaptures(capStore); err != nil {
			return fmt.Errorf("node service: %w", err)
		}
	}
	defer func() {
		if err := cancel.Close(); err != nil {
			logging.L().Warn("container close", "error", err.Error())
		}
	}()
	defer func() {
		// Close event hub after container (HTTP has already shut down).
		if eventHub != nil {
			eventHub.Close()
		}
	}()
	// Registered after the container's own shutdown, so it runs first:
	// heartbeats stop before anything they talk through is torn down.
	defer func() {
		if nodeKeeper != nil {
			if err := nodeKeeper.Close(); err != nil {
				logging.L().Warn("keeper shutdown", "error", err.Error())
			}
		}
		if nodeAcceptor != nil {
			if err := nodeAcceptor.Close(); err != nil {
				logging.L().Warn("acceptor shutdown", "error", err.Error())
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

// pickCatalogPollInterval scans all node configs and returns the first
// non-zero CatalogPollInterval it finds.  A value of 0 means "not set".
func pickCatalogPollInterval(nodes []platformconfig.NodeConfig) time.Duration {
	for _, nc := range nodes {
		if nc.Platform != nil && nc.Platform.CatalogPollInterval > 0 {
			return nc.Platform.CatalogPollInterval
		}
	}
	return 0
}

// loadGB35114Identity reads the process-wide SM2 key pair when GB 35114 is
// enabled, and validates it here so a bad key fails the boot instead of every
// later registration.
func loadGB35114Identity(cfg *platformconfig.Config) ([]byte, []byte, error) {
	priv, pub, err := cfg.Security.GB35114.Identity()
	if err != nil || priv == nil {
		return priv, pub, err
	}
	if _, err := sm.ParsePrivateKey(priv); err != nil {
		return nil, nil, fmt.Errorf("security.gb35114: private key: %w", err)
	}
	if _, err := sm.ParsePublicKey(pub); err != nil {
		return nil, nil, fmt.Errorf("security.gb35114: public key: %w", err)
	}
	return priv, pub, nil
}

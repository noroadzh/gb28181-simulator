# Architecture

`gb28181-simulator` uses a hexagonal (ports & adapters) layout under
`internal/`, introduced by Change 3 (`enterprise-skeleton`). The goal is a
clear dependency direction so Change 4+ (`node-abstraction` and beyond) can
add behaviour without the layers collapsing into each other.

## Layer diagram

```
                    ┌─────────────────────────────────────┐
                    │  cmd/            assembly entry     │
                    │  gb28181-simulator, sipprobe        │
                    └──────────────┬──────────────────────┘
                                   │ Provide / Build / MustGet
                    ┌──────────────▼──────────────────────┐
   inbound          │  internal/interface/                │   outbound
   HTTP / WS  ─────►│    http/    webui/                  │
                    └──────────────┬──────────────────────┘
                                   │
                    ┌──────────────▼──────────────────────┐
                    │  internal/app/                      │
                    │  use-case orchestration (reserved)  │
                    └──────────────┬──────────────────────┘
                                   │ depends on ports only
        ┌──────────────────────────▼──────────────────────────┐
        │  internal/domain/                                   │
        │    model/   immutable value objects                 │
        │    port/    the contracts (interfaces)               │
        └──────────────▲──────────────────────────────────────┘
                       │ implements
        ┌──────────────┴──────────────────────────────────────┐
        │  internal/adapter/                                  │
        │    sip/  sdp/  auth/  siptransport/  audit/         │
        └─────────────────────────────────────────────────────┘

        ┌─────────────────────────────────────────────────────┐
        │  internal/platform/   cross-cutting infrastructure  │
        │    config/  clock/  servicectx/                     │
        │    observability/{logging,tracing,audit}            │
        └─────────────────────────────────────────────────────┘
```

## Dependency rules

| From | May depend on |
|------|---------------|
| `domain/` | standard library only |
| `adapter/` | `domain/`, `platform/` |
| `interface/` | `domain/`, `platform/`, `adapter/` |
| `app/` | `domain/`, `platform/` |
| `platform/` | standard library + infrastructure libs (slog, viper, OTel) |
| anyone | `platform/` |

Nothing may depend *inward* on `adapter/` or `interface/` from `domain/`. The
rule is machine-checked:

```sh
# domain must have no non-stdlib dependency
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...
```

## Port contracts

All ports live in `internal/domain/port/` and operate on values from
`internal/domain/model/` — never on adapter types.

| Port | File | Methods | Adapter |
|------|------|---------|---------|
| `SIPTransport` | `transport.go` | `Send(ctx, Message, dst) error`, `Receive(ctx) (Message, string, error)`, `Close() error` | `internal/adapter/siptransport` |
| `SDPCodec` | `codec.go` | `Parse`, `Marshal` | `internal/adapter/sdp` |
| `Authenticator` | `auth.go` | `Verify` | `internal/adapter/auth` |
| `Challenger` | `auth.go` | `Challenge` | `internal/adapter/auth` |
| `AuditSink` | `audit.go` | `Emit` | `internal/adapter/audit` |
| `Clock` | `clock.go` | `Now()` | `internal/adapter/*` (via `platform/clock`) |
| `Storage` | `storage.go` | `CRUD`, `List`, `io.Closer` | `internal/storage` |

Each adapter carries a compile-time assertion so signature drift fails the
build rather than production:

```go
var _ port.SIPTransport = (*PortAdapter)(nil)
```

`internal/adapter/sip` is deliberately an exception: it is a message
build/parse helper library with no `Transport` type, so the `SIPTransport`
assertion lives in `internal/adapter/siptransport`.

## ServiceContext

`internal/platform/servicectx` is a hand-rolled, type-safe service registry.
It replaces `wire`/`fx` because the whole graph is 5-10 lines — a code
generator would cost more than it saves.

```go
type Container struct{ ... }

func NewContainer() *Container
func (c *Container) Provide(k Key, f func() (any, error)) *Container
func (c *Container) Build() (io.Closer, error)
func MustGet[T any](c *Container, k Key[T]) T
func NewKey[T any](name string) Key[T]
```

Semantics:

- **Declaration order = build order.** `Provide` records insertion order;
  `Build` instantiates in that order.
- **Reverse-order close.** The `io.Closer` returned by `Build` closes every
  `io.Closer` provider in reverse, so dependents shut down before their
  dependencies.
- **Roll-back on failure.** If a provider errors, `Build` closes everything it
  already constructed and returns the error.
- **Type safety.** `MustGet[*config.Config](c, configKey)` panics with the
  expected and actual types on mismatch, and with the key name when missing.
- **Duplicate keys** overwrite; the last registration wins.
- **No auto-wiring.** Providers must be declared in dependency order and
  resolve their dependencies explicitly.

### Usage

```go
configKey := servicectx.NewKey[*platformconfig.Config]("config")
loggerKey := servicectx.NewKey[*logging.Hub]("logger")

c := servicectx.NewContainer().
    Provide(configKey, func() (any, error) { return platformconfig.Load(path) }).
    Provide(loggerKey, func() (any, error) {
        // dependencies are read after Build, so providers stay self-contained
        return logging.DefaultHub(), nil
    })

cancel, err := c.Build()
if err != nil { return err }
defer cancel.Close()

hub := servicectx.MustGet[*logging.Hub](c, loggerKey)
```

Both `cmd/gb28181-simulator` and `cmd/sipprobe` follow this shape; neither
contains business logic.

## Observability

### Logging

`log/slog` via `internal/platform/observability/logging`. Levels: `trace`
(SIP wire bytes), `debug`, `info`, `warn`, `error`. Output goes to the
configured log file and to a WebSocket fan-out hub consumed by the dashboard.
Sensitive keys (`password`, `authorization`) are redacted.

### Tracing

`internal/platform/observability/tracing` wraps `sdktrace.TracerProvider`:

- **stdout exporter** is always registered when `tracing.enabled` is true,
  writing pretty-printed span JSON.
- **OTLP gRPC exporter** is added when `tracing.otlp_endpoint` is non-empty.
- **Sampling** via `sdktrace.TraceIDRatioBased(cfg.SampleRatio)`; the default
  is `0.01`, and `sample_ratio: 1.0` samples everything (development).
- **`Close(ctx)`** shuts down every processor and the provider itself, so the
  exporter goroutines do not leak.

Configuration:

```yaml
tracing:
  enabled: true
  sample_ratio: 1.0
  otlp_endpoint: ""     # host:port; empty = stdout only
  service_name: ""      # defaults to "gb28181-simulator"
```

`otel.SetTracerProvider` is called once at start-up, so domain code can obtain
a tracer with `otel.Tracer("...")` without importing the provider package.

## Known limitation

`internal/adapter/siptransport` does not expose the peer address of a received
message; gosip's `Messages()` channel does not carry it, and the transport
documents that callers needing it should parse the topmost `Via` header
(scheduled for Change 4+). Until then `sipprobe` in receive-only mode can
observe an inbound INVITE but cannot answer it, so a two-process
INVITE → 200 OK handshake is verified in-process by
`TestRun_AcceptAnyResponse` rather than across two binaries.

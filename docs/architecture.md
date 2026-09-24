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
                    │  use-case orchestration (NodeService)│
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
        │    nodereg/  node registry + lifecycle               │
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
| `NodeRegistry` | `node.go` | `Register`, `Unregister`, `Get`, `List`, `RecordRegistration` | `internal/adapter/nodereg` |
| `NodeLifecycle` | `node.go` | `Start`, `Stop`, `Status`, `Transport`, `Fail` | `internal/adapter/nodereg` |
| `NodeAdvancer` | `node.go` | `Advance(ctx, id, Status)` | `internal/adapter/nodereg` |
| `Authorizer` | `auth.go` | `Authorize(challenge, cred, method, uri)` | `internal/adapter/auth` |

`NodeAdvancer` is separate from `NodeLifecycle` on purpose: `Start`/`Stop`
are intent-revealing operations, while `Advance` is how an identity
implementation moves a node to `registered` or `online` once a protocol
exchange succeeds — the device registrar (Change 5) does exactly that.
`RecordRegistration` stores what a registration produced without touching
the status, and `Fail` is the mirror of a successful start: it faults the
node and releases its listener so nothing half-started is left behind.

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

## Node abstraction (Change 4)

Change 4 adds the first real behaviour on top of the skeleton: a node is an
identity plus a lifecycle, and several nodes can live in one process.

### Domain

`internal/domain/model` owns the value objects; nothing here knows about
sockets, HTTP or configuration.

- **`NodeID`** — a validated 20-digit GB/T 28181 code: 8 centre + 2 industry +
  3 type + 1 network + 6 serial. `ParseNodeID` rejects a wrong length, any
  non-digit, and a type-code segment it cannot map; it never guesses a
  default. `String()` returns the verbatim encoding.
- **`NodeKind`** — `device`, `platform-large`, `platform-small`, derived from
  the type-code segment so identity and kind can never disagree.
- **`NodeProfile`** — identity + signalling address + home domain + vendor.
  Immutable; changes go through `With...`.
- **`Node`** — a profile plus a `Status`.
- **`Status`** — `idle → registering → registered → online`, with `offline`
  and `fault` alongside. Transitions live in an explicit
  `map[Status]map[Status]bool` table; an illegal jump returns the sentinel
  `model.ErrIllegalTransition` and leaves the status unchanged. `fault` is a
  recoverable terminal state: only `idle` (reset) or `offline` (removal) may
  follow it.

### Adapter

`internal/adapter/nodereg` implements all three node ports over a
`map[string]*entry` guarded by `sync.RWMutex`:

- **One listener per node.** `Lifecycle` tracks each node's transport
  separately, so stopping one never touches another.
- **Address uniqueness at registration.** A second node claiming an address
  already held is rejected with an error naming the incumbent, rather than
  silently binding a random port.
- **Roll-back on failure.** `Start` advances the status first (atomic, so two
  concurrent starts cannot both bind) and, if the bind fails, rolls back to
  `idle` on a first start or `fault` on a restart. No half-started node is
  left behind.

### App

`internal/app.NodeService` is the use-case orchestration. It depends only on
domain ports — never on `internal/adapter/...`:

```sh
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/app/...
# → internal/domain/model, internal/domain/port
```

`Start` binds the listener and reaches `registering`; a `device` node that
carries a `Registration` then completes its registration before `Start`
returns — the registrar calls `MarkRegistered` and `MarkOnline`, or faults
the node when the platform refuses. A node without a registration stops at
`registering`, which is also where the platform identities stay until their
own stages land.

Staying online is the keeper's job (`internal/app/keeper.go`): one goroutine
per node, driven by an injected `port.Ticker`, sends a MANSCDP `Keepalive`
as a SIP `MESSAGE` every `heartbeat_interval` and re-registers at half the
granted lifetime (or a minute before it lapses, whichever is earlier). A run
of `heartbeat_max_failures` unanswered beats faults the node; a failed
renewal backs off (5s doubling to 60s) and leaves the node online, because
its registration is still valid. `Unregister` sends `Expires: 0` and only
then stops the goroutine and releases the listener — the reverse order would
aim a heartbeat at a closed transport. The keeper is closed by the
composition root on shutdown, and its root context is the process's, never a
request's.

### Composition

`cmd/gb28181-simulator` registers every `nodes:` entry at start-up and hands
the service to the HTTP layer. Node lifecycle events are *not* yet written to
`port.AuditSink`: the domain's only audit event, `model.WireEvent`, describes
wire-level transmission and cannot carry a status change, so that integration
is deferred to Change 13.

## Platform acceptance (Change 6, part one)

A `platform-large` node plays the UAS. `app.Acceptor` runs one goroutine per
platform node over that node's transport: `Receive` → dispatch REGISTER →
`Send` the answer back to the peer the request came from. Nothing in the
transport layer changed — `model.NewResponse` plus `PortAdapter.Send` already
covered responses.

```
device (UAC) ──REGISTER──▶ transport ──Receive(msg, peer)──▶ Acceptor
                                                              ├─ CredentialStore.Lookup(node, username)
                                                              ├─ Challenger.Challenge(realm)      → 401
                                                              ├─ Authenticator.Verify(msg, cred)  → 403 / 200
                                                              └─ DownstreamRegistry.Upsert/Remove
HTTP GET /v1/nodes/:id/devices ─▶ NodeService.Devices ─▶ Acceptor.Devices
```

- **Credentials never touch the node.** `model.PlatformServing` (realm,
  expires window) hangs off `NodeProfile`, but accounts live in
  `adapter/credstore` behind `port.CredentialStore`, partitioned by node, so
  a password can never reach HTTP, a log line or an error body.
- **The failure mode decides the answer.** `port.ErrMalformedCredentials`
  (unreadable header) may be re-challenged; `port.ErrInvalidCredentials`
  (parsed and wrong) must not be, per §L.2. The sentinels live in `port` so
  the use case can branch without importing the adapter that produced them.
- **Lifetime is policy, not arithmetic.** `model.ExpiresPolicy` clamps the
  request into `[min, max]`; `Expires: 0` is intercepted before it, because
  an explicit goodbye is not the same as asking for the default.
- **Serving stops before the port does.** `NodeService.Stop` calls
  `Acceptor.Stop` (which joins the goroutine and clears the table) and only
  then lets the lifecycle release the listener.

### MESSAGE dispatch and sweeping (Change 6, part two)

A registration is not the end of the conversation: a downstream that stays
online sends MANSCDP `MESSAGE`s, and a platform that never cleans up goes on
reporting devices that went silent. Both live in the same serving loop.

```
device ──MESSAGE(Keepalive)──▶ Acceptor ──MANSCDPCodec.DecodeNotify──▶ refresh
                                                                        └─ DownstreamRegistry.Upsert(WithSeen)
device ──MESSAGE(Catalog)────▶ Acceptor ──MarshalCatalog──▶ 200 OK + body
sweeper goroutine ──Ticker──▶ sweep: Drop rows with now ≥ ExpiresAt
```

- **Read is separate from write.** `port.MANSCDPCodec` pairs the notify a
  downstream sends with the catalog answer the platform gives back. It is
  deliberately not `port.KeepaliveCodec`, which only renders: forcing every
  device node to carry a parser it never calls would be a dependency nobody
  asked for.
- **An unreadable message is ignored, not answered.** A body produced by
  another vendor's device is data; a platform that answers an error the peer
  cannot interpret — or panics — can be switched off remotely. The loop
  decodes, and on failure logs at `debug` and goes on to the next message.
- **A heartbeat is not a re-registration.** `refresh` moves `last_seen_at`
  and leaves the granted lifetime and expiry alone, so a device cannot
  extend its registration by beating faster.
- **The catalog is per node.** It is built from that node's own online
  table, so two platforms sharing a process cannot see each other's
  downstreams.
- **Sweeping is bounded by the policy.** The beat is half the shortest
  lifetime the node grants, inside `[1s, 30s]`, so a silent device is gone
  within half a lifetime. The sweeper is a goroutine of the serving node and
  is joined by `Stop` / `Close`, so a stopped platform owns no goroutine.

## Platform-small: one node, two halves (Change 7, part one)

A cascade `A → B → C` needs B to be two things: the platform below it and
the subordinate above it. `platform-small` is that node. It reuses both use
cases unchanged — `Acceptor` for the half below, `Registrar` + `Keeper` for
the half above — and what is new lives in `NodeService`: the order the halves
are established in, the way one node advances once for two halves, and the
sorting of what arrives on the shared listener.

```
Start ──▶ serve (Acceptor.Serve)  ──▶ advanceOnline ──▶ online
      └──▶ register (Registrar)   ──▶ advanceOnline ──▶ (already online)
                                  └──▶ Keeper.Start
```

- **Serve first, register second.** Serving almost never fails; registering
  meets an unreachable or refusing peer every day. Putting the failing half
  second keeps its rollback to "stop the serving", which is the same unwind
  an already-established `Stop` performs. Registering first would have to
  withdraw from the upstream as part of the rollback, and the upstream would
  briefly see a subordinate that disappears at once.
- **One state machine, advanced once.** `serve` and `register` both end by
  bringing the node online, but the table has no `online → online` edge.
  `advanceOnline` makes the advance idempotent — whatever the node still
  needs is done, whatever is already true is skipped — instead of widening
  the table for every kind.
- **Either half failing faults the node.** A half-served platform is not a
  state worth reporting, so the other half is unwound (serving goroutine
  stopped, online table cleared) and the node faults. The error names the
  half that failed, and so does the log.
- **Both halves are optional and independently configured.** `platform:` on a
  platform-small means "serve", `registration:` means "register upstream";
  an omitted `platform:` still serves with the defaults, because being a
  platform is what makes a node serve.

### One listener, two readers

Both halves run over the node's own listener, as they must: the peer above
and the peer below see one node on one address. But a socket hands each
datagram to whoever reads first, and the serving half reads all the time —
it would take the `401` and `200 OK` the registering half is waiting for,
decide they are not requests, drop them, and the registration would never
finish.

`splitTransport` (`internal/app/node_split_transport.go`) is the answer: one
reader owns the socket and sorts what it reads — requests to the serving
half, responses to the registering one — and both halves send over the same
socket. The node keeps one splitter per node id; it is created when the node
starts and ended whenever the node stops or faults, so nothing is left
reading a listener that has been given back. A half that is not reading does
not jam the other: messages nobody wants are dropped rather than queued in
front of the ones somebody does.

This is a sorting of one socket, not a SIP transaction layer. When later
changes give a platform-small requests of its own to receive upstream
(`INVITE`, `SUBSCRIBE`), the sort has to become a match on transaction
rather than on direction.

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

## Known limitations

### Solved in Change 4: peer address

`internal/adapter/siptransport` now reports the peer a message arrived from.
gosip's `Messages()` channel does not carry it, but its connection handler
records one on each message with `SetSource` before publishing it, so
`Receive` returns `(Message, addr, error)` and a caller can hand the address
straight back to `Send` to answer. An address that is missing or lacks a port
is an explicit error — never an empty string — so a caller cannot reply to the
wrong destination. `sipprobe --answer` uses this to reply 200 OK, and the
two-process INVITE → 200 OK handshake is now verified by
`scripts/smoke-sip.sh` and `TestAnswer_TwoProcessesExchange`.

### Open: `modelToGosip` drops headers

`internal/adapter/siptransport` rebuilds a `model.Message` for gosip through
`internal/adapter/sip`'s builders, which currently keep only the method/URI
(or status), body and content type — **headers passed by the caller are
discarded**. Requests still work because the builders generate `Via`, `From`,
`To`, `Call-ID` and `CSeq` themselves, but a hand-built response loses its
headers and is rejected on the wire. Porting headers through is left to a
later change; `sipprobe --answer` is unaffected because it builds gosip
messages directly.

### Open: asynchronous socket release

gosip's `layer.Cancel()` only closes a cancellation channel; the listening
socket is released by its own goroutine afterwards. `Transport.Close()`
therefore waits (up to 500 ms) for the port to become bindable again,
otherwise stopping and immediately restarting a node on the same address
would fail with "address already in use". A timeout is not fatal — the caller
just sees the bind error it would have seen before.

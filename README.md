# gb28181-simulator

[![CI](https://img.shields.io/badge/CI-ci.yml-blue?logo=githubactions)](.github/workflows/ci.yml)
[![Release](https://img.shields.io/badge/release-v0.0.0--test-orange)](.github/workflows/release.yml)

GB/T 28181 protocol simulator written in Go. Change 1 of the 15-step roadmap
delivers the **project skeleton + Web shell**: a single binary that serves
health/version endpoints, streams structured logs over WebSocket and embeds
the Vue 3 dashboard. **No GB28181 protocol logic is implemented yet**; that
arrives in Change 2+.

## Quick start

```sh
make run         # builds web bundle then go run
open http://127.0.0.1:18080
```

The default configuration is loaded from:

| Platform | Path |
|----------|------|
| Linux/macOS | `$XDG_CONFIG_HOME/gb28181-simulator/config.yaml` (defaults to `~/.config/...`) |
| Windows | `%AppData%\gb28181-simulator\config.yaml` |

Override with `-config /path/to/config.yaml` or env
`GB28181_SIMULATOR_CONFIG`.

## Endpoints

- `GET /v1/health` — `{"status":"ok"}`
- `GET /v1/version` — version metadata
- `WS  /v1/logs/stream` — JSON-encoded structured log stream
- `GET /` — embedded Dashboard (Vue 3 + Element Plus)
- `GET /v1/nodes` — node inventory (`id`, `kind`, `status`, `addr`)
- `GET /v1/nodes/{id}` — one node; `404` with `{"error":...}` for an unknown id
- `POST /v1/nodes/{id}/start` — bind the listener, status → `registering`
- `POST /v1/nodes/{id}/stop` — release the listener, status → `offline`

An illegal status transition returns `409` with the node's current status:

```json
{"error":"app: start node 34020000011310000001: model: illegal node status transition: registering -> registering","status":"registering","action":"start"}
```

## Nodes

Declare GB/T 28181 nodes in the config; each node owns its own signalling
listener and lifecycle, and starting one never affects another. See
[`configs/config.example.yaml`](configs/config.example.yaml) for a complete
file.

```yaml
nodes:
  - id: "34020000011310000001"   # 20 digits; the 3-digit type code sets the kind
    kind: device                 # device | platform-large | platform-small
    domain: "3402000000"
    addr: "127.0.0.1:15060"      # must be unique across nodes
    vendor: acme                 # optional
```

Omitting `nodes:` runs with zero nodes. Statuses advance through
`idle → registering → registered → online`, stop lands on `offline`, and
`fault` is recoverable only back to `idle` or `offline`.

A `device` node registers as soon as it is started: add a `registration:`
block and it sends REGISTER, answers the platform's 401 challenge with a
Digest credential and goes `online`; a failure (timeout, refusal, 5xx)
faults the node and frees its port. Without the block a started node
stays `registering`, exactly as before.

A `platform-large` node is the other half: it **accepts** registrations.
Starting one launches a serving goroutine on its listener and takes the node
`online` (meaning: the platform is serving). A REGISTER without credentials
is answered `401` with a Digest challenge; one whose `Authorization` cannot
be read is challenged again; a username the platform has no account for, or
a well-formed response that does not match, is refused `403` without a
second challenge, as GB/T 28181 §L.2 requires. A device that gets through is
granted a lifetime clamped to the platform's window and recorded in the
node's online device table, readable at `GET /v1/nodes/{id}/devices`;
`Expires: 0` removes it. Stopping the platform ends the serving goroutine
before its port is released and clears the table.

A platform also **receives heartbeats, answers catalog queries and sweeps
out expired devices**: a downstream's `Keepalive` notify refreshes its row
without extending the lifetime it was granted, a `Catalog` query is answered
with that platform's own online devices, and a device that stops
re-registering is dropped once the granted lifetime lapses.

```yaml
  - id: "34020000002000000001"
    kind: platform-large
    domain: "3402000000"
    addr: "127.0.0.1:15061"
    platform:                      # optional; defaults below
      realm: "3402000000"          # default: the node's domain
      accounts:                    # a platform without accounts accepts nobody
        - username: "34020000011310000001"
          password: "change-me"    # never logged
      min_expires: 60              # default 60
      default_expires: 3600        # default 3600
      max_expires: 86400           # default 86400
```

A `platform-small` node is both halves at once — the link in the middle of a
cascade `A → B → C`. Declare `platform:` and it **serves** the downstreams
below it exactly as a platform-large does; declare `registration:` and it
**registers** with the platform above it exactly as a device does. Declare
both and the node relays: a device registers with the small platform, which
is itself registered with the large one. Each platform's online table holds
the level directly below it and only that level, so the platform at the top
can ask the platform in the middle for a catalogue and be told about the
devices that registered with it.

Both halves run over the node's one listener, and the node is still one
node: it comes `online` once, whichever half gets there first, and if either
half fails the node faults and the other half is unwound rather than left
running alone. Stopping it withdraws from the platform above (`Expires: 0`)
before it ends the serving below.

```yaml
  - id: "34020000002160000001"
    kind: platform-small
    domain: "3402000000"
    addr: "127.0.0.1:15062"
    platform:                      # optional; the half below it
      realm: "3402000000"
      accounts:
        - username: "34020000011310000002"
          password: "change-me"
    registration:                  # optional; the half above it
      server: "127.0.0.1:15061"
      server_id: "34020000002000000001"
      password: "change-me"
```

Once online the node is kept open: it sends a MANSCDP `Keepalive` as a SIP
`MESSAGE` every `heartbeat_interval`, renews its registration at half the
lifetime the platform granted, and gives up after `heartbeat_max_failures`
unanswered beats (faulting the node and freeing its port, as a failed
registration does). `POST /v1/nodes/{id}/unregister` sends `Expires: 0`,
stops the background work and takes the node `offline`; if the platform
refuses, the node stays `online` and the error names the stage.

```yaml
nodes:
  - id: "34020000011310000001"
    kind: device
    domain: "3402000000"
    addr: "127.0.0.1:15060"
    registration:
      server: "127.0.0.1:15061"    # host:port of the upstream platform
      server_id: "34020000002000000001"  # optional; used in the Request-URI
      password: "change-me"        # never logged (see log.redact_keys)
      expires: 3600                # optional; seconds, default 3600
      transport: udp               # optional; udp (default) or tcp
      timeout: 5s                  # optional; default 5s
      heartbeat_interval: 60s      # optional; keepalive period, default 60s
      heartbeat_timeout: 5s        # optional; default 5s; < interval
      heartbeat_max_failures: 3    # optional; default 3
```

## Tech stack

| Concern | Choice |
|---------|--------|
| Language | Go, pure Go / `CGO_ENABLED=0` |
| SIP | `github.com/ghettovoice/gosip` |
| HTTP | `github.com/labstack/echo/v4` |
| WebSocket | `github.com/gorilla/websocket` |
| Config | `github.com/spf13/viper` (YAML + env override) |
| Storage | `modernc.org/sqlite` (pure Go, no CGO) |
| Logging | `log/slog` (trace / debug / info / warn / error) |
| Tracing | OpenTelemetry — stdout exporter by default, OTLP gRPC optional (`tracing.otlp_endpoint`) |
| Dependency wiring | hand-rolled `internal/platform/servicectx` (no wire / fx) |
| Frontend | Vue 3 + Element Plus + Vite, embedded via `embed.FS` |

## Architecture

`internal/` follows a hexagonal (ports & adapters) layout — see
[`docs/architecture.md`](docs/architecture.md) for the layer diagram, the port
contract list and ServiceContext usage.

## Layout

```
cmd/gb28181-simulator      entrypoint
cmd/sipprobe               SIP diagnostic CLI (Change 2 §7)
internal/adapter           SIP / SDP / Digest / Transport / Audit / Node registry adapters
internal/app               use-case orchestration (NodeService, Change 4)
internal/domain            domain model + port interfaces
internal/interface/http    HTTP/WS server (Echo + gorilla/websocket)
internal/interface/webui   embedded Dashboard (Vue 3 + Element Plus, embed.FS)
internal/platform          config, logging, tracing, clock, servicectx
internal/storage           SQLite bootstrap (no business schema yet)
internal/sipprobe          sipprobe core (testable)
configs/                   example configurations
scripts/                   smoke helpers (Change 2 §8.2)
web/                       Vue 3 + Vite source
openspec/                  OpenSpec change artifacts
```

## Build matrix

```
linux/amd64   linux/arm64
darwin/amd64  darwin/arm64
windows/amd64
```

All binaries are produced with `CGO_ENABLED=0`. Run `make release-matrix`
locally or trigger the GitHub Actions workflow. As of Change 2 the matrix
emits both `gb28181-simulator` and `sipprobe` for every platform.

## Development

```sh
make fmt            # gofmt -s -w + goimports
make lint           # go vet ./...
make test           # go test -race -cover ./...
make build          # build bin/gb28181-simulator (web bundle first)
make sipprobe-build # build bin/sipprobe
make service-build  # build every service binary for the host platform
make sip-test       # fast feedback: SIP / SDP / Digest only (≈ 3-4 s)
make release-matrix # 5-platform binaries + sha256, CGO_ENABLED=0
./scripts/smoke-sip.sh   # two-process probe exchange, explicit port control
```

`bin/gb28181-simulator sipprobe ...` also exposes the diagnostic probe, so a
single binary can act as either the simulator or the probe.

The Dashboard sources live under `web/`; the production bundle is written to
`internal/interface/webui/embed/dist/` and embedded into the Go binary via `//go:embed`.

## License

Apache License 2.0 — see `LICENSE`.
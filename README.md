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
internal/adapter           SIP / SDP / Digest / Transport / Audit adapters
internal/app               use-case orchestration (reserved for Change 4+)
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
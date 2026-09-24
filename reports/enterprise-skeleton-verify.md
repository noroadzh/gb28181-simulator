# Change 3 — `enterprise-skeleton` verification report

Scope: reshape `internal/` into a hexagonal (ports & adapters) layout, add a
hand-rolled `ServiceContext` container and an OpenTelemetry trace provider, and
rewrite the `cmd/` entry points as assembly-only wiring. No business logic.

Date: 2026-09-24 · Toolchain: Go with `CGO_ENABLED=0`

## 1. Summary

| Dimension | Status |
|-----------|--------|
| Completeness | **46 / 47 tasks** (§11.4 blocked — see §6) |
| Correctness | **6 / 6** spec requirements covered |
| Coherence | **5 / 5** design decisions followed |
| Build | `go build ./...`, `CGO_ENABLED=0 go build ./...` — PASS |
| Tests | `go test -race -count=1 ./...` — 16/16 packages PASS, 0 failures |
| Test count | 159 test functions across `internal/` |

## 2. Task completion

| Section | Tasks | Done | Note |
|---------|-------|------|------|
| §1 Directory skeleton | 3 | 3 | five layers + `doc.go` placeholders |
| §2 Domain model & ports | 7 | 7 | `Message`/`Session`/`Credentials`, 5 (+2) ports |
| §3 Platform logging/config/clock | 4 | 4 | paths moved under `platform/` |
| §4 OTel trace provider | 5 | 5 | stdout default, OTLP gRPC optional |
| §5 ServiceContext container | 4 | 4 | 12 container tests |
| §6 Adapter migration | 6 | 6 | compile-time port assertions |
| §7 Interface migration | 3 | 3 | `internal/api`→`interface/http`, `webui`→`interface/webui` |
| §8 Storage as adapter | 1 | 1 | `var _ port.Storage = (*Store)(nil)` |
| §9 cmd entry rewrite | 3 | 3 | both binaries use Provide/Build/MustGet |
| §10 Dependency baseline | 3 | 3 | OTel `v1.26.0`, cross-compiles verified |
| §11 End-to-end | 5 | **4** | §11.4 blocked (§6) |
| §12 Docs & report | 3 | 3 | README, `docs/architecture.md`, this file |

Total 47, complete 46.

## 3. Requirement coverage (6 / 6)

| Requirement | Evidence | Status |
|---|---|---|
| Hexagonal layered structure | `internal/` = `adapter app domain interface platform sipprobe storage`; `go list -deps ./internal/domain/...` shows only `domain`, `domain/model`, `domain/port` | PASS |
| Domain ports as Go interfaces | `port.SIPTransport`, `SDPCodec`, `Authenticator`, `Challenger`, `AuditSink`, `Clock`, `Storage` all defined in `internal/domain/port/` | PASS |
| ServiceContext type-safe registry | `internal/platform/servicectx/` — `Provide`/`Build`/`MustGet[T]`/`NewKey[T]`; 12 tests incl. reverse close, rollback, duplicate key, panic messages | PASS |
| OTel trace provider in platform | `internal/platform/observability/tracing/` — stdout exporter default, OTLP gRPC optional, `TraceIDRatioBased` sampling, `Close` shuts processors down | PASS |
| cmd main.go as assembly entry | both `cmd/*/main.go` end in `Build` + `MustGet`; no business logic | PASS |
| CGO-free build preserved | `CGO_ENABLED=0` builds for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64; no CGO in OTel deps | PASS |

### Scenario spot-checks

- **domain zero-dependency**: `go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...` → only the three domain packages. PASS
- **adapter → domain compile-time assertion**: `var _ port.SDPCodec`, `port.SIPTransport`, `port.Authenticator`, `port.Challenger`, `port.AuditSink`, `port.Storage` all present. PASS
- **MustGet type safety**: panic messages distinguish missing key vs. type mismatch. PASS
- **OTel deps are pure Go**: no package under `./internal/platform/observability/tracing/...` reports `CgoFiles`. PASS

## 4. Design decisions (5 / 5)

| Decision | Implementation | Status |
|---|---|---|
| **D1** ports owned by `domain/port/`, zero external deps | all port files take `context` + `domain/model` types only | FOLLOWED |
| **D2** hand-rolled minimal container | `servicectx.Container` with ordered `Provide`, reverse `Close`, rollback; no wire/fx | FOLLOWED |
| **D3** OTel provider wrapping `sdktrace` | `tracing.Provider` with `Tracer(name)`/`Close(ctx)`, single `otel.SetTracerProvider` | FOLLOWED |
| **D4** move-not-rewrite migration | old packages relocated with import updates + assertions; `adapter/sip` documented as a helper library (assertion lives in `adapter/siptransport`) | FOLLOWED |
| **D5** cmd as assembly template | `main.go` = key definitions + `Provide` chain + `Build` + `MustGet` | FOLLOWED |

## 5. Evidence

### Build & tests

```
$ go build ./...                          # PASS
$ CGO_ENABLED=0 go build ./...            # PASS
$ go test -race -count=1 -timeout=120s ./...
ok  internal/adapter/audit         ok  internal/adapter/auth
ok  internal/adapter/sdp           ok  internal/adapter/sip
ok  internal/adapter/siptransport  ok  internal/domain/model
ok  internal/domain/port           ok  internal/interface/http
ok  internal/interface/webui       ok  internal/platform/clock
ok  internal/platform/config       ok  internal/platform/observability/logging
ok  internal/platform/observability/tracing
ok  internal/platform/servicectx   ok  internal/sipprobe
ok  internal/storage                      # 16/16, 0 FAIL
```

### Dependency baseline (§10)

```
$ go mod tidy
$ go list -m go.opentelemetry.io/otel{,/sdk,/exporters/stdout/stdouttrace}
go.opentelemetry.io/otel                              v1.26.0
go.opentelemetry.io/otel/sdk                          v1.26.0
go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.26.0
```

All three moved from `// indirect` to direct `require` entries.
The project is not a git working tree, so the versions are recorded here
instead of in a commit message.

```
$ CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  go build ./cmd/sipprobe   # PASS
$ CGO_ENABLED=0 GOOS=windows GOARCH=amd64  go build ./cmd/sipprobe   # PASS
$ make release-matrix                                                 # PASS
bin/{gb28181-simulator,sipprobe}-{linux-amd64,linux-arm64,
    darwin-amd64,darwin-arm64,windows-amd64}(.exe) + .sha256
```

`make sip-test` completes in ~5.0 s after the Makefile was repointed from the
removed `./internal/{sip,sdp,auth}/...` paths to
`./internal/adapter/{sip,sdp,auth}/...`.

### Start-up behaviour (§11.3)

```
$ ./bin/gb28181-simulator -config configs/config.example.yaml
config loaded path=configs/config.example.yaml        # stdout (pre-logger)
{
  "Name": "startup",
  "SpanContext": { "TraceID": "5e1e864703b480637adc3492ca06ec19", ... },
  "Resource": [ { "Key": "service.name", "Value": "gb28181-simulator" },
                { "Key": "telemetry.sdk.version", "Value": "1.26.0" } ],
  "InstrumentationLibrary": { "Name": "cmd/gb28181-simulator" }
}
```

Log file order confirms the spec sequence:

```
logger initialized        (level=info)
tracing provider started  (enabled=true, sample_ratio=1)
starting gb28181-simulator
http listening            (addr=127.0.0.1:18080)
```

### Golden fixtures (§11.5)

```
internal/adapter/auth/testdata  auth-invite-qop.auth: OK
                                auth-register-legacy.auth: OK
                                auth-register-qop.auth: OK
internal/adapter/sdp/testdata   gb28181-invite-av.sdp: OK
                                gb28181-invite-ps.sdp: OK
                                rfc4566-only.sdp: OK
```

6/6 OK. Only two `testdata` directories exist, not the ten the task text
assumed; the fixtures that do exist all verify.

## 6. Known limitation — §11.4 not completed

**Task**: launch two `bin/gb28181-simulator sipprobe` processes that exchange
INVITE / 200 OK, both exiting 0.

**Blocked.** `sipprobe` in receive-only mode observes inbound messages but
never answers them. Answering requires the peer address, and
`internal/adapter/siptransport` does not carry one — gosip's `Messages()`
channel drops it, and the transport's own doc comment defers the work:

> "The remote endpoint is not carried by gosip's Messages() channel; callers
> needing the peer address should parse it from the topmost Via header
> (Change 4+)."

Measured behaviour:

```
A  sipprobe --bind udp://127.0.0.1:15060                     → received INVITE, exit 0
B  sipprobe --bind udp://127.0.0.1:15061 --send-to …:15060
   --expect-status 200                                       → "timeout waiting for
                                                               status=200", exit 2
```

What was verified instead:

- the `sipprobe` subcommand on `bin/gb28181-simulator` works and exits
  promptly (it was silently starting the HTTP server before this change, which
  hung the process);
- a real INVITE → 200 OK exchange passes in-process:
  `go test -run TestRun_AcceptAnyResponse ./internal/sipprobe` — PASS
  (a fake UAS answers with 200 OK).

**Recommended follow-up (Change 4+)**: add `siptransport` support for the peer
address (Via-header parsing or a `ReceiveFrom` API), then have `runReceive`
answer requests with 200 OK. That makes the two-process handshake and
`scripts/smoke-sip.sh` pass as written.

## 7. Deviations from the task text

| Item | Deviation |
|---|---|
| §11.4 | Not completed — blocked, see §6 |
| §11.5 | 6 golden fixtures verified; the assumed 10 do not exist |
| §10.2 | Versions recorded in this report; project is not a git working tree |
| §11.2 | `make sip-test` ≈ 5.0 s (target was < 5 s) |

## 8. Cleanup performed during verification

- Deleted the stale `internal/webui/` (superseded by
  `internal/interface/webui/`) and the empty `internal/interfaces/` placeholder.
- Repointed `web/vite.config.js` `outDir`, the `Makefile` `clean` target and
  `README.md` to `internal/interface/webui/embed/dist`.
- Repointed `make sip-test` to `./internal/adapter/{sip,sdp,auth}/...`
  (the old paths no longer exist, so the target failed).
- Updated `README.md` endpoints from `/api/*` to `/v1/*`.
- Added the `sipprobe` subcommand to `cmd/gb28181-simulator` (shared via
  `internal/sipprobe.RunCLI`) so `scripts/smoke-sip.sh`'s invocation works and
  both binaries stay thin assembly entry points.
- Added `make service-build` and a `README.md` tech-stack table.

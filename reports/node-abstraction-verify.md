# Change 4 Verification — `node-abstraction`

| | |
|---|---|
| Change | `node-abstraction`（路线图 Change 4 / 15） |
| Artifacts | `openspec/changes/node-abstraction/`（proposal / specs ×2 / design D1–D10 / tasks） |
| Commit | `970c8fe` on `main` |
| Toolchain | go1.25.5 darwin/amd64 |
| Date | 2026-09-24 |
| Result | **45 / 45 tasks complete** — see §4 for six deviations that were recorded rather than hidden |

---

## 1. Summary

Change 4 adds the first real behaviour on top of the Change 3 skeleton: a node
is an identity plus a lifecycle, and several can run in one process. It also
closes all three carry-over items from `enterprise-skeleton`.

What landed:

- **Domain** — `NodeID` (20-digit GB/T 28181 code), `NodeKind`,
  `NodeProfile`, `Node`, and a `Status` machine with an explicit legal
  -transition table and the `ErrIllegalTransition` sentinel.
- **Ports** — `NodeRegistry`, `NodeLifecycle`, `NodeAdvancer`; and
  `SIPTransport` finally matches the archived spec (`Send(ctx, msg, dst)`,
  `Receive(ctx) (Message, string, error)`).
- **Adapters** — `internal/adapter/nodereg` (registry + lifecycle, one
  listener per node, address uniqueness, roll-back on bind failure), and
  `internal/adapter/siptransport` now reports the peer address.
- **App** — `internal/app.NodeService`, depending on domain ports only.
- **Interface** — `GET /v1/nodes`, `GET /v1/nodes/{id}`, and
  `POST /v1/nodes/{id}/{start,stop}` with 404 / 409 mapping.
- **Platform** — optional `nodes:` configuration that fails loudly.
- **sipprobe** — `--answer` (opt-in) replies 200 OK, which is what makes a
  two-process INVITE → 200 OK handshake possible.

## 2. Evidence baseline

| Metric | Before | After | Command |
|---|---|---|---|
| Packages `ok` | 16 | **18** | `go test -race -count=1 ./...` |
| FAIL | 0 | **0** | same |
| Test functions | 156 | **205** (+49) | `go test -list '.*' ./... \| grep -c '^Test'` |
| Golden fixtures | 6 OK | **7 OK** | `sha256sum -c golden-sha256` × 3 dirs |
| Cross-compile | 5 / 5 | **5 / 5** | `CGO_ENABLED=0` × 5 targets |

> **The artifacts say "159 existing tests"; the real baseline was 156.**
> Verified twice with `go test -list`. The number in the artifacts was not
> corrected there — it is recorded here instead, so the discrepancy is on the
> record rather than silently propagated.

`-race` runs are done **once at a time**: several packages bind fixed ports,
so back-to-back runs produce spurious port-contention failures.

## 3. Acceptance matrix

### §1 Domain identity (1.1–1.3) ✓

`internal/domain/model/node.go`. `ParseNodeID` rejects wrong length, non-digits
and unmapped type codes, naming the reason and observed length in every error.
Immutability is asserted by reflecting over the fields of `NodeID`,
`NodeProfile` and `Node`: none is exported, so no caller outside the package
can reach in and change a value.

```
--- PASS: TestNodeID_Parse_AcceptsLegalEncoding (5 subtests)
--- PASS: TestNodeID_Parse_RejectsIllegal (6 subtests)
--- PASS: TestNodeProfile_FieldsUnexported
```

### §2 Status machine (2.1–2.3) ✓

`internal/domain/model/node_status.go`. A 6×6 table-driven test compares the
implementation against an **independently written** expectation table, so a
typo in `legalTransitions` cannot hide behind a copy of itself. `errors.Is(err,
ErrIllegalTransition)` is asserted, and `Fault` only exits to `Idle` or
`Offline`.

### §3 Ports (3.1–3.3) ✓

`internal/domain/port/node.go` and `transport.go`.

```
$ go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/domain/...
internal/domain
internal/domain/model
internal/domain/port
```

The `SIPTransport` signature now matches `openspec/specs/enterprise-skeleton/spec.md`
verbatim: `Send(ctx, Message, dst) error`, `Receive(ctx) (Message, string, error)`,
`Close() error`.

### §4 siptransport (4.1–4.3) ✓

`Receive` returns the address the message arrived from. Verified end to end
over a real UDP socket — the reported peer equals the sender's real endpoint,
not a placeholder:

```
--- PASS: TestPortAdapter_SendUsesExplicitDestination
peer=127.0.0.1:64840 sender=127.0.0.1:64840 receiver=127.0.0.1:64311
```

An address without a port, or one that trims to empty, is an **explicit error**.
`Send` uses the caller's `dst` and rejects an empty one; the old
`destinationFromURI` helper (which inferred the target from the message URI)
was deleted, since keeping it would contradict D4.

### §5 nodereg (5.1–5.4) ✓

`internal/adapter/nodereg`. Duplicate ids are rejected without overwriting the
incumbent; a duplicate address is rejected with an error naming the node that
holds it. `go test -race` with 32 concurrent registrations, 8 concurrent
readers and 32 concurrent unregistrations reports no race and no lost update.

### §6 NodeService (6.1–6.5) ✓

```
$ go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/app/...
internal/domain/model
internal/domain/port
```

No adapter. `Start` lands on `registering` and stays there; `MarkRegistered`
and `MarkOnline` are the only things that advance a node further, as D9
requires. A bind failure rolls back to `idle` (first start) or `fault`
(restart) and leaves no listener bound.

### §7 Configuration (7.1–7.3) ✓

`nodes:` is optional; absent means an empty (never nil) slice. An illegal entry
aborts loading and names the index and field:

```
config: nodes[1].id: model: illegal node id "1234": length 4, want 20
config: nodes[0].kind: model: unknown node kind "camera"
```

### §8 HTTP (8.1–8.4) ✓

Zero nodes returns `[]`, not `null`. Unknown ids are 404 with
`{"error":...}`. An illegal transition is 409 and reports the real status —
observed live:

```
$ curl -X POST .../v1/nodes/34020000011310000001/start   # twice
{"error":"app: start node 34020000011310000001: model: illegal node status
 transition: registering -> registering","status":"registering","action":"start"}
```

`/v1/health` and `/healthz` are unaffected.

### §9 Assembly (9.1–9.3) ✓

`cmd/gb28181-simulator/main.go` registers every configured node and hands the
service to the HTTP layer. Startup order is unchanged:

```
config loaded path=configs/config.example.yaml
logger initialized
tracing provider started
nodes registered count=2
starting gb28181-simulator ... nodes=2
http listening addr=127.0.0.1:18100
```

`go list -deps ./internal/platform/servicectx` no longer contains
`internal/storage` (count: 0).

### §10 sipprobe --answer (10.1–10.4) ✓

Opt-in: without `--answer` the sender times out (exit 2); with it, the
handshake completes. The 200 OK is pinned by a golden fixture
(`internal/sipprobe/testdata/answer-200-ok.sip`, 240 bytes) whose branch is
inherited from the request rather than regenerated.

Two real processes, both exit 0 (`SIPPROBE_CROSS_PROCESS=1 go test ./internal/sipprobe -run TwoProcesses`):

```
[cross-process] receiver="-1	INVITE sip:probe@127.0.0.1:64232 SIP/2.0"
                sender="200	SIP/2.0 200 OK"
```

> **This Go test is opt-in** (`SIPPROBE_CROSS_PROCESS=1`). Run inside a
> parallel `go test -race ./...` the two child processes compete with every
> other package for CPU and for the loopback ports the test probes, and the
> handshake intermittently times out — it failed twice that way during this
> verification. The deterministic cross-process check is
> `scripts/smoke-sip.sh`, which runs the same exchange sequentially and is
> what CI should use.

`scripts/smoke-sip.sh` restored to its original assertion (A answers, B
expects 200):

```
[smoke] A out: -1	INVITE sip:probe@127.0.0.1:5060 SIP/2.0
[smoke] B out: 200	SIP/2.0 200 OK
[smoke] A rc=0 B rc=0
[smoke] OK
```

### §11 End-to-end (11.1–11.4) ✓

```
ok  internal/domain/model       ok  internal/adapter/nodereg
ok  internal/domain/port        ok  internal/app
ok  internal/adapter/siptransport   ok  internal/interface/http
... 18 packages, 0 FAIL
```

Two nodes, real UDP listeners, started together and exchanged traffic in both
directions without crosstalk; stopping one left the other running. Golden
fixtures: 7 / 7 OK. Cross-compile: all five targets PASS.

## 4. Deviations from the artifacts

Six. Each is a deliberate, recorded choice — none silently narrows specified
behaviour.

1. **Baseline is 156 tests, not 159** (§11.1). The artifacts' number was
   measured wrong; recorded in §2 rather than corrected in place.

2. **D4's mechanism differs from its wording** (§4.1). The artifact says "UDP
   takes `ReadFromUDP`, TCP takes `conn.RemoteAddr()`". The socket layer here
   belongs to gosip, which does not expose its connections — but gosip's
   connection handler already records exactly that address on each message via
   `SetSource`, so `Receive` reads it back. The observable contract is
   unchanged (peer address returned, usable for replying, error when
   unavailable). Re-implementing the socket layer to match the wording
   literally would have meant rewriting 240 lines and re-deriving TCP framing.

3. **`AuditSink` is not injected into `NodeService`** (§6.1). D6 lists it, but
   the domain's only audit event, `model.WireEvent`, describes wire-level
   transmission — `Transport` is mandatory and `Direction` is only
   transmit/receive — so it cannot carry a status change. Design Q2 already
   defers node lifecycle events to Change 13. The service documents this at
   the struct.

4. **A fourth port, `NodeAdvancer`, was added** (§3.1). `NodeLifecycle`'s
   `Start`/`Stop` are intent-revealing; something was needed for Change 5/6/7
   to move a node to `registered`/`online`. It was added rather than
   modifying the two ports §3.1 defines.

5. **`Registering → Idle` was added to the transition table** (§2.2). Spec
   6.3 requires a failed first start to roll back to `idle`, which the
   original table did not permit. The test's independent expectation table was
   updated to match.

6. **Type-code → kind mapping is defined here** (§1.2). The artifacts say
   "bytes 11–13 decide the kind" without listing values. This change fixes
   them to GB/T 28181-2016 Annex B practice: `111/112/113/118/131/132` →
   device, `200` → platform-large, `216` → platform-small. Anything else is an
   error, so Change 5/6/7 can extend the set without widening what counts as
   valid.

## 5. Closed carry-over items from `enterprise-skeleton`

| Item | Where it lived | How it closed |
|---|---|---|
| **Q4** — how to expose the peer address | `enterprise-skeleton` design Q4 | D4: extend `Receive` to return the address, implemented in §4 |
| **§11.4** — two `sipprobe` processes exchanging INVITE / 200 OK | `enterprise-skeleton` tasks §11.4, design D6 | D5 + §10: `--answer`, golden fixture, `smoke-sip.sh` restored, cross-process test |
| **Legacy 3** — `servicectx` depended on `internal/storage` | found after that change was archived; not in its artifacts | D10 + §9.3: `StorageKey` is now `NewKey[port.Storage]("storage")` |

Legacy 3 deserves a note: it was **not** in the archived artifacts. It was
found while re-reading the code, which is why the Change 3 plan document
originally attributed it to D4 — D4 only covers the `SIPTransport` signature.
It is now tracked as D10 and the plan document's attribution has been
corrected.

## 6. Known limitations

1. **`modelToGosip` drops headers.** It rebuilds a `model.Message` through the
   builders, keeping only method/URI (or status), body and content type.
   Requests work because the builders generate `Via`/`From`/`To`/`Call-ID`/`CSeq`
   themselves, but a hand-built response loses its headers and is rejected on
   the wire. Out of scope here (`sipprobe --answer` builds gosip messages
   directly and is unaffected). Documented in `docs/architecture.md`.
2. **Asynchronous socket release.** gosip's `layer.Cancel()` only closes a
   channel; the socket is released by its own goroutine. `Transport.Close()`
   now waits up to 500 ms for the port to become bindable, which fixes
   stop-then-restart on the same address. On timeout the caller simply sees
   the bind error it would have seen before.
3. **Stopping a node keeps its address claim.** The node is still registered
   and may be restarted, so another node cannot steal its port in the
   meantime; `Unregister` is what frees the address.

## 7. Reproduction

```sh
go build ./...
go test -race -count=1 ./...                      # once at a time
go test -list '.*' ./... | grep -c '^Test'        # 205
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/app/...
go list -deps ./internal/platform/servicectx | grep -c internal/storage   # 0
(cd internal/adapter/sdp/testdata && sha256sum -c golden-sha256)
(cd internal/adapter/auth/testdata && sha256sum -c golden-sha256)
(cd internal/sipprobe/testdata && sha256sum -c golden-sha256)
rm -f bin/gb28181-simulator && bash scripts/smoke-sip.sh
```

---

## 8. Verification (`openspec verify`)

Re-checked the four artifacts against the tree at `970c8fe`. Three dimensions:

| Dimension | Status |
|---|---|
| Completeness | **45 / 45 tasks** — `openspec status` reports `all_done` |
| Correctness | 8 / 8 requirements implemented; **22 / 24 scenarios fully covered**, 2 partially |
| Coherence | D1–D10 followed, with D6 (AuditSink) as the one documented exception |

Evidence re-run for this pass:

```
go list -deps ./internal/app/...            → internal/domain/{model,port} only
go list -deps ./internal/platform/servicectx | grep -c internal/storage   → 0
internal/domain/port/transport.go:25        → Send(ctx, msg, dst string) error
internal/domain/port/transport.go:34        → Receive(ctx) (Message, string, error)
sha256sum -c golden-sha256 (3 dirs)         → 7/7 OK
```

### CRITICAL

None. No task is left unchecked and no requirement is missing an implementation.

### WARNING

1. **[R3] "independent log fields keyed by its node id" — RESOLVED.**
   (Was: `internal/adapter/nodereg/` emitted no log record at all, although the
   requirement is a MUST and neither `design.md` D3 nor tasks 5.1–5.4
   decomposed it.)
   Fix: `Registry` now carries a `*slog.Logger` — `NewWithLogger` injects one,
   `New()` defaults to `logging.L()` — and every mutation emits a record
   carrying `node_id`: register / unregister, status advance, a rejected
   illegal transition, and listener bind / bind-failure / release. `Lifecycle`
   logs through the registry's logger, so there is a single injection point.
   Tasks gained **5.5** so the artifact matches the implementation.
   Test: `internal/adapter/nodereg/node_log_test.go` asserts each node's
   records carry its own `node_id=` and never mention the other node's id.

2. **[R7] "UDP 与 TCP 两种传输均如此" — TCP is untested.**
   `Receive` reads `msg.Source()`, which gosip also populates on its TCP
   connection handler, so TCP most likely works — but no test binds a TCP
   listener. Every peer-address test uses UDP.
   → Add one TCP case to `internal/adapter/siptransport/transport_peer_test.go`
   asserting the peer equals the client's `conn.LocalAddr()`.

3. **[R3 vs D9] Two scenarios demanded `Online`, which D9 makes unreachable —
   RESOLVED (spec text corrected, behaviour untouched).**
   R3's "两个节点各自绑定独立端口并共存" and "停止单个节点不影响其他节点" asserted
   the nodes are `Online`, contradicting design D9 and R6's own scenario. The
   implementation was right; the spec was wrong.
   Fix: both scenarios now state that the nodes start successfully and hold
   independent listeners — status `Registering`, with `Online` reachable only
   through the identity implementations — and the "stop" scenario asserts the
   other node's state and listener are unaffected instead of naming `Online`.
   R6's 409 scenario and tasks 8.3 now record that `Online` is unreachable in
   this change and the check runs from `registering`.

4. **[D6 vs tasks 6.1 vs spec R4] `AuditSink` is in all three artifacts but not
   in the constructor.** `design.md` D6 and spec R4 list `AuditSink`; tasks 6.1
   lists `NodeAdvancer` instead. `internal/app/node_service.go:42-48` takes
   `(registry, lifecycle, advancer, factory, clock)` — no sink. Q2 already
   defers node lifecycle events to Change 13, so the omission is deliberate, but
   three artifacts now say three different things.
   → Pick one: either drop `AuditSink` from spec R4 and D6, or state at
   `node_service.go` that it is pending Change 13 (a comment exists; the spec
   text should match).

5. **Artifact counts are stale.** proposal/design/tasks say "既有 159 个测试"
   and tasks 11.4 says "6 份 golden"; measured baseline is **156** and there are
   **7** fixtures after this change. Recorded here rather than silently
   corrected in the artifacts.
   → Correct the numbers, or leave them and accept §2 as the reference.

### SUGGESTION

1. **The two-process handshake is opt-in in Go tests.**
   `TestAnswer_TwoProcessesExchange` skips unless `SIPPROBE_CROSS_PROCESS=1`,
   so the default `go test ./...` no longer covers R8's last scenario. The
   deterministic cover is `scripts/smoke-sip.sh`.
   → Make sure CI runs `make sip-smoke` (or set the env var in the test job).

2. **`gofmt` is not universal.** Files added by this change are formatted; many
   pre-existing files (including `internal/app/doc.go`) are not.
   → Add `gofmt -l` to lint, or run a one-off `gofmt -w ./internal ./cmd`.

3. **`modelToGosip` drops caller headers** (Change 3 legacy, documented in
   `docs/architecture.md`). Out of scope here, but it will bite Change 5 the
   moment a response needs a custom header.

### Final assessment

No critical issues. **#1 and #3 are resolved** (implementation + spec text,
both with tests). **3 warnings remain** (#2 TCP peer-address coverage, #4
`AuditSink` wording across three artifacts, #5 stale test/fixture counts) —
none is a behavioural defect and none blocks archiving.

Post-fix checks: `go test -race -count=1 ./...` → 19 packages ok / 0 FAIL;
`openspec validate node-abstraction --strict` → valid; tasks **46 / 46**.

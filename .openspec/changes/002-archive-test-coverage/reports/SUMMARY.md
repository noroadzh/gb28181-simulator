# Change 2 Test Coverage Report

**Change:** 002-archive-test-coverage
**Date:** 2026-09-23
**Go version:** go1.25.5 darwin/amd64
**Flags:** `go test -race -count=1 -timeout=60s -v`
**Result:** ✅ **All tests pass.** 72 PASS, 0 FAIL, 0 SKIP across 12 packages.

---

## Per-package results

| Package | Tests | Wall time | Status |
| --- | --- | --- | --- |
| `internal/api` | 5 | 4.80 s | ✅ PASS |
| `internal/auth` | 12 | 2.66 s | ✅ PASS |
| `internal/config` | 4 | 2.21 s | ✅ PASS |
| `internal/logger` | 7 | 3.34 s | ✅ PASS |
| `internal/sdp` | 13 | 3.73 s | ✅ PASS |
| `internal/sip` | 6 | 5.24 s | ✅ PASS *(new)* |
| `internal/sip/audit` | 5 | 4.76 s | ✅ PASS *(new)* |
| `internal/sipprobe` | 7 | 1.94 s | ✅ PASS *(new)* |
| `internal/siptransport` | 8 | 4.35 s | ✅ PASS *(refactored)* |
| `internal/storage` | 3 | 6.36 s | ✅ PASS |
| `internal/webui` | 2 | 6.90 s | ✅ PASS |

**Total: 72 tests passing, 0 failing.**

`cmd/gb28181-simulator` and `cmd/sipprobe` are the binary entry-points (no test files; covered indirectly by `internal/api` / `internal/sipprobe` tests).

---

## What this change added

### 1. `internal/sip/audit` package (new)
- `emitter.go` — process-global, RWMutex-protected `Emitter` with `NopEmitter` default and `EmitterFunc` adapter.
- `redact.go` — `RedactAuthHeader` regex redacts `response="…"` and `Response="…"` per RFC 2617/7616.
- `audit_test.go` — adapter, global set/reset, concurrent set+emit (race-clean), redactor table-driven.

### 2. `internal/siptransport` (refactored)
- Wired `audit.Global().Emit` on every `Send` (after successful `layer.Send`) and `Receive` (after successful channel push).
- Fixed `Close` to also `close(t.out)` so `Receive` returns `io.ErrClosedPipe` immediately instead of blocking.
- `transport.go` now calls `req.SetDestination(dst)` before `layer.Send(req)` — without it, gosip falls back to the Via port and returns "connection on port X not found" for tests that bind to ephemeral ports.

### 3. `internal/sip` (bugfix)
- `builder.go` now passes `ViaHeader{viaHop}` (value) instead of `&vh` (pointer) — gosip's `Via()` method type-asserts to `ViaHeader` value.
- `builder.go` defaults `Via.Port` to `nil`, letting `transport.Layer.Send` rewrite it from the actual listening socket.
- Added `builder_test.go` — mandatory headers, X-GB-Ver, body/Content-Type/Content-Length, default reason phrases, CSeq, StartLine helpers.

### 4. `internal/siptransport/transport_test.go` (rewritten)
- Rewrote against the actual `siptransport.New(addr, opts)` public API rather than the stale internal-only sketch.
- `TestTransport_MultiInstance` / `…_Simultaneous` use real loopback UDP sockets and verify receive-side delivery.
- `TestTransport_AuditHook_InjectEmitter` swaps in an `audit.EmitterFunc`, sends one INVITE, asserts at least one event with `Direction == DirTransmit`.

### 5. `internal/sipprobe` package (new)
- `cmd/sipprobe` was the only binary left with no coverage. It now has a clean `internal/sipprobe` library (`Options`, `Mode`, `Result`, `Run`) plus a thin `main.go` shell.
- **Bug fixed**: `runSend` now strips the `udp://` / `tcp://` scheme prefix before calling `tr.Send(req, dst)`. Without this, gosip's `transport.Layer.Send` rejects with "too many colons in address".
- 7 tests cover: status parsing, mode inference, timeout exit code, missing-bind exit code, unexpected-status exit code, accept-any-status success path, result formatter.

---

## Reproduce

```bash
go test -race -count=1 -timeout=60s -v ./... \
  | tee .openspec/changes/002-archive-test-coverage/reports/all.txt
```

Per-package logs: `sip.txt`, `siptransport.txt`, `auth.txt`, `sdp.txt`.
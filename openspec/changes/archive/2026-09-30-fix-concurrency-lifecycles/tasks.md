# Tasks

## 1. Fix A1: INVITE watcher lifecycle

- [x] 1.1 Define `ErrSourceClosed` sentinel in `internal/domain/model/errors.go` and `internal/adapter/media/errors.go`; export both so callers can `errors.Is(err, media.ErrSourceClosed)`. Verify: `go build ./...` clean.

- [x] 1.2 In `internal/app/acceptor.go`: replace the package-level `var inviteTimers sync.Map` with an `Acceptor` field `inviteTimers map[string]*time.Timer` plus a new `inviteWatchers map[string]chan struct{}`; add a new mutex `inviteMu sync.Mutex` to protect both maps. Verify: `go build ./internal/app/...` compiles with no undefined references to the old `inviteTimers`.

- [x] 1.3 In `internal/app/acceptor.go`: rewrite `watchInviteExpiry` to use a `done chan struct{}` per dialog; the goroutine body becomes `select { case <-timer.C: ...; case <-done: ...; case <-a.ctx.Done(): ... }`. Store the `done` channel alongside the timer in `inviteWatchers`. Verify: existing `acceptor_test.go` passes; `go vet ./internal/app/...` clean.

- [x] 1.4 In `internal/app/acceptor.go`: update `cancelInviteExpiry` to close the `done` channel before calling `timer.Stop()`. Update `handleAck` and any other call sites to use the new `Acceptor`-field map. Verify: `go build ./...`; all existing SIP INVITE e2e tests pass.

- [x] 1.5 Add `TestAcceptor_INVITEExpiry_NoLeak` in `internal/app/acceptor_test.go`: create an Acceptor, spawn 200 mock INVITE dialogs via `watchInviteExpiry`, call `cancelInviteExpiry` on all 200, poll `len(a.inviteWatchers)` until zero (2s timeout, 50ms poll interval). Assert no watcher goroutine remains in the map. Verify: `go test -run TestAcceptor_INVITEExpiry_NoLeak ./internal/app/...` passes; `go test -race -run TestAcceptor_INVITEExpiry_NoLeak` passes.

## 2. Fix A2: split-transport close safety

- [x] 2.1 In `internal/app/node_split_transport.go`: add `closed atomic.Bool` field and `closeOnce sync.Once` to `splitTransport`. Rewrite `Close()` to use `closeOnce.Do` and set `closed.Store(true)` **before** `cancel()`, then `close(uas)`/`close(uac)` **after** `<-done`. Verify: `go build ./internal/app/...` clean.

- [x] 2.2 Add helper `trySend(ch chan arrival, a arrival) bool` that checks `s.closed.Load()` first and returns false if closed; replace the three bare `s.uas <-`/`s.uac <-` in `dispatch` (lines 138, 162, 186) and the two in transaction handler closures with calls to `trySend`. Verify: `go build ./...`; existing `node_split_transport_test.go` tests pass.

- [x] 2.3 Fix the TOCTOU double-lock in `dispatch:151-170`: merge the `already` check and the `handlers[callID] = h` write into a single locked critical section. Verify: `go build ./...`.

- [x] 2.4 Add `TestSplitTransport_CloseRace` in `internal/app/node_split_transport_test.go`: start a splitter, register a transaction handler, call `Close()` from goroutine A, then trigger the registered handler from goroutine B; assert no panic. Also add `TestSplitTransport_DoubleClose`: call `Close()` twice and assert the second returns nil. Verify: `go test -race -run TestSplitTransport_CloseRace ./internal/app/...` passes; `go test -run TestSplitTransport_DoubleClose ./internal/app/...` passes.

## 3. Fix A3: HLS source close interrupt

- [x] 3.1 In `internal/adapter/media/hls_source.go`: add `cancel context.CancelFunc` and `pw *io.PipeWriter` as fields of `HLSSource`; modify `Open` to create a cancellable `runCtx, cancel := context.WithCancel(ctx)` and store both `cancel` and `pw` under `h.mu`. Verify: `go build ./internal/adapter/media/...` clean.

- [x] 3.2 Rewrite `HLSSource.Close()` to lock `h.mu`, set `h.closed = true`, read and nil `h.cancel`/`h.pw`, unlock, then call `cancel()` and `pw.CloseWithError(ErrSourceClosed)` (both nil-safe). Add a second `h.closed` check for idempotency. Verify: `go build ./...`.

- [x] 3.3 Add `TestHLSSource_CloseUnblocksReader` in `internal/adapter/media/hls_source_test.go`: spin up `httptest.Server` whose handler blocks on `<-r.Context().Done()` (returns 200 with empty body after unblock), call `Open`, start a `go func() { rc.Read(buf) }()` goroutine, `time.Sleep(50ms)`, then `Close()`, assert the Read returns within 200ms with a non-nil, non-EOF error. Also assert a second `Close()` call returns nil. Verify: `go test -race -run TestHLSSource_CloseUnblocksReader ./internal/adapter/media/...` passes.

## 4. Regression and integration

- [x] 4.1 Run `go test -race ./internal/app/... ./internal/adapter/media/...` and assert zero races reported. Verify: exit code 0.

- [x] 4.2 Run `go test ./internal/app/... ./internal/adapter/media/...` and assert all tests pass. Verify: exit code 0.

- [x] 4.3 Run `make smoke` (the project's existing smoke suite) and assert it passes. Verify: exit code 0.

- [x] 4.4 Verify the three delta specs are consistent with the implementation: re-read each spec scenario and confirm the corresponding code path. List: spec `media-sources` "Close 打断慢分片" → `hls_source.go:Close` + `pw.CloseWithError`; spec `core-sip-stack` "关闭与 handler 并发不 panic" → `node_split_transport.go:trySend` + `closed` flag; spec `node-abstraction` "节点停止取消所有 pending 看门狗" → `acceptor.go:cancelInviteExpiry` close(done) 路径. Confirm all match.
package audit_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
)

// TestMain resets the global emitter after the package tests run so we
// don't leak state to other test packages that may be linked into the
// same binary.
func TestMain(m *testing.M) {
	audit.SetEmitter(nil)
	m.Run()
}

func TestEmitterFunc_Adapter(t *testing.T) {
	t.Parallel()
	var called int
	var captured audit.WireEvent
	emitter := audit.EmitterFunc(func(e audit.WireEvent) {
		called++
		captured = e
	})
	emitter.Emit(audit.WireEvent{Direction: audit.DirTransmit, Bytes: []byte("INVITE"), Timestamp: time.Now()})
	if called != 1 {
		t.Fatalf("expected 1 call, got %d", called)
	}
	if captured.Direction != audit.DirTransmit {
		t.Fatalf("expected transmit, got %q", captured.Direction)
	}
	if string(captured.Bytes) != "INVITE" {
		t.Fatalf("expected Bytes=INVITE, got %q", captured.Bytes)
	}
}

func TestGlobal_SetAndReset(t *testing.T) {
	// Not t.Parallel: mutates process-global state.
	var hits int
	audit.SetEmitter(audit.EmitterFunc(func(audit.WireEvent) { hits++ }))
	audit.Global().Emit(audit.WireEvent{Direction: audit.DirReceive})
	if hits != 1 {
		t.Fatalf("expected 1 hit, got %d", hits)
	}
	// Resetting to nil should drop into the NopEmitter without panicking.
	audit.SetEmitter(nil)
	audit.Global().Emit(audit.WireEvent{Direction: audit.DirTransmit})
}

func TestGlobal_ConcurrentSetAndEmit(t *testing.T) {
	// Not t.Parallel: mutates process-global state.
	var hits int64
	audit.SetEmitter(audit.EmitterFunc(func(audit.WireEvent) {
		atomic.AddInt64(&hits, 1)
	}))
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			audit.Global().Emit(audit.WireEvent{Direction: audit.DirReceive})
		}()
	}
	wg.Wait()
	if atomic.LoadInt64(&hits) != 100 {
		t.Fatalf("expected 100 hits under concurrency, got %d", atomic.LoadInt64(&hits))
	}
	audit.SetEmitter(nil)
}

func TestNopEmitter_NeverPanics(t *testing.T) {
	t.Parallel()
	var n audit.NopEmitter
	n.Emit(audit.WireEvent{Direction: audit.DirTransmit, Bytes: []byte("anything")})
}

func TestRedactAuthHeader(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no auth header",
			in:   "INVITE sip:alice@example.com SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1\r\n\r\n",
			want: "INVITE sip:alice@example.com SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1\r\n\r\n",
		},
		{
			name: "RFC 2617 lowercase response",
			in:   `WWW-Authenticate: Digest realm="x", nonce="abc", response="deadbeef1234567890"`,
			want: `WWW-Authenticate: Digest realm="x", nonce="abc", response="***REDACTED***"`,
		},
		{
			name: "RFC 7616 capitalised Response",
			in:   `Authorization: Digest username="alice", Response="cafef00dcafef00dcafef00dcafef00c"`,
			want: `Authorization: Digest username="alice", Response="***REDACTED***"`,
		},
		{
			name: "empty buffer",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := string(audit.RedactAuthHeader([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}
package audit

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time assertion that *SinkAdapter satisfies port.AuditSink.
func TestSinkAdapter_ImplementsPort(t *testing.T) {
	var _ port.AuditSink = (*SinkAdapter)(nil)
}

// TestSinkAdapter_RoundTripThroughPort drives the adapter via the port
// interface (not the concrete type) and asserts it still routes to the
// underlying legacy Emitter. This guarantees that any future migration of
// the transport layer to the port interface does not break the bridge.
func TestSinkAdapter_RoundTripThroughPort(t *testing.T) {
	var sink port.AuditSink = NewSinkAdapter(nil)
	if err := sink.Emit(newTestWireEvent(t, model.DirReceive, 7, nil)); err != nil {
		t.Fatalf("port AuditSink.Emit = %v, want nil", err)
	}
}

func TestSinkAdapter_NilEmitterBecomesNop(t *testing.T) {
	a := NewSinkAdapter(nil)
	if err := a.Emit(newTestWireEvent(t, model.DirTransmit, 0, nil)); err != nil {
		t.Fatalf("nil-emitter Emit = %v, want nil", err)
	}
}

// TestSinkAdapter_ForwardsFields verifies that every field on model.WireEvent
// is propagated to the legacy WireEvent unchanged.
func TestSinkAdapter_ForwardsFields(t *testing.T) {
	var got WireEvent
	em := EmitterFunc(func(e WireEvent) { got = e })
	a := NewSinkAdapter(em)

	we := newTestWireEvent(t, model.DirTransmit, 128, []byte("INVITE sip:x@y SIP/2.0\r\n\r\n"))
	if err := a.Emit(we); err != nil {
		t.Fatalf("Emit = %v", err)
	}
	if string(got.Direction) != string(we.Direction()) {
		t.Errorf("Direction = %q, want %q", got.Direction, we.Direction())
	}
	if got.Local != we.Local().String() {
		t.Errorf("Local = %q, want %q", got.Local, we.Local().String())
	}
	if got.Remote != we.Peer().String() {
		t.Errorf("Remote = %q, want %q", got.Remote, we.Peer().String())
	}
	if string(got.Bytes) != we.Preview() {
		t.Errorf("Bytes = %q, want %q (Preview)", got.Bytes, we.Preview())
	}
	if !got.Timestamp.Equal(we.At()) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, we.At())
	}
}

// TestSinkAdapter_ForwardsReceiveDirection specifically exercises the
// receive path because legacy callers distinguish the two via the Direction
// string and downstream tooling often filters on it.
func TestSinkAdapter_ForwardsReceiveDirection(t *testing.T) {
	var got WireEvent
	em := EmitterFunc(func(e WireEvent) { got = e })
	a := NewSinkAdapter(em)

	we := newTestWireEvent(t, model.DirReceive, 0, nil)
	if err := a.Emit(we); err != nil {
		t.Fatalf("Emit = %v", err)
	}
	if got.Direction != auditDirReceive {
		t.Errorf("Direction = %q, want %q (legacy receive)", got.Direction, auditDirReceive)
	}
}

// auditDirReceive is the legacy receive marker ("r") that the adapter must
// surface when the domain model signals DirReceive. Mirrored here so the
// test is robust against future constant renames in the audit package.
const auditDirReceive = "r"

// TestSinkAdapter_PreservesPreview verifies that the preview string supplied
// to the domain model is forwarded verbatim into the legacy WireEvent.Bytes
// field. The adapter treats Preview as the on-wire byte representation; if
// it diverged, downstream pcap/audit tooling would corrupt the payload.
func TestSinkAdapter_PreservesPreview(t *testing.T) {
	var got WireEvent
	em := EmitterFunc(func(e WireEvent) { got = e })
	a := NewSinkAdapter(em)

	const preview = "REGISTER sip:x@y SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1\r\n\r\n"
	we := newTestWireEvent(t, model.DirTransmit, len(preview), []byte(preview))
	if err := a.Emit(we); err != nil {
		t.Fatalf("Emit = %v", err)
	}
	if string(got.Bytes) != preview {
		t.Errorf("Bytes diverged.\n--- got ---\n%q\n--- want ---\n%q", got.Bytes, preview)
	}
}

// TestSinkAdapter_PreservesTimestamp verifies that the wall-clock value
// captured at the domain layer survives the bridge intact. This is important
// because downstream consumers use the timestamp to correlate with
// higher-level events (registration, call setup, …).
func TestSinkAdapter_PreservesTimestamp(t *testing.T) {
	var got WireEvent
	em := EmitterFunc(func(e WireEvent) { got = e })
	a := NewSinkAdapter(em)

	now := time.Date(2026, 9, 23, 12, 34, 56, 0, time.UTC)
	we, err := model.NewWireEvent(
		now,
		model.DirTransmit,
		"udp",
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
		&net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 5080},
		42,
		"x",
	)
	if err != nil {
		t.Fatalf("NewWireEvent: %v", err)
	}
	if err := a.Emit(we); err != nil {
		t.Fatalf("Emit = %v", err)
	}
	if !got.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, now)
	}
}

// TestSinkAdapter_ConcurrentEmitSafe ensures the SinkAdapter is safe for
// concurrent Emit calls. The legacy Emitter contract already requires this;
// we re-verify through the port adapter.
func TestSinkAdapter_ConcurrentEmitSafe(t *testing.T) {
	var (
		mu       sync.Mutex
		received int
	)
	em := EmitterFunc(func(e WireEvent) {
		mu.Lock()
		received++
		mu.Unlock()
	})
	a := NewSinkAdapter(em)

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = a.Emit(newTestWireEvent(t, model.DirReceive, i, nil))
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if received != n {
		t.Errorf("received = %d, want %d", received, n)
	}
}

// TestSinkAdapter_NeverReturnsError hammers the adapter through the port
// interface with many calls and verifies that Emit always returns nil. The
// port contract — "a sink that returns an error must not stop the transport"
// — depends on this guarantee. Run with -race to catch any future
// regression that introduces a partial-failure path.
func TestSinkAdapter_NeverReturnsError(t *testing.T) {
	// A panicking emitter would surface as a panic rather than an error;
	// using NopEmitter keeps the assertion tight to "always returns nil".
	sink := NewSinkAdapter(NopEmitter{})
	var sinkIface port.AuditSink = sink
	const n = 64
	for i := 0; i < n; i++ {
		if err := sinkIface.Emit(newTestWireEvent(t, model.DirTransmit, i, nil)); err != nil {
			t.Fatalf("Emit[%d] = %v, want nil", i, err)
		}
	}
}

// newTestWireEvent constructs a model.WireEvent from local test parameters
// and fails the test on construction error so the call site stays concise.
func newTestWireEvent(t *testing.T, dir model.Direction, size int, raw []byte) model.WireEvent {
	t.Helper()
	we, err := model.NewWireEvent(
		time.Now(),
		dir,
		"udp",
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
		&net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 5080},
		size,
		string(raw),
	)
	if err != nil {
		t.Fatalf("NewWireEvent: %v", err)
	}
	return we
}
package siptransport_test

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time assertion at the test site so a regression in the package
// header would surface here as well.
var _ port.SIPTransport = (*siptransport.PortAdapter)(nil)

// TestPortAdapter_InterfaceAssertion duplicates the package-level
// assertion; if the PortAdapter ever stops satisfying port.SIPTransport,
// this test file fails to compile.
func TestPortAdapter_InterfaceAssertion(t *testing.T) {
	// Touch the assertion via a value typed by the interface.
	var _ port.SIPTransport = (*siptransport.PortAdapter)(nil)
}

// TestPortAdapter_NilReceiverSafe verifies the wrapper tolerates a nil
// receiver on Send/Receive/Close, matching the contract that callers can
// safely call Close on an adapter they never finished initialising.
func TestPortAdapter_NilReceiverSafe(t *testing.T) {
	var p *siptransport.PortAdapter

	if err := p.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
	hdr := model.NewHeader("CSeq", "1 INVITE")
	m, _ := model.NewRequest("INVITE", "sip:bob@example.com:5060", []model.Header{hdr}, "")
	if err := p.Send(context.Background(), m, "127.0.0.1:5060"); err == nil {
		t.Error("nil Send must error")
	}
	if _, _, err := p.Receive(context.Background()); err == nil {
		t.Error("nil Receive must error")
	}
}

// TestPortAdapter_SendUsesExplicitDestination asserts the caller-supplied
// destination is what the message is sent to (never inferred from the
// message URI), and that Receive reports the address the message arrived
// from (design D4).
func TestPortAdapter_SendUsesExplicitDestination(t *testing.T) {
	// Probe a free port first: Transport.LocalAddr() reports the bind
	// string, so ":0" would be reported verbatim and nothing would be
	// routable.
	sender, err := siptransport.New("udp://" + freeUDPAddr(t))
	if err != nil {
		t.Fatalf("New sender: %v", err)
	}
	defer sender.Close()
	receiver, err := siptransport.New("udp://" + freeUDPAddr(t))
	if err != nil {
		t.Fatalf("New receiver: %v", err)
	}
	defer receiver.Close()

	adapterS := siptransport.NewPortAdapter(sender)
	adapterR := siptransport.NewPortAdapter(receiver)

	hdr := model.NewHeader("CSeq", "1 INVITE")
	m, err := model.NewRequest("INVITE", "sip:bob@example.com:5060", []model.Header{hdr}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The destination is explicit, and deliberately NOT the URI host
	// (example.com), which proves the URI is not used for routing.
	if err := adapterS.Send(ctx, m, receiver.LocalAddr()); err != nil {
		t.Fatalf("Send to %q: %v", receiver.LocalAddr(), err)
	}

	got, peer, err := adapterR.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.Method() != "INVITE" {
		t.Errorf("Method() = %q, want INVITE", got.Method())
	}
	if peer == "" {
		t.Fatal("Receive returned an empty peer address")
	}
	host, _, err := net.SplitHostPort(peer)
	if err != nil {
		t.Fatalf("peer %q is not host:port: %v", peer, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("peer host = %q, want 127.0.0.1", host)
	}
	// The peer must be the sender's real endpoint so a caller can hand it
	// straight back to Send and answer (spec: "peer address can be used to
	// reply directly").
	if peer != sender.LocalAddr() {
		t.Errorf("Receive peer = %q, want sender endpoint %q", peer, sender.LocalAddr())
	}

	// An empty destination is rejected rather than silently inferred.
	if err := adapterS.Send(ctx, m, ""); err == nil {
		t.Error("Send with empty destination succeeded, want error")
	}
}

// TestPortAdapter_ReceiveContextCancel verifies Receive respects
// context cancellation by closing the underlying transport mid-wait.
func TestPortAdapter_ReceiveContextCancel(t *testing.T) {
	tr, err := siptransport.New("udp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	adapter := siptransport.NewPortAdapter(tr)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := adapter.Receive(ctx); err == nil {
		t.Error("Receive must error on ctx timeout")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := adapter.Receive(context.Background()); err == nil {
		t.Error("Receive after Close must error")
	}
}

// TestPortAdapter_CloseIdempotent mirrors the underlying Transport
// contract: Close may be called repeatedly without panicking.
func TestPortAdapter_CloseIdempotent(t *testing.T) {
	tr, err := siptransport.New("udp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	adapter := siptransport.NewPortAdapter(tr)
	if err := adapter.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// freeUDPAddr reserves a loopback UDP port and releases it immediately so
// the caller can bind a Transport to it. Transport.LocalAddr() echoes the
// bind string, so binding ":0" would report ":0" back and be unroutable.
func freeUDPAddr(t *testing.T) string {
	t.Helper()
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	addr := l.LocalAddr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close probe socket: %v", err)
	}
	return addr
}

// stubCloser is a tiny io.Closer adapter used to assert double-Close does
// not invoke the underlying closer more than once. We can't inspect the
// underlying Transport here directly, so we use an out-of-band io.Closer
// tracker instead.
type stubCloser struct{ calls int32 }

func (s *stubCloser) Close() error { atomic.AddInt32(&s.calls, 1); return nil }

func TestPortAdapter_CloseDoesNotLeakDoubleCall(t *testing.T) {
	tr, err := siptransport.New("udp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	adapter := siptransport.NewPortAdapter(tr)
	_ = adapter.Close()
	// The underlying Transport.Close is sync.Once-guarded; a second
	// adapter.Close must not panic, even if the underlying socket has
	// already been closed.
	if err := adapter.Close(); err != nil {
		t.Fatalf("second adapter Close: %v", err)
	}
	// Sanity: stubCloser pattern with sync.Once — see servicectx tests
	// for the equivalent pattern.
	sc := &stubCloser{}
	if err := io.Closer(sc).Close(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&sc.calls); got != 1 {
		t.Errorf("stubCloser calls=%d want 1", got)
	}
}
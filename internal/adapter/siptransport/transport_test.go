package siptransport_test

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghettovoice/gosip/sip"

	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
	isip "github.com/your-org/gb28181-simulator/internal/adapter/sip"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
)

// TestMain installs a deterministic branch generator once for the whole
// package so that parallel tests don't race on the global DefaultBranch
// variable in internal/sip.
func TestMain(m *testing.M) {
	isip.SetDefaultBranchGenerator(isip.NewBranchGeneratorWith(func() string {
		return "z9hG4bK-testbranch"
	}))
	os.Exit(m.Run())
}

// newTestPair creates two Transport instances bound to loopback addresses.
// It returns the pair plus a cleanup function.
func newTestPair(t *testing.T) (*siptransport.Transport, *siptransport.Transport, func()) {
	t.Helper()
	l1, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l2, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		func() { _ = l1.Close() }()
		t.Fatal(err)
	}
	addr1 := l1.LocalAddr().String()
	addr2 := l2.LocalAddr().String()
	func() { _ = l1.Close() }()
	func() { _ = l2.Close() }()

	s1, err := siptransport.New("udp://" + addr1)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := siptransport.New("udp://"+addr2, siptransport.WithReceiveBuffer(4))
	if err != nil {
		func() { _ = s1.Close() }()
		t.Fatal(err)
	}
	cleanup := func() {
		func() { _ = s1.Close() }()
		func() { _ = s2.Close() }()
	}
	return s1, s2, cleanup
}

// buildRequest is a small helper that constructs a SIP INVITE via the
// internal/sip builder. Keeping it in the test file means we exercise the
// real builder (rather than hand-rolled gosip literals). The branch
// generator is installed once in TestMain.
func buildRequest(t *testing.T) sip.Request {
	t.Helper()
	req, err := isip.BuildRequest(
		sip.RequestMethod("INVITE"),
		"sip:34020000001320000001@127.0.0.1:5060",
		isip.WithFrom("sip:34020000001320000001@127.0.0.1"),
		isip.WithTo("sip:34020000001320000001@127.0.0.1"),
		isip.WithCallID("call-1234567890"),
		isip.WithCSeq(1),
		isip.WithViaHost("127.0.0.1"),
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	return req
}

// hostPort splits an "host:port" string and returns just the port.
func portOnly(addr string) string {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return p
}

func TestTransport_MultiInstance(t *testing.T) {
	t.Parallel()
	s1, s2, cleanup := newTestPair(t)
	defer cleanup()

	req := buildRequest(t)
	done := make(chan sip.Message, 1)
	errCh := make(chan error, 1)
	go func() {
		msg, _, err := s2.Receive(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		done <- msg
	}()

	if err := s1.Send(req, net.JoinHostPort("127.0.0.1", portOnly(s2.LocalAddr()))); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-done:
		if msg == nil {
			t.Fatal("expected non-nil message")
		}
		req, ok := msg.(sip.Request)
		if !ok {
			t.Fatalf("expected Request, got %T", msg)
		}
		if string(req.Method()) != "INVITE" {
			t.Fatalf("expected INVITE, got %s", req.Method())
		}
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("receive timed out — second instance not seeing packets")
	}
}

func TestTransport_MultiInstanceSimultaneous(t *testing.T) {
	t.Parallel()
	s1, s2, cleanup := newTestPair(t)
	defer cleanup()

	var received int32
	const n = 20
	recvCh := make(chan sip.Message, n*4)
	errCh := make(chan error, 1)
	stop := make(chan struct{})
	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			msg, _, err := s2.Receive(context.Background())
			if err != nil {
				if err == io.EOF || strings.Contains(err.Error(), "closed") {
					return
				}
				errCh <- err
				return
			}
			recvCh <- msg
			if atomic.AddInt32(&received, 1) >= n {
				return
			}
		}
	}()

	// UDP loopback is unreliable on shared CI runners, so re-send the
	// whole batch in a separate goroutine until the count is reached.
	// We stop sending as soon as `received >= n` so the receiver can exit
	// cleanly without seeing further datagrams during cleanup (which
	// would otherwise race against Transport.Close closing t.out).
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		deadline := time.Now().Add(30 * time.Second)
		for atomic.LoadInt32(&received) < n {
			if time.Now().After(deadline) {
				return
			}
			for i := 0; i < n && atomic.LoadInt32(&received) < n; i++ {
				if err := s1.Send(buildRequest(t), s2.LocalAddr()); err != nil {
					t.Errorf("send: %v", err)
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Wait for receiver to reach n (it returns when reached, closing
	// recvDone and stopping the receive loop).
	select {
	case <-recvDone:
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(30 * time.Second):
		close(stop)
		t.Fatalf("only received %d/%d messages", atomic.LoadInt32(&received), n)
	}

	// Wait for the sender goroutine so it doesn't run concurrently with
	// cleanup. The receiver already exited (recvDone closed).
	<-sendDone
	// Tell any receive-side select to break if it's still in its default
	// poll between attempts; safe to close exactly once.
	select {
	case <-stop:
	default:
		close(stop)
	}

	if got := atomic.LoadInt32(&received); got != int32(n) {
		t.Fatalf("expected exactly %d messages, got %d", n, got)
	}
	// Drain any extra datagrams that arrived during the retry loop.
	for {
		select {
		case <-recvCh:
		default:
			return
		}
	}
}

func TestTransport_ReceiveAfterClose(t *testing.T) {
	t.Parallel()
	s1, _, cleanup := newTestPair(t)
	cleanup() // close both first
	_, _, err := s1.Receive(context.Background())
	if err != io.ErrClosedPipe {
		t.Fatalf("expected io.ErrClosedPipe after Close, got %v", err)
	}
}

func TestTransport_SendAfterClose(t *testing.T) {
	t.Parallel()
	s1, s2, cleanup := newTestPair(t)
	cleanup() // close both first
	req := buildRequest(t)
	err := s1.Send(req, s2.LocalAddr())
	if err != io.ErrClosedPipe {
		t.Fatalf("expected io.ErrClosedPipe, got %v", err)
	}
}

func TestTransport_LocalAddr(t *testing.T) {
	t.Parallel()
	s, _, cleanup := newTestPair(t)
	defer cleanup()
	if !strings.HasPrefix(s.LocalAddr(), "127.0.0.1:") {
		t.Fatalf("expected loopback address, got %s", s.LocalAddr())
	}
}

func TestTransport_Protocol(t *testing.T) {
	t.Parallel()
	s, _, cleanup := newTestPair(t)
	defer cleanup()
	if s.Protocol() != "udp" {
		t.Fatalf("expected udp, got %s", s.Protocol())
	}
}

func TestTransport_AuditHook_InjectEmitter(t *testing.T) {
	// Note: not t.Parallel() because we mutate the process-global emitter.
	var mu sync.Mutex
	var events []audit.WireEvent
	emitter := audit.EmitterFunc(func(e audit.WireEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	})
	audit.SetEmitter(emitter)
	defer audit.SetEmitter(nil) // reset to nop after test

	s1, s2, cleanup := newTestPair(t)
	defer cleanup()

	req := buildRequest(t)
	done := make(chan struct{})
	go func() {
		_, _, _ = s2.Receive(context.Background())
		close(done)
	}()

	if err := s1.Send(req, s2.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("receive timed out")
	}

	// Give the receive goroutine a brief moment to publish the audit event.
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) < 1 {
		t.Fatalf("expected at least 1 audit event, got %d", len(events))
	}
	// At least one event must be a transmit ("t")
	foundTx := false
	for _, e := range events {
		if e.Direction == audit.DirTransmit {
			foundTx = true
			break
		}
	}
	if !foundTx {
		var dirs []string
		for _, e := range events {
			dirs = append(dirs, e.Direction)
		}
		t.Fatalf("expected a transmit event, got directions: %v", dirs)
	}
}

func TestTransport_NodeIDPropagatesToAudit(t *testing.T) {
	// Note: not t.Parallel() because we mutate the process-global emitter.
	var mu sync.Mutex
	var events []audit.WireEvent
	emitter := audit.EmitterFunc(func(e audit.WireEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	})
	audit.SetEmitter(emitter)
	defer audit.SetEmitter(nil)

	nodeID := "34020000001320000001"
	s1, s2, cleanup := newNodeIDPair(t, nodeID)
	defer cleanup()

	req := buildRequest(t)
	done := make(chan struct{})
	go func() {
		_, _, _ = s2.Receive(context.Background())
		close(done)
	}()

	if err := s1.Send(req, s2.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("receive timed out")
	}

	// Give the receive goroutine a brief moment to publish the audit event.
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 audit events (tx + rx), got %d", len(events))
	}
	for i, e := range events {
		if e.NodeID != nodeID {
			t.Fatalf("event[%d]: NodeID = %q, want %q", i, e.NodeID, nodeID)
		}
	}
}

func TestTransport_NodeIDEmptyByDefault(t *testing.T) {
	var mu sync.Mutex
	var events []audit.WireEvent
	emitter := audit.EmitterFunc(func(e audit.WireEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	})
	audit.SetEmitter(emitter)
	defer audit.SetEmitter(nil)

	s1, s2, cleanup := newTestPair(t)
	defer cleanup()

	req := buildRequest(t)
	done := make(chan struct{})
	go func() {
		_, _, _ = s2.Receive(context.Background())
		close(done)
	}()

	if err := s1.Send(req, s2.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("receive timed out")
	}

	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 audit events (tx + rx), got %d", len(events))
	}
	for i, e := range events {
		if e.NodeID != "" {
			t.Fatalf("event[%d]: NodeID = %q, want empty", i, e.NodeID)
		}
	}
}

// newNodeIDPair creates two transports both tagged with nodeID. It mirrors
// newTestPair but exercises the WithNodeID option.
func newNodeIDPair(t *testing.T, nodeID string) (*siptransport.Transport, *siptransport.Transport, func()) {
	t.Helper()
	l1, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l2, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		func() { _ = l1.Close() }()
		t.Fatal(err)
	}
	addr1 := l1.LocalAddr().String()
	addr2 := l2.LocalAddr().String()
	func() { _ = l1.Close() }()
	func() { _ = l2.Close() }()

	s1, err := siptransport.New("udp://"+addr1, siptransport.WithNodeID(nodeID))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := siptransport.New("udp://"+addr2, siptransport.WithNodeID(nodeID))
	if err != nil {
		func() { _ = s1.Close() }()
		t.Fatal(err)
	}
	cleanup := func() {
		func() { _ = s1.Close() }()
		func() { _ = s2.Close() }()
	}
	return s1, s2, cleanup
}

func TestAudit_RedactAuthHeader(t *testing.T) {
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
			name: "RFC 2617 WWW-Authenticate",
			in:   `WWW-Authenticate: Digest realm="x", nonce="abc", response="deadbeef1234567890"`,
			want: `WWW-Authenticate: Digest realm="x", nonce="abc", response="***REDACTED***"`,
		},
		{
			name: "RFC 7616 (algorithm case-insensitive)",
			in:   `Authorization: Digest username="alice", Response="cafef00dcafef00dcafef00dcafef00c"`,
			want: `Authorization: Digest username="alice", Response="***REDACTED***"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(audit.RedactAuthHeader([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

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

	isip "github.com/your-org/gb28181-simulator/internal/adapter/sip"
	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
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
		l1.Close()
		t.Fatal(err)
	}
	addr1 := l1.LocalAddr().String()
	addr2 := l2.LocalAddr().String()
	l1.Close()
	l2.Close()

	s1, err := siptransport.New("udp://" + addr1)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := siptransport.New("udp://"+addr2, siptransport.WithReceiveBuffer(4))
	if err != nil {
		s1.Close()
		t.Fatal(err)
	}
	cleanup := func() {
		s1.Close()
		s2.Close()
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
		msg, err := s2.Receive(context.Background())
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
	recvCh := make(chan sip.Message, n)
	errCh := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			msg, err := s2.Receive(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			recvCh <- msg
			if atomic.AddInt32(&received, 1) >= n {
				close(stop)
				return
			}
		}
	}()

	for i := 0; i < n; i++ {
		req := buildRequest(t)
		addr := s2.LocalAddr()
		if err := s1.Send(req, addr); err != nil {
			t.Fatal(err)
		}
	}
	// Wait for the receiver to count to n.
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&received) < n {
		select {
		case <-deadline:
			t.Fatalf("only received %d/%d messages", atomic.LoadInt32(&received), n)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if len(recvCh) != n {
		t.Fatalf("expected %d messages, got %d", n, len(recvCh))
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestTransport_ReceiveAfterClose(t *testing.T) {
	t.Parallel()
	s1, _, cleanup := newTestPair(t)
	cleanup() // close both first
	_, err := s1.Receive(context.Background())
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
		_, _ = s2.Receive(context.Background())
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
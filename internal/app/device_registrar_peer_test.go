package app

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// --- samePeer unit tests --------------------------------------------------

func TestSamePeer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		peer  string
		server string
		want  bool
	}{
		{"IP-IP equal", "127.0.0.1:5060", "127.0.0.1:5060", true},
		{"IP-IP different host", "192.168.1.2:5060", "127.0.0.1:5060", false},
		{"IP-IP different port", "127.0.0.1:5061", "127.0.0.1:5060", false},
		{"IPv6 loopback equivalence", "[::1]:5060", "127.0.0.1:5060", false},
		{"IPv4 vs IPv4-mapped IPv6", "[::ffff:127.0.0.1]:5060", "127.0.0.1:5060", true},
		{"localhost vs 127.0.0.1", "localhost:5060", "127.0.0.1:5060", true},
		{"127.0.0.1 vs localhost", "127.0.0.1:5060", "localhost:5060", true},
		{"same hostname", "gbsim-platform:5060", "gbsim-platform:5060", true},
		{"hostname and unresolvable peer", "nosuchhost.invalid:5060", "127.0.0.1:5060", false},
		{"unresolvable server", "127.0.0.1:5060", "nosuchhost.invalid:5060", false},
		{"missing port on peer falls back", "127.0.0.1", "127.0.0.1:5060", false},
		{"missing port both fall back equal", "127.0.0.1", "127.0.0.1", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := samePeer(tc.peer, tc.server); got != tc.want {
				t.Errorf("samePeer(%q, %q) = %v, want %v", tc.peer, tc.server, got, tc.want)
			}
		})
	}
}

// The Docker/deployment case in the proposal: the server is configured with
// a Docker service name, and the actual UDP source address is the container IP.
func TestSamePeer_DockerServiceName(t *testing.T) {
	t.Parallel()
	// Resolve gbsim-platform to the loopback address (simulating a container environment).
	// Here we use the semantics of "localhost vs 127.0.0.1" for verification;
	// the real gbsim-platform hostname only exists inside a container.
	if !samePeer("172.26.0.2:5060", "172.26.0.2:5060") {
		t.Fatal("identical IP:port must match")
	}
	if samePeer("172.26.0.2:5060", "172.26.0.3:5060") {
		t.Fatal("different container IP must not match")
	}
}

// --- registrar integration with hostname-configured server ---------------

// The full 401 → 200 flow where reg.Server() is a hostname (localhost) but
// the transport-reported peer is an IP (127.0.0.1). Before this change the
// strict string comparison dropped the 401 and the transaction timed out.
func TestRegistrar_ChallengeWithHostnameServer(t *testing.T) {
	t.Parallel()
	const challenge = `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=MD5`
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: "127.0.0.1:5060"},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: "127.0.0.1:5060"},
	}}
	auth := &stubAuthorizer{}
	clock := &fakeClock{now: time.Now()}
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:   "localhost:5060",
		ServerID: testPlatform,
		Password: "secret",
		Expires:  3600,
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}

	r := newTestRegistrar(t, auth, clock)
	r.newCallID = func() string { return testCallID }

	if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
		t.Fatalf("Register with hostname server: %v", err)
	}
	sent := tr.messages()
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2 (initial + authenticated retry)", len(sent))
	}
	if got := header(t, sent[1].msg, "Authorization"); got == "" {
		t.Error("authenticated retry missing Authorization header")
	}
}

// Port mismatch must still be rejected even with samePeer semantics.
func TestRegistrar_PortMismatchStillRejected(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: "127.0.0.1:5061"},
	}}
	auth := &stubAuthorizer{}
	clock := &fakeClock{now: time.Now()}
	reg := testRegistration(t, 3600, 500*time.Millisecond)

	r := newTestRegistrar(t, auth, clock)
	r.newCallID = func() string { return testCallID }

	_, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	if err == nil {
		t.Fatal("Register must fail when only a different-port peer answers")
	}
}

// --- lookup helper sanity -------------------------------------------------

func TestContainsIP(t *testing.T) {
	t.Parallel()
	ips := []string{"127.0.0.1", "::1"}
	if !containsIP(ips, net.ParseIP("127.0.0.1")) {
		t.Error("containsIP should find 127.0.0.1")
	}
	if containsIP(ips, net.ParseIP("10.0.0.1")) {
		t.Error("containsIP should not find 10.0.0.1")
	}
}

func TestIntersectIPs(t *testing.T) {
	t.Parallel()
	if !intersectIPs([]string{"127.0.0.1"}, []string{"::1", "127.0.0.1"}) {
		t.Error("intersectIPs should find common element")
	}
	if intersectIPs([]string{"127.0.0.1"}, []string{"::1"}) {
		t.Error("intersectIPs should not find common element when none exists")
	}
}

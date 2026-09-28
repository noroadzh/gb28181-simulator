package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// --- test doubles ---------------------------------------------------------

// scriptedTransport is a port.SIPTransport that records what was sent and
// replays a script of incoming messages. An empty script makes Receive
// block until the context is done, which is how the timeout path is
// exercised without sleeping.
type scriptedTransport struct {
	mu   sync.Mutex
	sent []sentMessage
	in   []incoming
}

type sentMessage struct {
	msg model.Message
	dst string
}

type incoming struct {
	msg  model.Message
	peer string
}

func (t *scriptedTransport) Send(_ context.Context, msg model.Message, dst string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = append(t.sent, sentMessage{msg: msg, dst: dst})
	return nil
}

func (t *scriptedTransport) Receive(ctx context.Context) (model.Message, string, error) {
	t.mu.Lock()
	if len(t.in) > 0 {
		next := t.in[0]
		t.in = t.in[1:]
		t.mu.Unlock()
		return next.msg, next.peer, nil
	}
	t.mu.Unlock()
	<-ctx.Done()
	return model.Message{}, "", ctx.Err()
}

func (t *scriptedTransport) Close() error   { return nil }
func (t *scriptedTransport) LocalAddr() string { return "127.0.0.1:5060" }

func (t *scriptedTransport) messages() []sentMessage {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]sentMessage, len(t.sent))
	copy(out, t.sent)
	return out
}

var _ port.SIPTransport = (*scriptedTransport)(nil)

// stubAuthorizer returns a fixed Authorization header and records the
// challenge it was asked about.
type stubAuthorizer struct {
	lastChallenge string
	lastMethod    string
	lastURI       string
	err           error
}

func (a *stubAuthorizer) Authorize(challenge string, cred model.Credentials, method, uri string) (model.Header, error) {
	a.lastChallenge = challenge
	a.lastMethod = method
	a.lastURI = uri
	if a.err != nil {
		return model.Header{}, a.err
	}
	return model.NewHeader("Authorization", `Digest username="`+cred.Username()+`", response="stub"`), nil
}

var _ port.Authorizer = (*stubAuthorizer)(nil)

// --- helpers --------------------------------------------------------------

const (
	testDevice   = "34020000011310000001"
	testPlatform = "34020000002000000001"
	testServer   = "127.0.0.1:5060"
	testCallID   = "register-callid-test"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testRegistration(t *testing.T, expires uint32, timeout time.Duration) model.Registration {
	t.Helper()
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:   testServer,
		ServerID: testPlatform,
		Password: "secret",
		Expires:  expires,
		Timeout:  timeout,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	return reg
}

func registeredNode(t *testing.T, reg model.Registration) model.Node {
	t.Helper()
	profile := testProfile(t, testDevice, "127.0.0.1:15060")
	profile, err := profile.WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	return model.NewNode(profile)
}

// response builds an incoming SIP response carrying the given headers.
func response(t *testing.T, status int, hdrs ...model.Header) model.Message {
	t.Helper()
	msg, err := model.NewResponse(status, "OK", hdrs, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	return msg
}

func callIDHeader(id string) model.Header { return model.NewHeader("Call-ID", id) }

func header(t *testing.T, msg model.Message, name string) string {
	t.Helper()
	h, ok := msg.Header(name)
	if !ok {
		t.Fatalf("%s header missing from %s", name, msg)
	}
	return h.Value()
}

func newTestRegistrar(t *testing.T, auth port.Authorizer, clock port.Clock) *Registrar {
	t.Helper()
	r, err := NewRegistrar(auth, clock, discardLogger())
	if err != nil {
		t.Fatalf("NewRegistrar: %v", err)
	}
	return r
}

// --- tests ----------------------------------------------------------------

func TestNewRegistrar_RequiresAuthorizer(t *testing.T) {
	t.Parallel()
	if _, err := NewRegistrar(nil, nil, nil); err == nil {
		t.Fatal("NewRegistrar without an Authorizer must fail")
	}
}

// The happy path without a challenge: some platforms answer 200 OK
// immediately.
func TestRegistrar_ImmediateSuccess(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	auth := &stubAuthorizer{}
	clock := &fakeClock{now: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}
	reg := testRegistration(t, 3600, 2*time.Second)

	r := newTestRegistrar(t, auth, clock)
	r.newCallID = func() string { return testCallID }

	result, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.Server() != testServer || result.GrantedExpiry() != 3600 {
		t.Errorf("result = %s, want server %s and granted 3600", result, testServer)
	}
	if !result.RegisteredAt().Equal(clock.now) {
		t.Errorf("registeredAt = %v, want the injected clock %v", result.RegisteredAt(), clock.now)
	}
	sent := tr.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	if sent[0].dst != testServer {
		t.Errorf("dst = %q, want %q", sent[0].dst, testServer)
	}
	msg := sent[0].msg
	if got := header(t, msg, "Expires"); got != "3600" {
		t.Errorf("Expires = %q, want 3600", got)
	}
	if got := header(t, msg, "CSeq"); got != "1 REGISTER" {
		t.Errorf("CSeq = %q, want \"1 REGISTER\"", got)
	}
	if got := header(t, msg, "Call-ID"); got != testCallID {
		t.Errorf("Call-ID = %q, want %q", got, testCallID)
	}
	if got := header(t, msg, "Contact"); got != "<sip:"+testDevice+"@127.0.0.1:15060>" {
		t.Errorf("Contact = %q", got)
	}
	if got := header(t, msg, "To"); got != "<sip:"+testDevice+"@3402000000>" {
		t.Errorf("To = %q", got)
	}
	// The Request-URI names the platform at the node's domain.
	if got, want := msg.URI().String(), "sip:"+testPlatform+"@3402000000"; got != want {
		t.Errorf("Request-URI = %q, want %q", got, want)
	}
}

// The full exchange: 401 challenge, then 200 OK for the authenticated
// REGISTER.
func TestRegistrar_ChallengeThenSuccess(t *testing.T) {
	t.Parallel()
	const challenge = `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=MD5`
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	auth := &stubAuthorizer{}
	clock := &fakeClock{now: time.Now()}
	reg := testRegistration(t, 3600, 2*time.Second)

	r := newTestRegistrar(t, auth, clock)
	r.newCallID = func() string { return testCallID }

	if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	sent := tr.messages()
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(sent))
	}
	// Same transaction: identical Call-ID, incremented CSeq.
	if a, b := header(t, sent[0].msg, "Call-ID"), header(t, sent[1].msg, "Call-ID"); a != b {
		t.Errorf("Call-ID changed inside the transaction: %q vs %q", a, b)
	}
	if got := header(t, sent[0].msg, "CSeq"); got != "1 REGISTER" {
		t.Errorf("first CSeq = %q", got)
	}
	second := sent[1].msg
	if got := header(t, second, "CSeq"); got != "2 REGISTER" {
		t.Errorf("second CSeq = %q, want \"2 REGISTER\"", got)
	}
	if _, ok := second.Header("Authorization"); !ok {
		t.Errorf("the authenticated REGISTER carries no Authorization: %s", second)
	}
	if auth.lastChallenge != challenge {
		t.Errorf("authorizer got challenge %q", auth.lastChallenge)
	}
	if auth.lastMethod != "REGISTER" || auth.lastURI != "sip:"+testPlatform+"@3402000000" {
		t.Errorf("authorizer got %s %s", auth.lastMethod, auth.lastURI)
	}
}

// A platform may shorten the lifetime we asked for; its answer wins.
func TestRegistrar_PlatformShortensExpiry(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader(testCallID),
			model.NewHeader("Expires", "600")), peer: testServer},
	}}
	auth := &stubAuthorizer{}
	reg := testRegistration(t, 3600, 2*time.Second)

	r := newTestRegistrar(t, auth, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	result, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.GrantedExpiry() != 600 {
		t.Errorf("granted expiry = %d, want 600", result.GrantedExpiry())
	}
}

// A final non-2xx response ends the transaction with a failure that names
// the stage and the status code.
func TestRegistrar_Rejected(t *testing.T) {
	t.Parallel()
	const challenge = `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=MD5`
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: testServer},
		{msg: response(t, 403, callIDHeader(testCallID)), peer: testServer},
	}}
	reg := testRegistration(t, 3600, 2*time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	_, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	var regErr *RegistrationError
	if !errors.As(err, &regErr) {
		t.Fatalf("got %v, want a *RegistrationError", err)
	}
	if regErr.Stage != StageResponse || regErr.StatusCode != 403 {
		t.Errorf("stage/status = %s/%d, want response/403", regErr.Stage, regErr.StatusCode)
	}
}

// No answer at all: the transaction must end when the timeout expires,
// not hang.
func TestRegistrar_Timeout(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{} // no scripted responses
	reg := testRegistration(t, 3600, 50*time.Millisecond)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	start := time.Now()
	_, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	if err == nil {
		t.Fatal("Register succeeded with no response, want a timeout")
	}
	if !errors.Is(err, ErrRegisterTimeout) {
		t.Errorf("got %v, want ErrRegisterTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Register took %v, want it bounded by the timeout", elapsed)
	}
}

// A 401 without a challenge is unusable: retrying would send the same
// unauthenticated REGISTER again.
func TestRegistrar_401WithoutChallenge(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID)), peer: testServer},
	}}
	reg := testRegistration(t, 3600, time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	_, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	if !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("got %v, want ErrNoChallenge", err)
	}
	if sent := tr.messages(); len(sent) != 1 {
		t.Errorf("sent %d messages, want 1 (no pointless retry)", len(sent))
	}
}

// Being challenged twice means the credentials are wrong.
func TestRegistrar_ChallengedTwice(t *testing.T) {
	t.Parallel()
	const challenge = `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=MD5`
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: testServer},
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: testServer},
	}}
	reg := testRegistration(t, 3600, time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	_, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	if !errors.Is(err, ErrStillUnauthorized) {
		t.Fatalf("got %v, want ErrStillUnauthorized", err)
	}
}

// Responses that belong to another transaction, or come from another peer,
// must not conclude ours.
func TestRegistrar_IgnoresForeignResponses(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 200, callIDHeader("someone-elses-callid")), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: "127.0.0.1:9999"},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	reg := testRegistration(t, 3600, 2*time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(tr.in) != 0 {
		t.Errorf("%d scripted responses left unread", len(tr.in))
	}
}

// A provisional response must not be mistaken for an answer.
func TestRegistrar_IgnoresProvisional(t *testing.T) {
	t.Parallel()
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 100, callIDHeader(testCallID)), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	reg := testRegistration(t, 3600, 2*time.Second)
	r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// An optional GB version is written to the wire as X-GB-Ver, and omitted
// when not configured.
func TestRegistrar_GBVersionHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		present bool
	}{
		{"2022", true},
		{"", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run("version="+tc.version, func(t *testing.T) {
			t.Parallel()
			tr := &scriptedTransport{in: []incoming{
				{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
			}}
			reg, err := model.NewRegistration(model.RegistrationParams{
				Server: testServer, Password: "secret", GBVersion: tc.version,
			})
			if err != nil {
				t.Fatalf("NewRegistration: %v", err)
			}
			r := newTestRegistrar(t, &stubAuthorizer{}, &fakeClock{now: time.Now()})
			r.newCallID = func() string { return testCallID }
			if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
				t.Fatalf("Register: %v", err)
			}
			_, ok := tr.messages()[0].msg.Header("X-GB-Ver")
			if ok != tc.present {
				t.Errorf("X-GB-Ver present = %v, want %v", ok, tc.present)
			}
		})
	}
}

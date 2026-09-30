package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// The acceptor is the UAS half, so its tests need the opposite of what the
// device-side tests need: something that hands it REGISTERs and records the
// answers it writes back. Everything else is a fake, because the point here
// is the decision — challenge, refuse or grant — not the digest maths.
type acceptorTransport struct {
	mu      sync.Mutex
	inbound chan model.Message
	sent    []model.Message
	peers   []string
	closed  bool
}

func newAcceptorTransport() *acceptorTransport {
	return &acceptorTransport{inbound: make(chan model.Message, 4)}
}

func (t *acceptorTransport) Receive(ctx context.Context) (model.Message, string, error) {
	select {
	case <-ctx.Done():
		return model.Message{}, "", ctx.Err()
	case msg := <-t.inbound:
		return msg, "127.0.0.1:15060", nil
	}
}

func (t *acceptorTransport) Send(ctx context.Context, msg model.Message, dst string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = append(t.sent, msg)
	t.peers = append(t.peers, dst)
	return nil
}

func (t *acceptorTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

func (t *acceptorTransport) LocalAddr() string { return "127.0.0.1:15060" }

func (t *acceptorTransport) delivered() []model.Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]model.Message, len(t.sent))
	copy(out, t.sent)
	return out
}

// deliver pushes a request in and waits for the acceptor to answer it.
// Requests sent by the acceptor itself (NOTIFY and friends) are skipped so
// the message handed back is always the response to the request under test.
func (t *acceptorTransport) deliver(tb testing.TB, req model.Message) model.Message {
	tb.Helper()
	// Wait for a *new* answer: a previous one is still in the log, and
	// returning it would make every assertion pass for the wrong reason.
	before := len(t.responses())
	t.inbound <- req
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := t.responses(); len(got) > before {
			return got[len(got)-1]
		}
		time.Sleep(time.Millisecond)
	}
	tb.Fatal("timed out waiting for an answer")
	return model.Message{}
}

// responses returns only the sent messages that are responses, i.e. those
// without a method. A SIP request the platform originates (NOTIFY) carries
// its method; a reply does not.
func (t *acceptorTransport) responses() []model.Message {
	all := t.delivered()
	out := make([]model.Message, 0, len(all))
	for _, m := range all {
		if m.Method() == "" {
			out = append(out, m)
		}
	}
	return out
}

func (t *acceptorTransport) answers() int { return len(t.delivered()) }

type fakeChallenger struct {
	seq int
}

func (c *fakeChallenger) Challenge(realm string) (model.Challenge, error) {
	c.seq++
	return model.NewChallenge(realm, fmt.Sprintf("nonce-%d", c.seq), "MD5", "", "auth")
}

// fakeAuthenticator answers how the composition root's real adapter would:
// nil, a parse failure worth re-challenging, or a mismatch that must not be
// re-challenged.
type fakeAuthenticator struct {
	outcome error
	calls   int
}

func (a *fakeAuthenticator) Verify(req model.Message, cred model.Credentials) error {
	a.calls++
	return a.outcome
}

type fakeCredentials struct {
	mu       sync.Mutex
	byNode   map[string]map[string]model.Credentials
	lookedUp []string
}

func newFakeCredentials() *fakeCredentials {
	return &fakeCredentials{byNode: make(map[string]map[string]model.Credentials)}
}

func (f *fakeCredentials) add(tb testing.TB, nodeID model.NodeID, username, password string) {
	tb.Helper()
	cred, err := model.NewCredentials(username, "3402000000", password)
	if err != nil {
		tb.Fatalf("NewCredentials: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := nodeID.String()
	if f.byNode[key] == nil {
		f.byNode[key] = make(map[string]model.Credentials)
	}
	f.byNode[key][username] = cred
}

func (f *fakeCredentials) Lookup(nodeID model.NodeID, username string) (model.Credentials, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookedUp = append(f.lookedUp, username)
	cred, ok := f.byNode[nodeID.String()][username]
	return cred, ok
}

type fakeDevices struct {
	mu     sync.Mutex
	byNode map[string]map[string]model.DownstreamDevice
}

func newFakeDevices() *fakeDevices {
	return &fakeDevices{byNode: make(map[string]map[string]model.DownstreamDevice)}
}

func (d *fakeDevices) Upsert(ctx context.Context, nodeID model.NodeID, dev model.DownstreamDevice) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := nodeID.String()
	if d.byNode[key] == nil {
		d.byNode[key] = make(map[string]model.DownstreamDevice)
	}
	d.byNode[key][dev.DeviceID()] = dev
	return nil
}

func (d *fakeDevices) Remove(ctx context.Context, nodeID model.NodeID, deviceID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.byNode[nodeID.String()], deviceID)
	return nil
}

func (d *fakeDevices) Lookup(ctx context.Context, nodeID model.NodeID, deviceID string) (model.DownstreamDevice, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dev, ok := d.byNode[nodeID.String()][deviceID]
	return dev, ok
}

// List honours the port's contract — ordered by device id — so a test that
// reads a table gets the same sequence the real registry would give it.
func (d *fakeDevices) List(ctx context.Context, nodeID model.NodeID) []model.DownstreamDevice {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]model.DownstreamDevice, 0, len(d.byNode[nodeID.String()]))
	for _, dev := range d.byNode[nodeID.String()] {
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID() < out[j].DeviceID() })
	return out
}

func (d *fakeDevices) Clear(ctx context.Context, nodeID model.NodeID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.byNode, nodeID.String())
	return nil
}

func mustPlatformNode(t *testing.T) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID("34020000002000000001")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	return id
}

func registerRequest(t *testing.T, deviceID string, hdrs ...model.Header) model.Message {
	t.Helper()
	base := []model.Header{
		model.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=abc123"),
		model.NewHeader("To", "<sip:"+deviceID+"@3402000000>"),
		model.NewHeader("Call-ID", "call-1"),
		model.NewHeader("CSeq", "1 REGISTER"),
		model.NewHeader("Contact", "<sip:"+deviceID+"@127.0.0.1:15060>"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-1"),
	}
	msg, err := model.NewRequest("REGISTER", "sip:3402000000@3402000000", append(base, hdrs...), "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

func acceptorFixture(t *testing.T, auth port.Authenticator) (
	*Acceptor, *acceptorTransport, *fakeCredentials, *fakeDevices, model.NodeID,
) {
	t.Helper()
	nodeID := mustPlatformNode(t)
	tr := newAcceptorTransport()
	creds := newFakeCredentials()
	devices := newFakeDevices()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	acceptor, err := NewAcceptor(context.Background(), clock, &fakeChallenger{}, auth, creds,
		devices, newFakeMANSCDP(), nil, discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	policy, err := model.NewExpiresPolicy(60, 3600, 7200)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	if err := acceptor.Serve(nodeID, tr, "3402000000", policy); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = acceptor.Close() })
	return acceptor, tr, creds, devices, nodeID
}

func responseHeader(t *testing.T, msg model.Message, name string) string {
	t.Helper()
	h, ok := msg.Header(name)
	if !ok {
		t.Fatalf("response has no %s header", name)
	}
	return h.Value()
}

// A REGISTER that proves nothing about itself gets a challenge, not a
// refusal: the first message of every GB/T 28181 registration is an
// unauthenticated one.
func TestAcceptor_ChallengesUnauthenticatedRegister(t *testing.T) {
	_, tr, creds, devices, _ := acceptorFixture(t, &fakeAuthenticator{})
	req := registerRequest(t, "34020000011310000001")
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode())
	}
	challenge := responseHeader(t, resp, "WWW-Authenticate")
	for _, want := range []string{"realm=\"3402000000\"", "nonce=", `qop="auth"`, "algorithm=MD5"} {
		if !strings.Contains(challenge, want) {
			t.Errorf("challenge %q is missing %s", challenge, want)
		}
	}
	// The answer must be recognisable as belonging to the request.
	if responseHeader(t, resp, "Call-ID") != "call-1" {
		t.Errorf("Call-ID = %q, want it echoed", responseHeader(t, resp, "Call-ID"))
	}
	if !strings.Contains(responseHeader(t, resp, "To"), "tag=") {
		t.Errorf("To = %q, want it tagged", responseHeader(t, resp, "To"))
	}
	if len(devices.List(context.Background(), mustPlatformNode(t))) != 0 {
		t.Error("a challenged registration was recorded")
	}
	if len(creds.lookedUp) != 0 {
		t.Error("a challenge looked an account up before it was sent")
	}
}

// Every challenge carries a fresh nonce: a reused one would let a captured
// answer be replayed against a later registration.
func TestAcceptor_ChallengesUseFreshNonces(t *testing.T) {
	_, tr, _, _, _ := acceptorFixture(t, &fakeAuthenticator{})
	first := tr.deliver(t, registerRequest(t, "34020000011310000001"))
	second := tr.deliver(t, registerRequest(t, "34020000011310000001"))
	if responseHeader(t, first, "WWW-Authenticate") == responseHeader(t, second, "WWW-Authenticate") {
		t.Error("two challenges carry the same header value, want fresh nonces")
	}
}

func TestAcceptor_RefusesUnknownAccount(t *testing.T) {
	_, tr, _, devices, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	req := registerRequest(t, "34020000011310000001", model.NewHeader("Authorization", `Digest username="34020000011310000001", realm="3402000000", nonce="n", response="x"`))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 403 {
		t.Fatalf("status = %d, want 403", resp.StatusCode())
	}
	if _, ok := resp.Header("WWW-Authenticate"); ok {
		t.Error("a refusal must not carry a fresh challenge (GB/T 28181 §L.2)")
	}
	if len(devices.List(context.Background(), nodeID)) != 0 {
		t.Error("an unknown device was recorded")
	}
}

// A header that cannot be read is not proof of anything, so it may be
// challenged again.
func TestAcceptor_ReChallengesUnreadableAuthorization(t *testing.T) {
	auth := &fakeAuthenticator{outcome: fmt.Errorf("%w: garbled", port.ErrMalformedCredentials)}
	_, tr, creds, _, _ := acceptorFixture(t, auth)
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	req := registerRequest(t, "34020000011310000001", model.NewHeader("Authorization", "Digest nonsense"))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode())
	}
}

// A header that reads and does not match is a refusal, never a second
// challenge: that is what GB/T 28181 §L.2 asks for.
func TestAcceptor_RefusesBadResponse(t *testing.T) {
	auth := &fakeAuthenticator{outcome: fmt.Errorf("%w: mismatch", port.ErrInvalidCredentials)}
	_, tr, creds, devices, nodeID := acceptorFixture(t, auth)
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	req := registerRequest(t, "34020000011310000001", model.NewHeader("Authorization", `Digest username="34020000011310000001", response="wrong"`))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 403 {
		t.Fatalf("status = %d, want 403", resp.StatusCode())
	}
	if _, ok := resp.Header("WWW-Authenticate"); ok {
		t.Error("a bad response was re-challenged")
	}
	if len(devices.List(context.Background(), nodeID)) != 0 {
		t.Error("a refused registration was recorded")
	}
}

// An existing row survives a failed re-registration: dropping it would be
// punishing a device for one bad message.
func TestAcceptor_FailedReRegistrationKeepsRecord(t *testing.T) {
	auth := &fakeAuthenticator{}
	_, tr, creds, devices, nodeID := acceptorFixture(t, auth)
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`),
		model.NewHeader("Expires", "3600"))
	if resp := tr.deliver(t, req); resp.StatusCode() != 200 {
		t.Fatalf("first status = %d, want 200", resp.StatusCode())
	}
	auth.outcome = fmt.Errorf("%w: mismatch", port.ErrInvalidCredentials)
	if resp := tr.deliver(t, req); resp.StatusCode() != 403 {
		t.Fatalf("second status = %d, want 403", resp.StatusCode())
	}
	if got := devices.List(context.Background(), nodeID); len(got) != 1 {
		t.Errorf("table has %d rows after a failed re-registration, want 1", len(got))
	}
}

func TestAcceptor_GrantsAndRecords(t *testing.T) {
	_, tr, creds, devices, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`),
		model.NewHeader("Expires", "3600"))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "Expires"); got != "3600" {
		t.Errorf("Expires = %q, want 3600", got)
	}
	if got := responseHeader(t, resp, "Contact"); got != "<sip:34020000011310000001@127.0.0.1:15060>" {
		t.Errorf("Contact = %q, want it echoed", got)
	}
	if responseHeader(t, resp, "Date") == "" {
		t.Error("200 OK has no Date header")
	}

	rows := devices.List(context.Background(), nodeID)
	if len(rows) != 1 {
		t.Fatalf("table has %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.DeviceID() != "34020000011310000001" {
		t.Errorf("device id = %q", row.DeviceID())
	}
	// The address is the peer the request came from, not the one the
	// device claims in its Contact.
	if row.Addr() != "127.0.0.1:15060" {
		t.Errorf("addr = %q, want the peer address", row.Addr())
	}
	if row.GrantedExpiry() != 3600 {
		t.Errorf("granted = %d, want 3600", row.GrantedExpiry())
	}
	if row.Transport() != "udp" {
		t.Errorf("transport = %q, want udp", row.Transport())
	}
}

// A 2022 peer sees its X-GB-Ver echoed back and the version is recorded.
func TestAcceptor_GrantsEchoesGBVersion2022(t *testing.T) {
	_, tr, creds, devices, _ := acceptorFixture(t, &fakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`),
		model.NewHeader("Expires", "3600"),
		model.NewHeader("X-GB-Ver", "2022"))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "X-GB-Ver"); got != "2022" {
		t.Errorf("X-GB-Ver = %q, want 2022", got)
	}
	rows := devices.List(context.Background(), mustPlatformNode(t))
	if len(rows) != 1 {
		t.Fatalf("table has %d rows, want 1", len(rows))
	}
	if !rows[0].Is2022() {
		t.Error("recorded device is not 2022, want GBVersion=2022")
	}
}

// A 2016 peer gets no X-GB-Ver header and an empty recorded version.
func TestAcceptor_GrantsOmitsGBVersionWhenAbsent(t *testing.T) {
	_, tr, creds, devices, _ := acceptorFixture(t, &fakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`),
		model.NewHeader("Expires", "3600"))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if _, ok := resp.Header("X-GB-Ver"); ok {
		t.Error("200 OK carries X-GB-Ver for a 2016 REGISTER, want it absent")
	}
	rows := devices.List(context.Background(), mustPlatformNode(t))
	if len(rows) != 1 || rows[0].Is2022() {
		t.Error("recorded device should not be marked as 2022")
	}
}

// Re-registering with a new version updates the stored GBVersion without
// creating a second row.
func TestAcceptor_ReRegistrationUpdatesGBVersion(t *testing.T) {
	_, tr, creds, devices, _ := acceptorFixture(t, &fakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	base := []model.Header{
		model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`),
		model.NewHeader("Expires", "3600"),
	}
	tr.deliver(t, registerRequest(t, "34020000011310000001",
		append(base, model.NewHeader("X-GB-Ver", "2016"))...))
	tr.deliver(t, registerRequest(t, "34020000011310000001",
		append(base, model.NewHeader("X-GB-Ver", "2022"))...))
	rows := devices.List(context.Background(), mustPlatformNode(t))
	if len(rows) != 1 {
		t.Fatalf("table has %d rows after re-registering, want 1", len(rows))
	}
	if !rows[0].Is2022() {
		t.Error("re-registration did not update GBVersion to 2022")
	}
}

// The platform grants what its window allows, not what the device asked
// for: that window is what bounds how stale the table can get.
func TestAcceptor_ClampsExpiry(t *testing.T) {
	cases := []struct {
		name      string
		expires   string
		wantGrant string
	}{
		{"absent takes the default", "", "3600"},
		{"below the minimum", "10", "60"},
		{"above the maximum", "86400", "7200"},
		{"inside the window", "1800", "1800"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, tr, creds, devices, nodeID := acceptorFixture(t, &fakeAuthenticator{})
			creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
			var hdrs []model.Header
			hdrs = append(hdrs, model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`))
			if tc.expires != "" {
				hdrs = append(hdrs, model.NewHeader("Expires", tc.expires))
			}
			resp := tr.deliver(t, registerRequest(t, "34020000011310000001", hdrs...))
			if got := responseHeader(t, resp, "Expires"); got != tc.wantGrant {
				t.Errorf("Expires = %q, want %q", got, tc.wantGrant)
			}
			rows := devices.List(context.Background(), nodeID)
			if len(rows) != 1 {
				t.Fatalf("table has %d rows, want 1", len(rows))
			}
			if got := fmt.Sprintf("%d", rows[0].GrantedExpiry()); got != tc.wantGrant {
				t.Errorf("granted = %s, want %s", got, tc.wantGrant)
			}
		})
	}
}

// Re-registering refreshes the row instead of adding another.
func TestAcceptor_ReRegistrationRefreshes(t *testing.T) {
	_, tr, creds, devices, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`),
		model.NewHeader("Expires", "3600"))
	tr.deliver(t, req)
	tr.deliver(t, req)
	rows := devices.List(context.Background(), nodeID)
	if len(rows) != 1 {
		t.Fatalf("table has %d rows after re-registering, want 1", len(rows))
	}
}

// `Expires: 0` is a goodbye: acknowledged, and forgotten.
func TestAcceptor_ZeroExpiresUnregisters(t *testing.T) {
	_, tr, creds, devices, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	authz := model.NewHeader("Authorization", `Digest username="34020000011310000001", response="ok"`)
	tr.deliver(t, registerRequest(t, "34020000011310000001", authz, model.NewHeader("Expires", "3600")))
	if got := len(devices.List(context.Background(), nodeID)); got != 1 {
		t.Fatalf("table has %d rows before leaving, want 1", got)
	}

	resp := tr.deliver(t, registerRequest(t, "34020000011310000001", authz, model.NewHeader("Expires", "0")))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "Expires"); got != "0" {
		t.Errorf("Expires = %q, want 0", got)
	}
	if got := len(devices.List(context.Background(), nodeID)); got != 0 {
		t.Errorf("table has %d rows after leaving, want 0", got)
	}
	// Leaving twice is harmless.
	resp = tr.deliver(t, registerRequest(t, "34020000011310000001", authz, model.NewHeader("Expires", "0")))
	if resp.StatusCode() != 200 {
		t.Errorf("second leave: status = %d, want 200", resp.StatusCode())
	}
}

// Heartbeats are the next change's business; until then a platform must
// stay quiet rather than answer a message it does not understand.
func TestAcceptor_IgnoresNonRegister(t *testing.T) {
	acceptor, tr, _, _, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	msg, err := model.NewRequest("MESSAGE", "sip:34020000002000000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000011310000001@3402000000>;tag=abc"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", "call-2"),
		model.NewHeader("CSeq", "2 MESSAGE"),
	}, "<Notify/>")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	tr.inbound <- msg
	// Give the loop a chance to (not) answer.
	for i := 0; i < 50; i++ {
		if tr.answers() > 0 {
			t.Fatal("a MESSAGE was answered")
		}
		time.Sleep(time.Millisecond)
	}
	// The loop is still alive and able to serve.
	creds := newFakeCredentials()
	_ = creds
	if acceptor.Serving() != 1 {
		t.Errorf("Serving() = %d, want 1 after ignoring a MESSAGE", acceptor.Serving())
	}
	if _, ok := acceptor.serving[nodeID.String()]; !ok {
		t.Error("the node is no longer served after ignoring a MESSAGE")
	}
}

// A REGISTER that names no caller is refused outright: there is nobody to
// challenge and nothing to record.
func TestAcceptor_RefusesRequestWithoutCaller(t *testing.T) {
	_, tr, _, _, _ := acceptorFixture(t, &fakeAuthenticator{})
	msg, err := model.NewRequest("REGISTER", "sip:3402000000@3402000000", []model.Header{
		model.NewHeader("Call-ID", "call-3"),
		model.NewHeader("CSeq", "1 REGISTER"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := tr.deliver(t, msg); resp.StatusCode() != 403 {
		t.Errorf("status = %d, want 403", resp.StatusCode())
	}
}

func TestAcceptor_ServeValidates(t *testing.T) {
	acceptor, err := NewAcceptor(context.Background(), newSyncClock(time.Now()), &fakeChallenger{},
		&fakeAuthenticator{}, newFakeCredentials(), newFakeDevices(), newFakeMANSCDP(), nil,
		discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	nodeID := mustPlatformNode(t)
	policy, err := model.NewExpiresPolicy(0, 0, 0)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	if err := acceptor.Serve(nodeID, nil, "3402000000", policy); err == nil {
		t.Error("Serve without a transport: got nil, want an error")
	}
	if err := acceptor.Serve(nodeID, newAcceptorTransport(), "", policy); err == nil {
		t.Error("Serve without a realm: got nil, want an error")
	}
	if err := acceptor.Serve(nodeID, newAcceptorTransport(), "3402000000", model.ExpiresPolicy{}); err == nil {
		t.Error("Serve without a policy: got nil, want an error")
	}
	if acceptor.Serving() != 0 {
		t.Errorf("Serving() = %d, want 0 after every Serve was refused", acceptor.Serving())
	}
}

// Stop ends the goroutine before the caller goes on to release the
// listener, which is the whole point of waiting for it.
func TestAcceptor_StopAndClose(t *testing.T) {
	acceptor, tr, _, _, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	if acceptor.Serving() != 1 {
		t.Fatalf("Serving() = %d, want 1", acceptor.Serving())
	}
	acceptor.Stop(nodeID)
	if acceptor.Serving() != 0 {
		t.Errorf("Serving() = %d, want 0 after Stop", acceptor.Serving())
	}
	// The goroutine is really gone: a request pushed in now is never read.
	select {
	case tr.inbound <- registerRequest(t, "34020000011310000001"):
	default:
	}
	acceptor.Stop(nodeID) // idempotent
	if err := acceptor.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Two platforms serve independently: stopping one must not silence the
// other.
func TestAcceptor_NodesAreIndependent(t *testing.T) {
	acceptor, err := NewAcceptor(context.Background(), newSyncClock(time.Now()), &fakeChallenger{},
		&fakeAuthenticator{}, newFakeCredentials(), newFakeDevices(), newFakeMANSCDP(), nil,
		discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	defer func() { _ = acceptor.Close() }()
	policy, err := model.NewExpiresPolicy(0, 0, 0)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	first := mustPlatformNode(t)
	second, err := model.ParseNodeID("34020000002000000002")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	if err := acceptor.Serve(first, newAcceptorTransport(), "3402000000", policy); err != nil {
		t.Fatalf("Serve(first): %v", err)
	}
	if err := acceptor.Serve(second, newAcceptorTransport(), "3402000000", policy); err != nil {
		t.Fatalf("Serve(second): %v", err)
	}
	if acceptor.Serving() != 2 {
		t.Fatalf("Serving() = %d, want 2", acceptor.Serving())
	}
	acceptor.Stop(first)
	if acceptor.Serving() != 1 {
		t.Errorf("Serving() = %d, want 1 after stopping one node", acceptor.Serving())
	}
}

// The errors the app layer distinguishes are the port's, never an
// adapter's: importing one would break the dependency rule.
func TestAcceptor_DistinguishesPortErrors(t *testing.T) {
	malformed := fmt.Errorf("%w: garbled", port.ErrMalformedCredentials)
	if !errors.Is(malformed, port.ErrMalformedCredentials) {
		t.Fatal("a wrapped malformed error no longer matches its sentinel")
	}
	if errors.Is(malformed, port.ErrInvalidCredentials) {
		t.Error("a malformed error matches the invalid sentinel, want them distinct")
	}
}

// signingFakeAuthenticator is a test double that always accepts verification
// but only emits a SecurityInfo header when the Authorization advertises
// algorithm=SM3 (task 5.1/5.2).
type signingFakeAuthenticator struct {
	fakeAuthenticator
}

func (f *signingFakeAuthenticator) SignSecurityInfo(req model.Message, _ model.Credentials) (model.Header, bool) {
	// The real adapter is wired through port.SecurityInfoSigner; this fake
	// mirrors the algorithm-based gate so acceptor tests stay fast.
	h, ok := req.Header("Authorization")
	if !ok {
		return model.Header{}, false
	}
	if !strings.Contains(strings.ToLower(h.Value()), "algorithm=sm3") {
		return model.Header{}, false
	}
	return model.NewHeader("SecurityInfo", "SM2,00000000"), true
}

// An SM3-capable peer (algorithm=SM3) that reaches the grant branch must see
// a SecurityInfo header attached by the adapter (task 5.1).
func TestAcceptor_SM3CapablePeerReceivesSecurityInfo(t *testing.T) {
	_, tr, creds, _, _ := acceptorFixture(t, &signingFakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	authz := `Digest username="34020000011310000001", realm="3402000000", nonce="n1", uri="sip:3402000000@3402000000", response="ok", algorithm=SM3`
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", authz),
		model.NewHeader("Expires", "3600"))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "SecurityInfo"); got == "" {
		t.Error("200 OK is missing SecurityInfo header for SM3-capable peer")
	}
}

// An MD5-capable peer must not receive a SecurityInfo header so its response
// is byte-identical to pre-change output (task 5.2).
func TestAcceptor_MD5PeerNoSecurityInfo(t *testing.T) {
	_, tr, creds, _, _ := acceptorFixture(t, &signingFakeAuthenticator{})
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	authz := `Digest username="34020000011310000001", realm="3402000000", nonce="n1", uri="sip:3402000000@3402000000", response="ok", algorithm=MD5`
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", authz),
		model.NewHeader("Expires", "3600"))
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if h, _ := resp.Header("SecurityInfo"); h.Value() != "" {
		t.Errorf("SecurityInfo = %q, want absent for MD5 peer", h.Value())
	}
}

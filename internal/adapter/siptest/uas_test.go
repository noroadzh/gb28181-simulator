package siptest_test

import (
	"context"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	sipauth "github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptest"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

const (
	e2eDeviceA = "34020000011310000001"
	e2eDeviceB = "34020000011310000002"
	e2eDomain  = "3402000000"
	e2ePasswd  = "gb28181-secret"
	e2eServer  = "34020000002000000001"
)

// freeAddr reserves a loopback UDP address and gives it straight back, so
// a test can bind it deliberately.
func freeAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := conn.LocalAddr().String()
	if err := conn.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func headerValue(t *testing.T, m model.Message, name string) string {
	t.Helper()
	h, ok := m.Header(name)
	if !ok {
		t.Fatalf("header %q missing from %s", name, m)
	}
	return h.Value()
}

// --- UAS behaviour (task 10.1) -------------------------------------------

func TestUAS_ChallengesThenAccepts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uas, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewUAS: %v", err)
	}
	defer func() { _ = uas.Close() }()
	go func() { _ = uas.Serve(ctx) }()

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	port := siptransport.NewPortAdapter(client)

	requestURI := "sip:" + e2eServer + "@" + e2eDomain
	first, err := model.NewRequest("REGISTER", requestURI, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=abc"),
		model.NewHeader("To", "<sip:"+e2eDeviceA+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "uas-callid-1"),
		model.NewHeader("CSeq", "1 REGISTER"),
		model.NewHeader("Contact", "<sip:"+e2eDeviceA+"@"+client.LocalAddr()+">"),
		model.NewHeader("Expires", "3600"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := port.Send(ctx, first, uas.Addr()); err != nil {
		t.Fatalf("send REGISTER: %v", err)
	}

	resp, _, err := port.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if resp.StatusCode() != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode())
	}
	challenge := headerValue(t, resp, "WWW-Authenticate")
	if !strings.HasPrefix(challenge, "Digest ") {
		t.Errorf("challenge = %q, want a Digest challenge", challenge)
	}
	if headerValue(t, resp, "Call-ID") != "uas-callid-1" {
		t.Errorf("the 401 does not echo our Call-ID: %s", resp)
	}

	// Answer the challenge with the right password.
	authorizer, err := sipauth.NewAuthorizerAdapter(sipauth.NewAuthorizer(nil))
	if err != nil {
		t.Fatalf("authorizer: %v", err)
	}
	cred, err := model.NewCredentials(e2eDeviceA, e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	authHeader, err := authorizer.Authorize(challenge, cred, "REGISTER", requestURI)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	second, err := model.NewRequest("REGISTER", requestURI, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=abc"),
		model.NewHeader("To", "<sip:"+e2eDeviceA+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "uas-callid-1"),
		model.NewHeader("CSeq", "2 REGISTER"),
		model.NewHeader("Contact", "<sip:"+e2eDeviceA+"@"+client.LocalAddr()+">"),
		model.NewHeader("Expires", "3600"),
		authHeader,
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := port.Send(ctx, second, uas.Addr()); err != nil {
		t.Fatalf("send authenticated REGISTER: %v", err)
	}
	resp, _, err = port.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode(), resp)
	}
	if got := headerValue(t, resp, "Expires"); got != "3600" {
		t.Errorf("Expires = %q, want 3600", got)
	}
	if len(uas.Received()) != 2 {
		t.Errorf("UAS recorded %d messages, want 2", len(uas.Received()))
	}
}

func TestUAS_RejectsWrongPassword(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uas, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewUAS: %v", err)
	}
	defer func() { _ = uas.Close() }()
	go func() { _ = uas.Serve(ctx) }()

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	port := siptransport.NewPortAdapter(client)

	requestURI := "sip:" + e2eServer + "@" + e2eDomain
	first, err := model.NewRequest("REGISTER", requestURI, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=abc"),
		model.NewHeader("To", "<sip:"+e2eDeviceA+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "uas-callid-2"),
		model.NewHeader("CSeq", "1 REGISTER"),
		model.NewHeader("Expires", "3600"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := port.Send(ctx, first, uas.Addr()); err != nil {
		t.Fatalf("send: %v", err)
	}
	resp, _, err := port.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	challenge := headerValue(t, resp, "WWW-Authenticate")

	authorizer, err := sipauth.NewAuthorizerAdapter(sipauth.NewAuthorizer(nil))
	if err != nil {
		t.Fatalf("authorizer: %v", err)
	}
	cred, err := model.NewCredentials(e2eDeviceA, e2eDomain, "not-the-password")
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	authHeader, err := authorizer.Authorize(challenge, cred, "REGISTER", requestURI)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	second, err := model.NewRequest("REGISTER", requestURI, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=abc"),
		model.NewHeader("To", "<sip:"+e2eDeviceA+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "uas-callid-2"),
		model.NewHeader("CSeq", "2 REGISTER"),
		model.NewHeader("Expires", "3600"),
		authHeader,
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := port.Send(ctx, second, uas.Addr()); err != nil {
		t.Fatalf("send: %v", err)
	}
	resp, _, err = port.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if resp.StatusCode() != 403 {
		t.Fatalf("status = %d, want 403", resp.StatusCode())
	}
}

// --- end-to-end registration (task 10.2 / 10.3) --------------------------

// registrationService assembles the real stack: registry, lifecycle with
// real UDP listeners, and the app service with the device registrar.
func registrationService(t *testing.T) *app.NodeService {
	t.Helper()
	registry := nodereg.New()
	factory := func(addr string, _ model.NodeID) (port.SIPTransport, error) {
		tr, err := siptransport.New("udp://" + addr)
		if err != nil {
			return nil, err
		}
		return siptransport.NewPortAdapter(tr), nil
	}
	lifecycle := nodereg.NewLifecycle(registry, factory)
	svc, err := app.NewNodeService(registry, lifecycle, registry, factory, nil)
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	authorizer, err := sipauth.NewAuthorizerAdapter(sipauth.NewAuthorizer(nil))
	if err != nil {
		t.Fatalf("authorizer: %v", err)
	}
	registrar, err := app.NewRegistrar(authorizer, nil, discardLogger())
	if err != nil {
		t.Fatalf("NewRegistrar: %v", err)
	}
	if _, err := svc.WithRegistrar(registrar); err != nil {
		t.Fatalf("WithRegistrar: %v", err)
	}
	return svc
}

func deviceProfile(t *testing.T, id, addr string, reg model.Registration) model.NodeProfile {
	t.Helper()
	profile, err := model.NewNodeProfile(id, addr, e2eDomain, "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	profile, err = profile.WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	return profile
}

// A device started against the test UAS must register for real: two
// REGISTERs on the wire, then online.
func TestDeviceRegistrationOverUDP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uas, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewUAS: %v", err)
	}
	defer func() { _ = uas.Close() }()
	go func() { _ = uas.Serve(ctx) }()

	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:   uas.Addr(),
		ServerID: e2eServer,
		Password: e2ePasswd,
		Expires:  3600,
		Timeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}

	svc := registrationService(t)
	nodeAddr := freeAddr(t)
	created, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, nodeAddr, reg))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Start(ctx, created.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	node, _ := svc.Get(ctx, created.ID())
	if node.Status() != model.StatusOnline {
		t.Fatalf("status = %s, want online", node.Status())
	}
	result, ok := node.RegistrationResult()
	if !ok || result.Server() != uas.Addr() {
		t.Errorf("registration result = %v (present=%v), want server %s", result, ok, uas.Addr())
	}

	msgs := uas.Received()
	if len(msgs) != 2 {
		t.Fatalf("UAS received %d REGISTERs, want 2 (unauthenticated + authenticated)", len(msgs))
	}
	first, second := msgs[0], msgs[1]
	if _, ok := first.Header("Authorization"); ok {
		t.Error("the first REGISTER must not carry an Authorization header")
	}
	if _, ok := second.Header("Authorization"); !ok {
		t.Fatal("the retried REGISTER carries no Authorization header")
	}
	// Same transaction, next CSeq.
	if headerValue(t, first, "Call-ID") != headerValue(t, second, "Call-ID") {
		t.Error("the two REGISTERs belong to different transactions")
	}
	if headerValue(t, first, "CSeq") != "1 REGISTER" {
		t.Errorf("first CSeq = %q", headerValue(t, first, "CSeq"))
	}
	if headerValue(t, second, "CSeq") != "2 REGISTER" {
		t.Errorf("second CSeq = %q", headerValue(t, second, "CSeq"))
	}
	// GB/T 28181 §L.1 essentials.
	if got, want := first.URI().String(), "sip:"+e2eServer+"@"+e2eDomain; got != want {
		t.Errorf("Request-URI = %q, want %q", got, want)
	}
	if got, want := headerValue(t, first, "To"), "<sip:"+e2eDeviceA+"@"+e2eDomain+">"; got != want {
		t.Errorf("To = %q, want %q", got, want)
	}
	if got, want := headerValue(t, first, "Contact"), "<sip:"+e2eDeviceA+"@"+nodeAddr+">"; got != want {
		t.Errorf("Contact = %q, want %q", got, want)
	}
	if got := headerValue(t, first, "Expires"); got != "3600" {
		t.Errorf("Expires = %q, want 3600", got)
	}
	if got := headerValue(t, first, "Via"); !strings.Contains(got, "SIP/2.0/UDP") {
		t.Errorf("Via = %q, want UDP", got)
	}

	// The wire bytes of the first REGISTER, with the per-run values masked
	// out, must match the recorded golden snapshot.
	compareGolden(t, "register_first.golden", snapshot(first))
}

// Two devices registering to two platforms must not see each other's
// traffic, even though they share one process.
func TestTwoDevicesRegisterIndependently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uasA, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewUAS A: %v", err)
	}
	defer func() { _ = uasA.Close() }()
	uasB, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewUAS B: %v", err)
	}
	defer func() { _ = uasB.Close() }()
	go func() { _ = uasA.Serve(ctx) }()
	go func() { _ = uasB.Serve(ctx) }()

	svc := registrationService(t)
	type device struct {
		id   string
		uas  *siptest.UAS
		node model.Node
	}
	devices := make([]device, 0, 2)
	for i, tc := range []struct {
		id  string
		uas *siptest.UAS
	}{
		{e2eDeviceA, uasA},
		{e2eDeviceB, uasB},
	} {
		reg, err := model.NewRegistration(model.RegistrationParams{
			Server:   tc.uas.Addr(),
			ServerID: e2eServer,
			Password: e2ePasswd,
			Timeout:  3 * time.Second,
		})
		if err != nil {
			t.Fatalf("NewRegistration[%d]: %v", i, err)
		}
		created, err := svc.Create(ctx, deviceProfile(t, tc.id, freeAddr(t), reg))
		if err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
		devices = append(devices, device{id: tc.id, uas: tc.uas, node: created})
	}

	for _, d := range devices {
		id, err := model.ParseNodeID(d.id)
		if err != nil {
			t.Fatalf("ParseNodeID: %v", err)
		}
		if err := svc.Start(ctx, id); err != nil {
			t.Fatalf("Start %s: %v", d.id, err)
		}
		node, _ := svc.Get(ctx, id)
		if node.Status() != model.StatusOnline {
			t.Errorf("%s: status = %s, want online", d.id, node.Status())
		}
	}
	// Each platform saw exactly its own device.
	for _, d := range devices {
		for _, msg := range d.uas.Received() {
			if from := headerValue(t, msg, "From"); !strings.Contains(from, d.id) {
				t.Errorf("platform of %s received a REGISTER from %q", d.id, from)
			}
		}
		if len(d.uas.Received()) != 2 {
			t.Errorf("platform of %s received %d messages, want 2", d.id, len(d.uas.Received()))
		}
	}
}

// --- golden snapshot helpers ---------------------------------------------

// hostPortPattern matches the ephemeral loopback address a test binds.
var hostPortPattern = regexp.MustCompile(`\d{1,3}(\.\d{1,3}){3}:\d+`)

// snapshot renders a message as stable text: everything that changes per
// run (branch, Call-ID, tag, the ephemeral local address, digest values) is
// masked, so the bytes that define the protocol stay comparable.
func snapshot(m model.Message) string {
	var sb strings.Builder
	sb.WriteString("REGISTER ")
	sb.WriteString(m.URI().String())
	sb.WriteString(" SIP/2.0\n")
	for _, h := range m.Headers() {
		value := hostPortPattern.ReplaceAllString(h.Value(), "<local>")
		switch h.Name() {
		case "Via":
			value = maskBranch(value)
		case "From", "To":
			value = maskTag(value)
		case "Call-ID":
			value = "<call-id>"
		case "Authorization":
			value = maskDigest(value)
		}
		sb.WriteString(h.Name())
		sb.WriteString(": ")
		sb.WriteString(value)
		sb.WriteString("\n")
	}
	return sb.String()
}

func maskBranch(via string) string {
	out := via
	if i := strings.Index(out, "branch="); i >= 0 {
		end := strings.IndexAny(out[i:], ";,")
		if end < 0 {
			end = len(out[i:])
		}
		out = out[:i] + "branch=<branch>" + out[i+end:]
	}
	return out
}

func maskTag(value string) string {
	if i := strings.Index(value, "tag="); i >= 0 {
		return value[:i] + "tag=<tag>"
	}
	return value
}

func maskDigest(value string) string {
	out := value
	for _, key := range []string{"nonce", "response", "cnonce", "opaque"} {
		out = replaceParam(out, key, "<"+key+">")
	}
	return out
}

func replaceParam(value, key, with string) string {
	idx := strings.Index(value, key+"=")
	if idx < 0 {
		return value
	}
	start := idx + len(key) + 1
	if start >= len(value) {
		return value
	}
	quote := value[start] == '"'
	if quote {
		start++
	}
	end := start
	for end < len(value) && value[end] != '"' && value[end] != ',' {
		end++
	}
	return value[:start] + with + value[end:]
}

// compareGolden checks text against testdata/<name>, which is the reviewed
// record of what a REGISTER must look like on the wire.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (got:\n%s)", path, err, got)
	}
	if string(want) != got {
		t.Errorf("wire snapshot differs from %s:\n--- golden ---\n%s\n--- got ---\n%s",
			path, want, got)
	}
}

// --- GB/T 35114 SM2 end-to-end (task 5.1 / 5.2) ---------------------------
// TestUAS_SM2Registration verifies that, when SM2 mode is enabled:
//  1. the first REGISTER is challenged with an SM3 WWW-Authenticate header,
//  2. a response carrying a correct SM2 signature is accepted (200 OK + SecurityInfo),
//  3. a response with a wrong SM2 signature is rejected (403).
func TestUAS_SM2Registration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Build SM2 credentials for the "device".
	devPriv, devPub, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair device: %v", err)
	}
	devCred, err := model.NewCredentials(e2eDeviceA, e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	devCred = devCred.WithSM2KeyPair(devPriv, devPub)

	// Build SM2 credentials for the "platform" (UAS) that signs 200 OKs.
	platPriv, _, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair platform: %v", err)
	}

	uas, err := siptest.NewUAS("udp", freeAddr(t), e2eDomain, e2ePasswd,
		siptest.WithSM2(devCred, platPriv))
	if err != nil {
		t.Fatalf("NewUAS: %v", err)
	}
	defer func() { _ = uas.Close() }()
	go func() { _ = uas.Serve(ctx) }()

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	port := siptransport.NewPortAdapter(client)

	requestURI := "sip:" + e2eServer + "@" + e2eDomain
	first, err := model.NewRequest("REGISTER", requestURI, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=abc"),
		model.NewHeader("To", "<sip:"+e2eDeviceA+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "sm2-callid-1"),
		model.NewHeader("CSeq", "1 REGISTER"),
		model.NewHeader("Contact", "<sip:"+e2eDeviceA+"@"+client.LocalAddr()+">"),
		model.NewHeader("Expires", "3600"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := port.Send(ctx, first, uas.Addr()); err != nil {
		t.Fatalf("send REGISTER: %v", err)
	}

	resp, _, err := port.Receive(ctx)
	if err != nil {
		t.Fatalf("receive 401: %v", err)
	}
	if resp.StatusCode() != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode())
	}
	challenge := headerValue(t, resp, "WWW-Authenticate")
	if !strings.HasPrefix(challenge, "Digest ") {
		t.Fatalf("challenge = %q, want a Digest challenge", challenge)
	}
	if !strings.Contains(challenge, "algorithm=SM3") {
		t.Fatalf("challenge missing SM3 algorithm: %s", challenge)
	}

	// Build the Authorization header using the device's SM2 private key so
	// that the platform can verify the response. We bypass Authorize here
	// because the adapter does not generate nc/cnonce (GB/T 35114 requires
	// both for qop=auth).
	// Parse the WWW-Authenticate header manually to extract fields.
	challengeBody := strings.TrimSpace(strings.TrimPrefix(challenge, "Digest "))
	realm, nonce, opaque, alg := "", "", "", "MD5"
	for _, kv := range strings.Split(challengeBody, ", ") {
		kv = strings.TrimSpace(kv)
		if !strings.Contains(kv, "=") {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.Trim(parts[1], `"`)
		switch key {
		case "realm":
			realm = val
		case "nonce":
			nonce = val
		case "opaque":
			opaque = val
		case "algorithm":
			alg = val
		}
	}
	qop := ""
	if strings.Contains(challenge, `qop="auth"`) {
		qop = "auth"
	}
	chModel, err := model.NewChallenge(realm, nonce, alg, opaque, qop)
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}
	authStr, err := sipauth.BuildAuthorizationWithHash(
		sipauth.SM3Hash, devCred, chModel,
		"REGISTER", requestURI, "00000001", "abc123",
	)
	if err != nil {
		t.Fatalf("BuildAuthorizationWithHash: %v", err)
	}
	authHeader := model.NewHeader("Authorization", authStr)
	second, err := model.NewRequest("REGISTER", requestURI, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=abc"),
		model.NewHeader("To", "<sip:"+e2eDeviceA+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "sm2-callid-1"),
		model.NewHeader("CSeq", "2 REGISTER"),
		model.NewHeader("Contact", "<sip:"+e2eDeviceA+"@"+client.LocalAddr()+">"),
		model.NewHeader("Expires", "3600"),
		authHeader,
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := port.Send(ctx, second, uas.Addr()); err != nil {
		t.Fatalf("send authenticated REGISTER: %v", err)
	}
	resp, _, err = port.Receive(ctx)
	if err != nil {
		t.Fatalf("receive 200: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode(), resp)
	}
	secInfo := headerValue(t, resp, "SecurityInfo")
	if !strings.HasPrefix(secInfo, "SM2,") {
		t.Fatalf("SecurityInfo = %q, want SM2,<hex>", secInfo)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(secInfo, "SM2,")); err != nil {
		t.Fatalf("SecurityInfo signature is not valid hex: %v", err)
	}

	// Now exercise the rejection path: build an Authorization header with
	// a different SM2 private key so the signature does not verify.
	wrongPriv, _, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair wrong: %v", err)
	}
	wrongCred := devCred.WithSM2KeyPair(wrongPriv, devPub)
	wrongAuthStr, err := sipauth.BuildAuthorizationWithHash(
		sipauth.SM3Hash, wrongCred, chModel,
		"REGISTER", requestURI, "00000001", "abc123",
	)
	if err != nil {
		t.Fatalf("BuildAuthorizationWithHash wrong: %v", err)
	}
	wrongAuth := model.NewHeader("Authorization", wrongAuthStr)
	third, err := model.NewRequest("REGISTER", requestURI, []model.Header{
		model.NewHeader("From", "<sip:"+e2eDeviceA+"@"+e2eDomain+">;tag=abc"),
		model.NewHeader("To", "<sip:"+e2eDeviceA+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "sm2-callid-2"),
		model.NewHeader("CSeq", "1 REGISTER"),
		model.NewHeader("Contact", "<sip:"+e2eDeviceA+"@"+client.LocalAddr()+">"),
		model.NewHeader("Expires", "3600"),
		wrongAuth,
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := port.Send(ctx, third, uas.Addr()); err != nil {
		t.Fatalf("send wrong-auth REGISTER: %v", err)
	}
	resp, _, err = port.Receive(ctx)
	if err != nil {
		t.Fatalf("receive 403: %v", err)
	}
	if resp.StatusCode() != 403 {
		t.Fatalf("status = %d, want 403", resp.StatusCode())
	}
}

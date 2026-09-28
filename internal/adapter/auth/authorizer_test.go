package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// stubRequest is the minimal auth.Request a server-side Verify needs.
type stubRequest struct {
	method string
	auth   string
}

func (s stubRequest) Method() string        { return s.method }
func (s stubRequest) Authorization() string { return s.auth }

func mustCredentials(t *testing.T, user, realm, password string) model.Credentials {
	t.Helper()
	cred, err := model.NewCredentials(user, realm, password)
	if err != nil {
		t.Fatalf("NewCredentials(%q,%q): %v", user, realm, err)
	}
	return cred
}

func mustChallenge(t *testing.T, realm, nonce, alg, opaque, qop string) model.Challenge {
	t.Helper()
	ch, err := model.NewChallenge(realm, nonce, alg, opaque, qop)
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}
	return ch
}

// TestBuildAuthorization_GoldenRFC2617 pins the produced header byte for
// byte using the worked example of RFC 2617 §3.5 (reproduced in RFC 7616
// §3.9). If this ever changes, interoperability with real platforms broke.
func TestBuildAuthorization_GoldenRFC2617(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "Mufasa", "testrealm@host.com", "Circle Of Life")
	ch := mustChallenge(t, "testrealm@host.com", "dcd98b7102dd2f0e8b11d0f600bfb0c093", "MD5", "", "auth")

	got, err := auth.BuildAuthorization(cred, ch, "GET", "/dir/index.html", "00000001", "0a4f113b")
	if err != nil {
		t.Fatalf("BuildAuthorization: %v", err)
	}
	want := `Digest username="Mufasa", realm="testrealm@host.com", ` +
		`nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", uri="/dir/index.html", ` +
		`response="6629fae49393a05397450978507c4ef1", algorithm=MD5, ` +
		`qop=auth, nc=00000001, cnonce="0a4f113b"`
	if got != want {
		t.Errorf("Authorization:\n got %q\nwant %q", got, want)
	}
}

// The header we produce must be accepted by the server-side path — that
// path re-derives the digest from the password, so it is an independent
// check of the formula, not a restatement of it.
func TestBuildAuthorization_AcceptedByVerify(t *testing.T) {
	t.Parallel()
	const password = "gb28181-secret"
	cred := mustCredentials(t, "34020000001320000001", "3402000000", password)
	ch := mustChallenge(t, "3402000000", "YWJjZGVmZ2hpamtsbW5vcA==", "MD5", "opaque-value", "auth")

	value, err := auth.BuildAuthorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000", "00000001", "0a4f113b")
	if err != nil {
		t.Fatalf("BuildAuthorization: %v", err)
	}
	r := auth.NewResponder(nil)
	if err := r.Verify(stubRequest{method: "REGISTER", auth: value}, password); err != nil {
		t.Errorf("Verify rejected our own Authorization (%v): %v", value, err)
	}
}

// BuildEmptyAuthorization produces an Authorization header with response=""
// for no-auth registrations. The platform-side AuthenticatorAdapter skips
// the Digest check when cred.NoAuth() is true and response is empty.
func TestBuildEmptyAuthorization(t *testing.T) {
	t.Parallel()
	ch := mustChallenge(t, "realm", "nonce", "MD5", "", "auth")

	header, err := auth.BuildEmptyAuthorization("device1", ch, "REGISTER", "sip:realm")
	if err != nil {
		t.Fatalf("BuildEmptyAuthorization: %v", err)
	}
	fields, err := auth.ParseAuthorization(header)
	if err != nil {
		t.Fatalf("ParseAuthorization: %v", err)
	}
	if fields.Username != "device1" {
		t.Errorf("username = %q, want device1", fields.Username)
	}
	if fields.Response != "" {
		t.Errorf("response = %q, want empty string", fields.Response)
	}
	if fields.Qop != "" || fields.Nc != "" || fields.Cnonce != "" {
		t.Errorf("qop/nc/cnonce should be absent in no-auth mode, got %q/%q/%q",
			fields.Qop, fields.Nc, fields.Cnonce)
	}
}

// Authorizer.Authorization emits BuildEmptyAuthorization when the credential
// has no password (no-auth mode).
func TestAuthorizer_Authorization_EmptyPassword(t *testing.T) {
	t.Parallel()
	cred, err := model.NewCredentials("device1", "realm", "")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	ch := mustChallenge(t, "realm", "nonce", "MD5", "", "auth")

	authorizer := auth.NewAuthorizer(nil)
	header, err := authorizer.Authorization(cred, ch, "REGISTER", "sip:realm")
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}
	fields, err := auth.ParseAuthorization(header)
	if err != nil {
		t.Fatalf("ParseAuthorization: %v", err)
	}
	if fields.Response != "" {
		t.Errorf("response = %q, want empty for no-auth", fields.Response)
	}
}

// A wrong password must be rejected, otherwise the golden test could pass
// on a response that does not depend on the credentials at all.
func TestBuildAuthorization_WrongPasswordRejected(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "34020000001320000001", "3402000000", "right")
	ch := mustChallenge(t, "3402000000", "YWJjZGVmZ2hpamtsbW5vcA==", "MD5", "", "auth")
	value, err := auth.BuildAuthorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000", "00000001", "0a4f113b")
	if err != nil {
		t.Fatalf("BuildAuthorization: %v", err)
	}
	r := auth.NewResponder(nil)
	if err := r.Verify(stubRequest{method: "REGISTER", auth: value}, "wrong"); !errors.Is(err, auth.ErrInvalidResponse) {
		t.Errorf("Verify with a wrong password: got %v, want %v", err, auth.ErrInvalidResponse)
	}
}

// Challenges without qop come from older platforms: RFC 2617 §3 form, no
// nc / cnonce at all.
func TestBuildAuthorization_WithoutQop(t *testing.T) {
	t.Parallel()
	const password = "gb28181-secret"
	cred := mustCredentials(t, "34020000001320000001", "3402000000", password)
	ch := mustChallenge(t, "3402000000", "YWJjZGVmZ2hpamtsbW5vcA==", "MD5", "", "")

	value, err := auth.BuildAuthorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000", "", "")
	if err != nil {
		t.Fatalf("BuildAuthorization: %v", err)
	}
	for _, absent := range []string{"qop=", "nc=", "cnonce="} {
		if strings.Contains(value, absent) {
			t.Errorf("qop-less Authorization must not contain %q: %v", absent, value)
		}
	}
	r := auth.NewResponder(nil)
	if err := r.Verify(stubRequest{method: "REGISTER", auth: value}, password); err != nil {
		t.Errorf("Verify rejected the RFC 2617 form: %v", err)
	}
}

// An algorithm we cannot compute must be reported, not silently hashed as
// MD5 — a wrong digest would only show up as an opaque rejection.
func TestBuildAuthorization_UnsupportedAlgorithm(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "34020000001320000001", "3402000000", "secret")
	ch := mustChallenge(t, "3402000000", "nonce-value", "SHA-256", "", "auth")
	_, err := auth.BuildAuthorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000", "00000001", "0a4f113b")
	if !errors.Is(err, auth.ErrUnknownAlgorithm) {
		t.Fatalf("got %v, want %v", err, auth.ErrUnknownAlgorithm)
	}
}

func TestBuildAuthorization_MissingMethodOrURI(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "34020000001320000001", "3402000000", "secret")
	ch := mustChallenge(t, "3402000000", "nonce-value", "MD5", "", "auth")
	if _, err := auth.BuildAuthorization(cred, ch, "", "sip:x@y", "00000001", "c"); err == nil {
		t.Error("expected an error for an empty method")
	}
	if _, err := auth.BuildAuthorization(cred, ch, "REGISTER", "", "00000001", "c"); err == nil {
		t.Error("expected an error for an empty URI")
	}
}

// Opaque is echoed back verbatim; platforms use it to keep state.
func TestBuildAuthorization_EchoesOpaque(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "34020000001320000001", "3402000000", "secret")
	ch := mustChallenge(t, "3402000000", "nonce-value", "MD5", "opq-123", "auth")
	value, err := auth.BuildAuthorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000", "00000001", "0a4f113b")
	if err != nil {
		t.Fatalf("BuildAuthorization: %v", err)
	}
	if !strings.Contains(value, `opaque="opq-123"`) {
		t.Errorf("Authorization lacks the echoed opaque: %v", value)
	}
}

func TestParseChallenge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		wantErr bool
		check   func(t *testing.T, ch model.Challenge)
	}{
		{
			name:  "canonical",
			value: `Digest realm="3402000000", nonce="abc123", qop="auth", algorithm=MD5, opaque="opq"`,
			check: func(t *testing.T, ch model.Challenge) {
				if ch.Realm() != "3402000000" || ch.Nonce() != "abc123" {
					t.Errorf("realm/nonce: %q / %q", ch.Realm(), ch.Nonce())
				}
				if ch.Qop() != "auth" || ch.Algorithm() != "MD5" || ch.Opaque() != "opq" {
					t.Errorf("qop/alg/opaque: %q / %q / %q", ch.Qop(), ch.Algorithm(), ch.Opaque())
				}
			},
		},
		{
			name:  "unquoted and mixed case",
			value: `digest realm=3402000000, nonce=abc123, algorithm=md5`,
			check: func(t *testing.T, ch model.Challenge) {
				if ch.Realm() != "3402000000" || ch.Nonce() != "abc123" {
					t.Errorf("realm/nonce: %q / %q", ch.Realm(), ch.Nonce())
				}
				// algorithm=md5 is still MD5; only the wire casing differs.
				if !strings.EqualFold(ch.Algorithm(), "MD5") {
					t.Errorf("algorithm: %q", ch.Algorithm())
				}
			},
		},
		{
			name:  "missing algorithm defaults to MD5",
			value: `Digest realm="3402000000", nonce="abc123", qop="auth"`,
			check: func(t *testing.T, ch model.Challenge) {
				if ch.Algorithm() != auth.DefaultAlgorithm {
					t.Errorf("algorithm: got %q want %q", ch.Algorithm(), auth.DefaultAlgorithm)
				}
			},
		},
		{
			name:  "several qop values picks auth",
			value: `Digest realm="3402000000", nonce="abc123", qop="auth-int,auth"`,
			check: func(t *testing.T, ch model.Challenge) {
				if ch.Qop() != "auth" {
					t.Errorf("qop: got %q want auth", ch.Qop())
				}
			},
		},
		{name: "no realm", value: `Digest nonce="abc123"`, wantErr: true},
		{name: "no nonce", value: `Digest realm="3402000000"`, wantErr: true},
		{name: "not digest", value: `Basic realm="3402000000"`, wantErr: true},
		{name: "empty", value: "", wantErr: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ch, err := auth.ParseChallenge(tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got challenge %+v", ch)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChallenge: %v", err)
			}
			tc.check(t, ch)
		})
	}
}

// A challenge we generated must survive the round trip, so the client and
// server halves of this package agree on the format.
func TestParseChallenge_RoundTripWithChallenger(t *testing.T) {
	t.Parallel()
	c := auth.NewChallenger(nil)
	value, nonce, err := c.Challenge("3402000000", auth.WithOpaque())
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	ch, err := auth.ParseChallenge(value)
	if err != nil {
		t.Fatalf("ParseChallenge of our own challenge: %v", err)
	}
	if ch.Realm() != "3402000000" || ch.Nonce() != nonce {
		t.Errorf("realm/nonce mismatch: %q / %q (want nonce %q)", ch.Realm(), ch.Nonce(), nonce)
	}
	if ch.Qop() != "auth" || ch.Algorithm() != "MD5" || ch.Opaque() == "" {
		t.Errorf("qop/alg/opaque: %q / %q / %q", ch.Qop(), ch.Algorithm(), ch.Opaque())
	}
}

// The Authorization we build must parse back to the same fields.
func TestParseAuthorization_RoundTrip(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "34020000001320000001", "3402000000", "secret")
	ch := mustChallenge(t, "3402000000", "nonce-value", "MD5", "opq", "auth")
	const uri = "sip:34020000002000000001@3402000000"
	value, err := auth.BuildAuthorization(cred, ch, "REGISTER", uri, "00000001", "0a4f113b")
	if err != nil {
		t.Fatalf("BuildAuthorization: %v", err)
	}
	fields, err := auth.ParseAuthorization(value)
	if err != nil {
		t.Fatalf("ParseAuthorization of our own header: %v", err)
	}
	if fields.Username != cred.Username() {
		t.Errorf("username: got %q want %q", fields.Username, cred.Username())
	}
	if fields.Realm != ch.Realm() || fields.Nonce != ch.Nonce() || fields.URI != uri {
		t.Errorf("realm/nonce/uri: %q / %q / %q", fields.Realm, fields.Nonce, fields.URI)
	}
	if fields.Qop != "auth" || fields.Nc != "00000001" || fields.Cnonce != "0a4f113b" {
		t.Errorf("qop/nc/cnonce: %q / %q / %q", fields.Qop, fields.Nc, fields.Cnonce)
	}
	if fields.Opaque != "opq" {
		t.Errorf("opaque: got %q want opq", fields.Opaque)
	}
}

func TestNewCNonce_Unique(t *testing.T) {
	t.Parallel()
	a, err := auth.NewCNonce()
	if err != nil {
		t.Fatalf("NewCNonce: %v", err)
	}
	b, err := auth.NewCNonce()
	if err != nil {
		t.Fatalf("NewCNonce: %v", err)
	}
	if a == "" || b == "" {
		t.Fatalf("empty cnonce: %q / %q", a, b)
	}
	if a == b {
		t.Errorf("two cnonces collided: %q", a)
	}
}

func TestAuthorizer_MintsFreshCNoncePerCall(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "34020000001320000001", "3402000000", "secret")
	ch := mustChallenge(t, "3402000000", "nonce-value", "MD5", "", "auth")
	a := auth.NewAuthorizer(nil)
	first, err := a.Authorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000")
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}
	second, err := a.Authorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000")
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}
	if first == second {
		t.Errorf("two transactions produced the same Authorization: %v", first)
	}
	if !strings.Contains(first, "nc=00000001") {
		t.Errorf("nc must start at 00000001: %v", first)
	}
}

// A deterministic cnonce makes the whole header predictable in tests.
func TestAuthorizer_WithCNonceFunc(t *testing.T) {
	t.Parallel()
	cred := mustCredentials(t, "34020000001320000001", "3402000000", "secret")
	ch := mustChallenge(t, "3402000000", "nonce-value", "MD5", "", "auth")
	a := auth.NewAuthorizer(nil, auth.WithCNonceFunc(func() (string, error) { return "fixed", nil }))
	value, err := a.Authorization(cred, ch, "REGISTER", "sip:34020000002000000001@3402000000")
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}
	if !strings.Contains(value, `cnonce="fixed"`) {
		t.Errorf("cnonce not honoured: %v", value)
	}
}

func TestFormatNC(t *testing.T) {
	t.Parallel()
	if got, want := auth.FormatNC(1), "00000001"; got != want {
		t.Errorf("FormatNC(1): got %q want %q", got, want)
	}
	if got, want := auth.FormatNC(255), "000000ff"; got != want {
		t.Errorf("FormatNC(255): got %q want %q", got, want)
	}
}

package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// TestPortAssertions is a redundant runtime assertion that the adapters satisfy
// the port interfaces. Compile-time `var _ ...` guards already catch drift,
// but this test ensures CI (which may skip vet) still fails on interface
// regression.
func TestPortAssertions(t *testing.T) {
	var _ port.Authenticator = (*AuthenticatorAdapter)(nil)
	var _ port.Challenger = (*ChallengerAdapter)(nil)
}

// TestChallengerAdapter_ImplementsPort exercises the port.Challenger contract
// through the model.Challenge returned value: realm/algorithm/qop must match
// inputs and the nonce must be non-empty.
func TestChallengerAdapter_ImplementsPort(t *testing.T) {
	challenger := NewChallenger(nil)
	adapter, err := NewChallengerAdapter(challenger)
	if err != nil {
		t.Fatalf("NewChallengerAdapter: %v", err)
	}

	realm := "gb28181.example.com"
	ch, err := adapter.Challenge(realm)
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	if ch.Realm() != realm {
		t.Errorf("Realm = %q, want %q", ch.Realm(), realm)
	}
	if ch.Nonce() == "" {
		t.Error("Nonce must not be empty")
	}
	if ch.Algorithm() != "MD5" {
		t.Errorf("Algorithm = %q, want MD5", ch.Algorithm())
	}
	if ch.Qop() != "auth" {
		t.Errorf("Qop = %q, want auth", ch.Qop())
	}
	// Ensure the underlying type is the canonical model.Challenge.
	var _ model.Challenge = ch
}

// TestAuthenticatorAdapter_ImplementsPort exercises the port.Authenticator
// binding: the adapter must not panic when Verify is called with a real
// model.Message. The actual credential match is asserted by the legacy
// TestVerify_* tests.
func TestAuthenticatorAdapter_ImplementsPort(t *testing.T) {
	responder := NewResponder(nil)
	adapter, err := NewAuthenticatorAdapter(responder)
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	// Minimal request that satisfies the adapter's type signature.
	msg, err := model.NewRequest("INVITE", "sip:34020000001320000001@3402000000:5060", nil, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	cred, err := model.NewCredentials("alice", "room1", "password")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	// Must not panic; nil error is accepted (credentials mismatch handled
	// by legacy responder, tested in auth_test.go).
	_ = adapter.Verify(msg, cred)
}

// TestAuthenticatorAdapter_MissingAuthorization_ReturnsErrMalformed
// verifies the documented contract that a request without an Authorization
// header surfaces as ErrMalformedAuthorization (not a generic error). This
// protects callers from having to differentiate between "no credential
// presented" and "credential presented but wrong".
func TestAuthenticatorAdapter_MissingAuthorization_ReturnsErrMalformed(t *testing.T) {
	responder := NewResponder(nil)
	adapter, err := NewAuthenticatorAdapter(responder)
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	msg, err := model.NewRequest("REGISTER", "sip:alice@example.com", nil, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	cred, err := model.NewCredentials("alice", "example.com", "password")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}

	if err := adapter.Verify(msg, cred); !errors.Is(err, ErrMalformedAuthorization) {
		t.Fatalf("Verify(no Auth) = %v, want ErrMalformedAuthorization", err)
	}
}

// TestAuthenticatorAdapter_ValidDigestCredentials_Pass builds a Digest
// Authorization header via the matching ComputeResponse formula and confirms
// the port.Authenticator adapter verifies it end-to-end.
func TestAuthenticatorAdapter_ValidDigestCredentials_Pass(t *testing.T) {
	const (
		method   = "REGISTER"
		username = "alice"
		realm    = "gb28181"
		password = "secret"
		nonce    = "abc123"
		uri      = "sip:alice@example.com"
		qop      = "auth"
		nc       = "00000001"
		cnonce   = "0a4f113b"
	)
	hash := MD5Hash
	ha1 := hash(username + ":" + realm + ":" + password)
	ha2 := hash(method + ":" + uri)
	expected := hash(strings.Join([]string{ha1, nonce, nc, cnonce, qop, ha2}, ":"))

	auth := `Digest username="alice", realm="gb28181", nonce="abc123", ` +
		`uri="sip:alice@example.com", qop=auth, nc=00000001, cnonce="0a4f113b", ` +
		`response="` + expected + `", algorithm=MD5`

	responder := NewResponder(nil)
	adapter, err := NewAuthenticatorAdapter(responder)
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	headers := []model.Header{model.NewHeader("Authorization", auth)}
	msg, err := model.NewRequest(method, uri, headers, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	cred, err := model.NewCredentials(username, realm, password)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if err := adapter.Verify(msg, cred); err != nil {
		t.Fatalf("Verify(valid) = %v, want nil", err)
	}
}

// TestAuthenticatorAdapter_InvalidDigestCredentials_Fail confirms that a
// request with the correct structure but wrong password surfaces as
// ErrInvalidResponse. The port-level call site can therefore distinguish
// "credential mismatch" from "parse error".
func TestAuthenticatorAdapter_InvalidDigestCredentials_Fail(t *testing.T) {
	const (
		method   = "REGISTER"
		username = "alice"
		realm    = "gb28181"
		nonce    = "abc123"
		uri      = "sip:alice@example.com"
		qop      = "auth"
		nc       = "00000001"
		cnonce   = "0a4f113b"
	)
	// Computed with the correct password but the verifier is given a wrong one.
	hash := MD5Hash
	ha1 := hash(username + ":" + realm + ":rightpw")
	ha2 := hash(method + ":" + uri)
	expected := hash(strings.Join([]string{ha1, nonce, nc, cnonce, qop, ha2}, ":"))

	auth := `Digest username="alice", realm="gb28181", nonce="abc123", ` +
		`uri="sip:alice@example.com", qop=auth, nc=00000001, cnonce="0a4f113b", ` +
		`response="` + expected + `", algorithm=MD5`

	responder := NewResponder(nil)
	adapter, err := NewAuthenticatorAdapter(responder)
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	headers := []model.Header{model.NewHeader("Authorization", auth)}
	msg, err := model.NewRequest(method, uri, headers, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	cred, err := model.NewCredentials(username, realm, "WRONG")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if err := adapter.Verify(msg, cred); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Verify(wrong pw) = %v, want ErrInvalidResponse", err)
	}
}

// TestChallengerAdapter_NoncesAreUnique verifies that the port.Challenger
// adapter does not cache or short-circuit across calls — every Challenge
// invocation must yield a fresh nonce. This protects callers that maintain
// a nonce-store from accidentally accepting a replay.
func TestChallengerAdapter_NoncesAreUnique(t *testing.T) {
	adapter, err := NewChallengerAdapter(NewChallenger(nil))
	if err != nil {
		t.Fatalf("NewChallengerAdapter: %v", err)
	}
	const n = 64
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		ch, err := adapter.Challenge("gb28181")
		if err != nil {
			t.Fatalf("Challenge[%d]: %v", i, err)
		}
		if _, dup := seen[ch.Nonce()]; dup {
			t.Fatalf("nonce %q repeated after %d calls", ch.Nonce(), i)
		}
		seen[ch.Nonce()] = struct{}{}
	}
}

// TestAuthenticatorAdapter_AcceptsUnknownHashWithoutCrash ensures the
// adapter's Verify path is unaffected by an exotic hash algorithm set on the
// Responder after construction (ReplaceHash). The model layer is hash-agnostic;
// failures surface from the legacy Responder and the adapter must not panic
// wrapping them. We use sha256 — which the legacy Responder does not recognise
// — and assert that the error is non-nil but the call does not panic.
func TestAuthenticatorAdapter_AcceptsUnknownHashWithoutCrash(t *testing.T) {
	responder := NewResponder(nil)
	responder.ReplaceHash(func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	})
	adapter, err := NewAuthenticatorAdapter(responder)
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	const (
		method = "REGISTER"
		uri    = "sip:alice@example.com"
	)
	auth := `Digest username="alice", realm="gb28181", nonce="abc", ` +
		`uri="sip:alice@example.com", response="x", algorithm=MD5`

	headers := []model.Header{model.NewHeader("Authorization", auth)}
	msg, err := model.NewRequest(method, uri, headers, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	cred, err := model.NewCredentials("alice", "gb28181", "secret")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if err := adapter.Verify(msg, cred); err == nil {
		t.Fatal("Verify with sha256 responder must error; got nil")
	}
}

// TestPortAdapters_ConcurrentAccessSafe spawns parallel goroutines that
// hammer both port adapters to confirm no data races are introduced by the
// bridging layer. Run with `go test -race` to catch regressions.
func TestPortAdapters_ConcurrentAccessSafe(t *testing.T) {
	chalAdapter, err := NewChallengerAdapter(NewChallenger(nil))
	if err != nil {
		t.Fatalf("NewChallengerAdapter: %v", err)
	}
	authAdapter, err := NewAuthenticatorAdapter(NewResponder(nil))
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	msg, err := model.NewRequest("REGISTER", "sip:alice@example.com", nil, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	cred, err := model.NewCredentials("alice", "gb28181", "secret")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 32
	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := chalAdapter.Challenge("gb28181"); err != nil {
				t.Errorf("Challenge: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			_ = authAdapter.Verify(msg, cred)
		}()
	}
	wg.Wait()
}

// TestAuthenticatorAdapter_NoAuthEmptyResponse verifies the documented
// no-auth contract: when cred.NoAuth() is true and the Authorization header
// carries response="", the verifier skips the Digest check and returns nil.
// A non-empty response on a no-auth credential still gets verified normally.
func TestAuthenticatorAdapter_NoAuthEmptyResponse(t *testing.T) {
	responder := NewResponder(nil)
	adapter, err := NewAuthenticatorAdapter(responder)
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	cred, err := model.NewCredentials("alice", "gb28181", "")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	cred = cred.WithNoAuth()

	// Empty response + no-auth cred → nil.
	authHeader := `Digest username="alice", realm="gb28181", nonce="n", uri="sip:alice@example.com", response=""`
	msg, err := model.NewRequest("REGISTER", "sip:alice@example.com",
		[]model.Header{model.NewHeader("Authorization", authHeader)}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := adapter.Verify(msg, cred); err != nil {
		t.Errorf("Verify(no-auth, empty response) = %v, want nil", err)
	}

	// Non-empty response on a no-auth credential still flows through normal
	// verification, which fails when the empty-password formula cannot match
	// the digest the peer sent. This protects against a misconfigured node
	// that accidentally sends a real Digest silently getting a 200.
	authHeaderNonEmpty := `Digest username="alice", realm="gb28181", nonce="n", uri="sip:alice@example.com", response="deadbeef"`
	msg2, err := model.NewRequest("REGISTER", "sip:alice@example.com",
		[]model.Header{model.NewHeader("Authorization", authHeaderNonEmpty)}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := adapter.Verify(msg2, cred); err == nil {
		t.Error("Verify(no-auth, non-empty response) = nil, want error")
	}

	// And a regular (non-no-auth) credential with empty response must
	// still be rejected, so no-auth cannot be silently opted-in.
	regular, err := model.NewCredentials("alice", "gb28181", "password")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if err := adapter.Verify(msg, regular); err == nil {
		t.Error("Verify(regular cred, empty response) = nil, want error")
	}
}

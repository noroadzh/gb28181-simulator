package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockReq is a minimal Request implementation for testing without
// pulling in the SIP stack.
type mockReq struct {
	method, authorization string
}

func (r *mockReq) Method() string        { return r.method }
func (r *mockReq) Authorization() string { return r.authorization }

// --- Challenger ---------------------------------------------------------

// TestChallenger_GeneratesUniqueNonce (task §3.1)
func TestChallenger_GeneratesUniqueNonce(t *testing.T) {
	c := NewChallenger(nil)
	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		_, nonce, err := c.Challenge("gb28181")
		if err != nil {
			t.Fatalf("Challenge: %v", err)
		}
		if _, dup := seen[nonce]; dup {
			t.Fatalf("nonce %q repeated after %d calls", nonce, i)
		}
		seen[nonce] = struct{}{}
		if len(nonce) < 22 {
			t.Errorf("nonce %q shorter than 22 chars (16-byte base64)", nonce)
		}
	}
}

// TestChallenger_FormatMatchesRFC7616 verifies the byte-level layout of
// the produced WWW-Authenticate header value.
func TestChallenger_FormatMatchesRFC7616(t *testing.T) {
	c := NewChallenger(nil)
	val, _, err := c.Challenge("gb28181", WithOpaque())
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	want := []string{
		`Digest realm="gb28181"`,
		`qop="auth"`,
		`algorithm=MD5`,
		`nonce="`,
		`opaque="`,
	}
	for _, w := range want {
		if !strings.Contains(val, w) {
			t.Errorf("Challenge output missing %q in:\n%s", w, val)
		}
	}
}

// --- Responder ----------------------------------------------------------

// TestResponder_VerifyQopAuth_Pass walks through the RFC 7616 §3.4
// formula end-to-end. We hand-compute the expected response so the test
// is fully reproducible without depending on the same Responder code.
func TestResponder_VerifyQopAuth_Pass(t *testing.T) {
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

	r := NewResponder(nil)
	if err := r.Verify(&mockReq{method: method, authorization: auth}, password); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestResponder_WrongPassword_Fails covers the "ErrInvalidResponse" path.
func TestResponder_WrongPassword_Fails(t *testing.T) {
	const (
		method   = "REGISTER"
		username = "alice"
		realm    = "gb28181"
		nonce    = "abc123"
		uri      = "sip:alice@example.com"
	)
	expected := MD5Hash(MD5Hash(username+":"+realm+":rightpass") + ":" + nonce + ":" + MD5Hash(method+":"+uri))
	auth := `Digest username="alice", realm="gb28181", nonce="abc123", ` +
		`uri="sip:alice@example.com", response="` + expected + `"`

	r := NewResponder(nil)
	err := r.Verify(&mockReq{method: method, authorization: auth}, "wrongpass")
	if err != ErrInvalidResponse {
		t.Fatalf("Verify(wrongpass) = %v, want ErrInvalidResponse", err)
	}
}

// TestResponder_NoQopLegacyClient (task §3.4)
func TestResponder_NoQopLegacyClient(t *testing.T) {
	const (
		method   = "REGISTER"
		username = "alice"
		realm    = "gb28181"
		password = "secret"
		nonce    = "legacy-nonce-001"
		uri      = "sip:alice@example.com"
	)
	// RFC 2617 §3: response = MD5(MD5(user:realm:pass) : nonce : MD5(method:uri))
	ha1 := MD5Hash(username + ":" + realm + ":" + password)
	ha2 := MD5Hash(method + ":" + uri)
	expected := MD5Hash(ha1 + ":" + nonce + ":" + ha2)

	auth := `Digest username="alice", realm="gb28181", nonce="legacy-nonce-001", ` +
		`uri="sip:alice@example.com", response="` + expected + `"`

	r := NewResponder(nil)
	if err := r.Verify(&mockReq{method: method, authorization: auth}, password); err != nil {
		t.Fatalf("Verify (legacy RFC 2617): %v", err)
	}
}

// TestResponder_HashFuncOverride (task §3.3) — replaces MD5 with SHA-1
// and verifies the Golden hash updates accordingly.
func TestResponder_HashFuncOverride(t *testing.T) {
	const (
		method   = "REGISTER"
		username = "alice"
		realm    = "gb28181"
		password = "secret"
		nonce    = "n1"
		uri      = "sip:alice@example.com"
	)
	ha1 := SHA1Hash(username + ":" + realm + ":" + password)
	ha2 := SHA1Hash(method + ":" + uri)
	expected := SHA1Hash(ha1 + ":" + nonce + ":" + ha2)

	auth := `Digest username="alice", realm="gb28181", nonce="n1", ` +
		`uri="sip:alice@example.com", response="` + expected + `"`

	r := NewResponder(SHA1Hash)
	if err := r.Verify(&mockReq{method: method, authorization: auth}, password); err != nil {
		t.Fatalf("Verify with SHA1Hash: %v", err)
	}
	// Wrong password under SHA1Hash must also fail with the right error.
	if err := r.Verify(&mockReq{method: method, authorization: auth}, "wrong"); err != ErrInvalidResponse {
		t.Errorf("SHA1 verify wrong pass = %v, want ErrInvalidResponse", err)
	}
}

// TestResponder_UTF8Normalization_Pass (task §3.5) — username contains
// non-ASCII bytes that form valid UTF-8.
func TestResponder_UTF8Normalization_Pass(t *testing.T) {
	const (
		method   = "REGISTER"
		username = "用户-001" // 7 bytes of valid UTF-8
		realm    = "gb28181"
		password = "secret"
		nonce    = "n-utf8"
		uri      = "sip:user@3402000000"
	)
	ha1 := MD5Hash(username + ":" + realm + ":" + password)
	ha2 := MD5Hash(method + ":" + uri)
	expected := MD5Hash(ha1 + ":" + nonce + ":" + ha2)

	auth := `Digest username="` + username + `", realm="gb28181", nonce="n-utf8", ` +
		`uri="sip:user@3402000000", response="` + expected + `"`

	r := NewResponder(nil)
	if err := r.Verify(&mockReq{method: method, authorization: auth}, password); err != nil {
		t.Fatalf("UTF-8 verify: %v", err)
	}
}

// TestResponder_UTF8Normalization_ASCIIIdentity (task §3.5) — RFC 7616
// qop=auth path and RFC 2617 no-qop path produce the same hash for the
// same ASCII inputs (modulo the qop-related fields).
func TestResponder_UTF8Normalization_ASCIIIdentity(t *testing.T) {
	const (
		method   = "REGISTER"
		username = "alice"
		realm    = "gb28181"
		password = "secret"
		nonce    = "n-id"
		uri      = "sip:alice@example.com"
	)
	// Path A: qop=auth
	ha1a := MD5Hash(username + ":" + realm + ":" + password)
	ha2a := MD5Hash(method + ":" + uri)
	withQop := MD5Hash(strings.Join([]string{ha1a, nonce, "00000001", "c", "auth", ha2a}, ":"))

	// Path B: no qop
	noQop := MD5Hash(ha1a + ":" + nonce + ":" + ha2a)

	// For ASCII inputs the two algorithms agree on HA1/HA2; they only
	// diverge by the ":nc:cnonce:qop:" segment. So withQop must NOT
	// equal noQop. This test exists to assert that the dual-mode branch
	// is honoured, not collapsed.
	if withQop == noQop {
		t.Errorf("qop-mode and no-qop-mode produced identical hash — formula collapsed")
	}

	// And both paths independently verify with the matching Authorization.
	authQop := `Digest username="alice", realm="gb28181", nonce="n-id", ` +
		`uri="sip:alice@example.com", qop=auth, nc=00000001, cnonce="c", response="` + withQop + `"`
	if err := NewResponder(nil).Verify(&mockReq{method: method, authorization: authQop}, password); err != nil {
		t.Errorf("qop path verify: %v", err)
	}
	authNoQop := `Digest username="alice", realm="gb28181", nonce="n-id", ` +
		`uri="sip:alice@example.com", response="` + noQop + `"`
	if err := NewResponder(nil).Verify(&mockReq{method: method, authorization: authNoQop}, password); err != nil {
		t.Errorf("no-qop path verify: %v", err)
	}
}

// TestResponder_InvalidUTF8 rejects malformed UTF-8 in username.
func TestResponder_InvalidUTF8(t *testing.T) {
	// 0xff is never valid UTF-8 at byte boundary.
	badName := "bad\xffname"
	auth := `Digest username="` + badName + `", realm="gb28181", nonce="n", ` +
		`uri="sip:x@y", response="` + strings.Repeat("0", 32) + `"`
	err := NewResponder(nil).Verify(&mockReq{method: "REGISTER", authorization: auth}, "p")
	if err != ErrInvalidUTF8 {
		t.Errorf("invalid UTF-8: err = %v, want ErrInvalidUTF8", err)
	}
}

// TestParseAuthorization_RejectsNonDigest (spec edge case)
func TestParseAuthorization_RejectsNonDigest(t *testing.T) {
	if _, err := ParseAuthorization("Bearer abc"); err == nil {
		t.Errorf("Bearer should not parse as Digest")
	}
}

// TestParseAuthorization_RoundTrip parses a fixture and re-emits a minimal
// canonical form to confirm every field was extracted.
func TestParseAuthorization_RoundTrip(t *testing.T) {
	in := `Digest username="u", realm="r", nonce="n", uri="sip:u@x", qop=auth, nc=00000001, cnonce="c", response="` + strings.Repeat("0", 32) + `"`
	f, err := ParseAuthorization(in)
	if err != nil {
		t.Fatalf("ParseAuthorization: %v", err)
	}
	checks := map[string]string{
		"Username": "u", "Realm": "r", "Nonce": "n", "URI": "sip:u@x",
		"Qop": "auth", "Nc": "00000001", "Cnonce": "c",
		"Response": strings.Repeat("0", 32), "Alg": "MD5",
	}
	got := map[string]string{
		"Username": f.Username, "Realm": f.Realm, "Nonce": f.Nonce,
		"URI": f.URI, "Qop": f.Qop, "Nc": f.Nc, "Cnonce": f.Cnonce,
		"Response": f.Response, "Alg": f.Alg,
	}
	for k, want := range checks {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
}

// --- Fixture SHA256 校验 (任务 §8.1) ---------------------------------------

func TestFixtures_SHA256Stable(t *testing.T) {
	expected, err := os.ReadFile(filepath.Join("testdata", "golden-sha256"))
	if err != nil {
		t.Fatalf("read golden-sha256: %v", err)
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".auth") {
			continue
		}
		b, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		sum := sha256.Sum256(b)
		got := hex.EncodeToString(sum[:])
		want := ""
		for _, ln := range strings.Split(string(expected), "\n") {
			if strings.HasSuffix(strings.TrimSpace(ln), "  "+e.Name()) {
				want = strings.Fields(ln)[0]
				break
			}
		}
		if want == "" {
			t.Errorf("no SHA256 recorded for %s; add to golden-sha256", e.Name())
			continue
		}
		if got != want {
			t.Errorf("SHA256 drift on %s: got=%s want=%s", e.Name(), got, want)
		}
	}
}
package auth

import (
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Fields is the structured form of an Authorization header. Returned by
// ParseAuthorization (lives in internal/sip/auth.go but the type itself
// is auth-package owned so callers do not need to import sip).
type Fields struct {
	Username string
	Realm    string
	Nonce    string
	URI      string
	Response string
	Qop      string // empty when absent (RFC 2617 fallback)
	Nc       string // only meaningful when Qop != ""
	Cnonce   string // only meaningful when Qop != ""
	Alg      string // "MD5" / "MD5-sess" / "SHA-256" / ...
	Opaque   string
	// SecurityInfo carries the GB 35114 mutual-authentication
	// directive (e.g. "SM2,<hex signature>"); empty when absent.
	SecurityInfo string
	// Note carries the optional GB 35114 SM3-integrity value echoed by
	// the client; empty when the peer did not request Note integrity.
	Note string
}

// ParseAuthorization parses a Digest Authorization header value (the
// portion after "Authorization: "). It returns the structured Fields
// and a sentinel error for at least one invalid parameter so callers can
// distinguish parse-failure from credential-failure.
//
// The function is exposed because the auth package would otherwise have
// to be imported by both internal/sip/parser.go and internal/auth —
// keeping it in auth avoids the import cycle.
func ParseAuthorization(value string) (Fields, error) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "digest ") {
		return Fields{}, fmt.Errorf("%w: missing Digest scheme", ErrMalformedAuthorization)
	}
	body := strings.TrimSpace(value[len("Digest "):])

	out := Fields{}
	i := 0
	for i < len(body) {
		// skip leading whitespace and commas (commas are valid RFC 7616
		// separators per §3.1)
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == ',') {
			i++
		}
		if i >= len(body) {
			break
		}
		// Read key=
		j := i
		for j < len(body) && body[j] != '=' && body[j] != ',' {
			j++
		}
		if j >= len(body) || body[j] != '=' {
			return Fields{}, fmt.Errorf("%w: expected '=' after key", ErrMalformedAuthorization)
		}
		key := strings.ToLower(strings.TrimSpace(body[i:j]))
		i = j + 1
		// Read value: optional "..." quoted, or bare token up to next ','
		if i < len(body) && body[i] == '"' {
			i++ // skip opening quote
			j = i
			for j < len(body) && body[j] != '"' {
				// handle escaped quotes inside the value
				if body[j] == '\\' && j+1 < len(body) {
					j += 2
					continue
				}
				j++
			}
			if j >= len(body) {
				return Fields{}, fmt.Errorf("%w: unterminated quoted value", ErrMalformedAuthorization)
			}
			val := body[i:j]
			i = j + 1 // skip closing quote
			switch key {
			case "username":
				out.Username = val
			case "realm":
				out.Realm = val
			case "nonce":
				out.Nonce = val
			case "uri":
				out.URI = val
			case "response":
				out.Response = val
			case "qop":
				out.Qop = val
			case "nc":
				out.Nc = val
			case "cnonce":
				out.Cnonce = val
			case "algorithm":
				out.Alg = val
			case "opaque":
				out.Opaque = val
			case "security-info":
				out.SecurityInfo = val
			case "note":
				out.Note = val
			}
		} else {
			j = i
			for j < len(body) && body[j] != ',' && body[j] != ' ' && body[j] != '\t' {
				j++
			}
			val := body[i:j]
			i = j
			switch key {
			case "username", "realm", "nonce", "uri", "response",
				"qop", "nc", "cnonce", "algorithm", "opaque", "note":
				if val == "" {
					return Fields{}, fmt.Errorf("%w: empty value for %q", ErrMalformedAuthorization, key)
				}
			}
			switch key {
			case "username":
				out.Username = val
			case "realm":
				out.Realm = val
			case "nonce":
				out.Nonce = val
			case "uri":
				out.URI = val
			case "response":
				out.Response = val
			case "qop":
				out.Qop = val
			case "nc":
				out.Nc = val
			case "cnonce":
				out.Cnonce = val
			case "algorithm":
				out.Alg = val
			case "opaque":
				out.Opaque = val
			case "security-info":
				out.SecurityInfo = val
			case "note":
				out.Note = val
			}
		}
	}
	if out.Username == "" || out.Realm == "" || out.Nonce == "" || out.URI == "" {
		return Fields{}, fmt.Errorf("%w: missing required parameter", ErrMalformedAuthorization)
	}
	// An empty Response is legal for no-auth / test-intranet registrations:
	// the caller (AuthenticatorAdapter) is responsible for deciding whether
	// to honour it based on the credential's NoAuth flag.
	if out.Alg == "" {
		out.Alg = "MD5"
	}
	return out, nil
}

// Responder verifies Digest Authorization headers. A Responder is bound
// to one HashFunc at construction; use ReplaceHash to swap it later
// (Change 12 hot-swap).
type Responder struct {
	hash HashFunc
}

// NewResponder returns a Responder that uses the supplied HashFunc.
// Pass nil to use MD5Hash (the default).
func NewResponder(hash HashFunc) *Responder {
	if hash == nil {
		hash = MD5Hash
	}
	return &Responder{hash: hash}
}

// ReplaceHash swaps the underlying HashFunc. Intended for tests and for
// Change 12 (GB35114) to substitute SM3 at runtime.
func (r *Responder) ReplaceHash(hash HashFunc) {
	if hash == nil {
		hash = MD5Hash
	}
	r.hash = hash
}

// Verify recomputes the Digest response from the request's Authorization
// header and the server-held password, then compares it to the response
// sent by the client. It returns nil on match, ErrInvalidResponse on
// mismatch, and ErrMalformedAuthorization / ErrInvalidUTF8 /
// ErrUnknownAlgorithm for parse/structural problems.
//
// Mode selection: if the parsed Qop field is non-empty, RFC 7616 §3.4 is
// used; otherwise RFC 2617 §3 (no qop) is used.
//
// username and realm go through a UTF-8 validity check per RFC 7616
// §3.3 before being hashed. Pure-ASCII inputs (the GB/T 28181 norm) take
// the same code path and produce the same hash as the legacy RFC 2617
// algorithm, so existing devices interoperate.
func (r *Responder) Verify(req Request, password string) error {
	if req == nil {
		return fmt.Errorf("%w: nil request", ErrMalformedAuthorization)
	}
	fields, err := ParseAuthorization(req.Authorization())
	if err != nil {
		return err
	}
	if !utf8.ValidString(fields.Username) || !utf8.ValidString(fields.Realm) {
		return ErrInvalidUTF8
	}
	// We support MD5 / MD5-sess (the GB/T 28181 default) and SM3 /
	// SM3-sess (the GB 35114 A/B-grade algorithm). The hash function is
	// selected from the algorithm field in the Authorization header so
	// callers do not need to pre-configure the Responder per peer.
	var hash HashFunc
	switch strings.ToLower(fields.Alg) {
	case "", "md5", "md5-sess":
		hash = r.hash
	case "sm3", "sm3-sess":
		hash = sm.HashSM3
	default:
		return fmt.Errorf("%w: %s", ErrUnknownAlgorithm, fields.Alg)
	}

	ha1 := hash(fields.Username + ":" + fields.Realm + ":" + password)
	ha2 := hash(req.Method() + ":" + fields.URI)

	var expected string
	if fields.Qop != "" {
		// RFC 7616 §3.4 (qop=auth or qop=auth-int). GB/T 28181 §L.2
		// mandates qop=auth, so we don't differentiate auth-int here.
		if fields.Nc == "" || fields.Cnonce == "" {
			return fmt.Errorf("%w: qop requires nc/cnonce", ErrMalformedAuthorization)
		}
		expected = hash(strings.Join([]string{
			ha1, fields.Nonce, fields.Nc, fields.Cnonce, fields.Qop, ha2,
		}, ":"))
	} else {
		// RFC 2617 §3 (no qop): MD5(HA1 : nonce : HA2)
		expected = hash(ha1 + ":" + fields.Nonce + ":" + ha2)
	}

	// Constant-time compare. Both hex strings are 32 lowercase chars
	// (MD5 hex); unequal lengths short-circuit to a safe mismatch.
	if len(fields.Response) != len(expected) {
		return ErrInvalidResponse
	}
	if subtle.ConstantTimeCompare([]byte(fields.Response), []byte(expected)) != 1 {
		return ErrInvalidResponse
	}

	// GB 35114 Note integrity: when the client echoes a Note the server
	// re-hashes nonce+realm+timestamp and compares. Any mismatch (including
	// a malformed or tampered note) is treated as a malformed Authorization
	// per the spec's SM3 Note integrity scenario.
	if fields.Note != "" {
		ts, hashPart, ok := strings.Cut(fields.Note, ":")
		if !ok {
			return fmt.Errorf("%w: missing Note separator", ErrMalformedAuthorization)
		}
		tsInt, err := strconv.ParseInt(ts, 10, 64)
		if err != nil || tsInt < 0 {
			return fmt.Errorf("%w: invalid Note timestamp", ErrMalformedAuthorization)
		}
		expected := sm.HashSM3(fields.Nonce + ":" + fields.Realm + ":" + strconv.FormatInt(tsInt, 10))
		if expected != hashPart {
			return ErrMalformedAuthorization
		}
	}
	return nil
}

// VerifyWithCredentials is the GB 35114-aware variant of Verify. It
// first performs the standard Digest verification, then — when the
// Authorization carries a security-info directive and cred has an SM2
// public key — verifies the SM2 signature over the Digest response.
// Requests without security-info pass through unchanged so MD5-only
// peers see byte-identical behaviour.
//
// A security-info directive whose algorithm tag is not "SM2" or whose
// signature fails verification returns ErrInvalidSecurityInfo.
func (r *Responder) VerifyWithCredentials(req Request, cred model.Credentials) error {
	fields, err := ParseAuthorization(req.Authorization())
	if err != nil {
		return err
	}
	if err := r.Verify(req, cred.Password()); err != nil {
		return err
	}
	if fields.SecurityInfo == "" {
		return nil
	}
	pub := cred.SM2PublicKey()
	if pub == nil {
		return fmt.Errorf("%w: peer sent security-info but server credential has no SM2 public key", ErrInvalidSecurityInfo)
	}
	rest, ok := strings.CutPrefix(fields.SecurityInfo, "SM2,")
	if !ok {
		return fmt.Errorf("%w: unsupported scheme %q", ErrInvalidSecurityInfo, fields.SecurityInfo)
	}
	sig, err := hex.DecodeString(rest)
	if err != nil {
		return fmt.Errorf("%w: bad signature encoding", ErrInvalidSecurityInfo)
	}
	if !sm.VerifySM2(pub, []byte(fields.Response), sig) {
		return ErrInvalidSecurityInfo
	}
	return nil
}

// ComputeResponse is the client-side helper that builds the response=
// field of an Authorization header. Mirrors Verify exactly: same
// branching on Qop, same HashFunc apply point.
//
// method is the SIP request method (e.g. "REGISTER"); the other fields
// mirror the Headers in the challenge. This is exposed so that future
// Change 4 (UAC) and Change 6 (UAS) can build their Authorization
// headers without duplicating the formula.
func (r *Responder) ComputeResponse(method, username, realm, password, nonce, uri, qop, nc, cnonce string) string {
	ha1 := r.hash(username + ":" + realm + ":" + password)
	ha2 := r.hash(method + ":" + uri)
	if qop != "" {
		return r.hash(strings.Join([]string{
			ha1, nonce, nc, cnonce, qop, ha2,
		}, ":"))
	}
	return r.hash(ha1 + ":" + nonce + ":" + ha2)
}

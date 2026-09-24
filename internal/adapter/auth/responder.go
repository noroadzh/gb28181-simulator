package auth

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"unicode/utf8"
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
				"qop", "nc", "cnonce", "algorithm", "opaque":
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
			}
		}
	}
	if out.Username == "" || out.Realm == "" || out.Nonce == "" ||
		out.URI == "" || out.Response == "" {
		return Fields{}, fmt.Errorf("%w: missing required parameter", ErrMalformedAuthorization)
	}
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
	// We currently support MD5 (default) and MD5-sess. Anything else
	// (e.g. SHA-256) requires a HashFunc swap that we don't auto-detect.
	if fields.Alg != "MD5" && fields.Alg != "MD5-sess" {
		return fmt.Errorf("%w: %s", ErrUnknownAlgorithm, fields.Alg)
	}

	ha1 := r.hash(fields.Username + ":" + fields.Realm + ":" + password)
	ha2 := r.hash(req.Method() + ":" + fields.URI)

	var expected string
	if fields.Qop != "" {
		// RFC 7616 §3.4 (qop=auth or qop=auth-int). GB/T 28181 §L.2
		// mandates qop=auth, so we don't differentiate auth-int here.
		if fields.Nc == "" || fields.Cnonce == "" {
			return fmt.Errorf("%w: qop requires nc/cnonce", ErrMalformedAuthorization)
		}
		expected = r.hash(strings.Join([]string{
			ha1, fields.Nonce, fields.Nc, fields.Cnonce, fields.Qop, ha2,
		}, ":"))
	} else {
		// RFC 2617 §3 (no qop): MD5(HA1 : nonce : HA2)
		expected = r.hash(ha1 + ":" + fields.Nonce + ":" + ha2)
	}

	// Constant-time compare. Both hex strings are 32 lowercase chars
	// (MD5 hex); unequal lengths short-circuit to a safe mismatch.
	if len(fields.Response) != len(expected) {
		return ErrInvalidResponse
	}
	if subtle.ConstantTimeCompare([]byte(fields.Response), []byte(expected)) != 1 {
		return ErrInvalidResponse
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

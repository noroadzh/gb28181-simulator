package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Authorizer builds the client side of a Digest exchange: it turns a
// challenge received from a platform plus this node's credentials into an
// Authorization header value. It mirrors Responder, which does the same
// from the server's side.
type Authorizer struct {
	responder *Responder
	newCNonce func() (string, error)
}

// NewAuthorizer returns an Authorizer using the supplied HashFunc (nil
// means MD5). cnonce generation can be overridden by tests through
// WithCNonceFunc.
func NewAuthorizer(hash HashFunc, opts ...AuthorizerOption) *Authorizer {
	a := &Authorizer{
		responder: NewResponder(hash),
		newCNonce: NewCNonce,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// AuthorizerOption customises an Authorizer.
type AuthorizerOption func(*Authorizer)

// WithCNonceFunc replaces the cnonce generator. Intended for tests that
// need a deterministic Authorization header.
func WithCNonceFunc(f func() (string, error)) AuthorizerOption {
	return func(a *Authorizer) { a.newCNonce = f }
}

// Authorization builds a complete Authorization header value for one
// request. Each call mints a fresh cnonce and counts nc from 1, i.e. one
// call per registration transaction.
//
// When cred carries no password (a no-auth / empty-password credential for
// test/intranet scenarios) the rendered Authorization uses an empty
// response= field and omits qop/nc/cnonce so the upstream platform — when
// it also opts into allow_no_auth — can skip the Digest comparison.
func (a *Authorizer) Authorization(cred model.Credentials, ch model.Challenge, method, uri string) (string, error) {
	if cred.Password() == "" {
		return BuildEmptyAuthorization(cred.Username(), ch, method, uri)
	}
	cnonce, err := a.newCNonce()
	if err != nil {
		return "", err
	}
	hash := MD5Hash
	if a.responder != nil {
		hash = a.responder.hash
	}
	return BuildAuthorizationWithHash(hash, cred, ch, method, uri, FormatNC(1), cnonce)
}

// BuildAuthorization renders an Authorization header value with the
// default (MD5) hash. See BuildAuthorizationWithHash.
func BuildAuthorization(cred model.Credentials, ch model.Challenge, method, uri, nc, cnonce string) (string, error) {
	return BuildAuthorizationWithHash(MD5Hash, cred, ch, method, uri, nc, cnonce)
}

// BuildAuthorizationWithHash renders the value of an Authorization header
// answering ch for the given request, e.g.
//
//	Digest username="34020000001320000001", realm="3402000000",
//	       nonce="...", uri="sip:34020000002000000001@3402000000",
//	       response="...", algorithm=MD5, qop=auth, nc=00000001,
//	       cnonce="..."
//
// When the challenge offers no qop the RFC 2617 §3 form is used and nc /
// cnonce are omitted. The algorithm is taken from ch.Algorithm(); MD5 /
// MD5-sess use the supplied hash (default MD5) while SM3 / SM3-sess
// require hash to be non-nil (an SM3 HashFunc). Any other algorithm is
// rejected with ErrUnknownAlgorithm rather than silently producing a
// wrong digest.
func BuildAuthorizationWithHash(
	hash HashFunc,
	cred model.Credentials,
	ch model.Challenge,
	method, uri, nc, cnonce string,
) (string, error) {
	if method == "" {
		return "", fmt.Errorf("auth: empty method")
	}
	if uri == "" {
		return "", fmt.Errorf("auth: empty URI")
	}
	alg := ch.Algorithm()
	if alg == "" {
		alg = DefaultAlgorithm
	}
	switch strings.ToUpper(alg) {
	case "", "MD5", "MD5-SESS":
		if hash == nil {
			hash = MD5Hash
		}
	case "SM3", "SM3-SESS":
		if hash == nil {
			return "", fmt.Errorf("%w: %s requires an SM3 HashFunc", ErrUnknownAlgorithm, alg)
		}
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownAlgorithm, alg)
	}
	qop := ch.Qop()

	// The realm comes from the challenge: the platform decides the
	// protection space, the credential only supplies username/password.
	response := (&Responder{hash: hash}).ComputeResponse(
		method, cred.Username(), ch.Realm(), cred.Password(),
		ch.Nonce(), uri, qop, nc, cnonce,
	)

	fields := make([]string, 0, 10)
	fields = append(fields,
		`username=`+quote(cred.Username()),
		`realm=`+quote(ch.Realm()),
		`nonce=`+quote(ch.Nonce()),
		`uri=`+quote(uri),
		`response=`+quote(response),
		`algorithm=`+alg,
	)
	if qop != "" {
		fields = append(fields,
			`qop=`+qop,
			`nc=`+nc,
			`cnonce=`+quote(cnonce),
		)
	}
	if opaque := ch.Opaque(); opaque != "" {
		fields = append(fields, `opaque=`+quote(opaque))
	}
	if note := ch.Note(); note != "" {
		fields = append(fields, `Note=`+quote(note))
	}
	if priv := cred.SM2PrivateKey(); priv != nil {
		// GB 35114 §B.2.2 style mutual-authentication: the device
		// signs the Digest response with its SM2 private key and
		// carries the signature in the SecurityInfo directive so the
		// platform can verify device identity beyond the shared
		// password.
		sig, err := sm.SignSM2(priv, []byte(response))
		if err != nil {
			return "", fmt.Errorf("auth: sign digest response: %w", err)
		}
		fields = append(fields, `security-info="SM2,`+hex.EncodeToString(sig)+`"`)
	}
	return "Digest " + strings.Join(fields, ", "), nil
}

// NewCNonce returns a fresh client nonce: 8 random bytes, hex-encoded.
// It must differ per transaction so two registrations cannot be replayed
// as one another.
func NewCNonce() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: read random: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// BuildEmptyAuthorization renders an Authorization header with an empty
// response= field, suitable for test/intranet no-auth registrations. The
// response is intentionally empty so a platform that also has allow_no_auth
// can accept the peer without a password comparison.
//
// Unlike a real Digest exchange, qop/nc/cnonce are omitted because there is
// nothing to hash and no shared secret to protect. The username is still
// present so the platform can attribute the session.
func BuildEmptyAuthorization(username string, ch model.Challenge, method, uri string) (string, error) {
	if method == "" {
		return "", fmt.Errorf("auth: empty method")
	}
	if uri == "" {
		return "", fmt.Errorf("auth: empty URI")
	}
	alg := ch.Algorithm()
	if alg == "" {
		alg = DefaultAlgorithm
	}
	switch strings.ToUpper(alg) {
	case "", "MD5", "MD5-SESS", "SM3", "SM3-SESS":
		// accepted: any algorithm is fine since we produce no response
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownAlgorithm, alg)
	}

	fields := []string{
		`username=` + quote(username),
		`realm=` + quote(ch.Realm()),
		`nonce=` + quote(ch.Nonce()),
		`uri=` + quote(uri),
		`response=""`,
		`algorithm=` + alg,
	}
	if opaque := ch.Opaque(); opaque != "" {
		fields = append(fields, `opaque=`+quote(opaque))
	}
	return "Digest " + strings.Join(fields, ", "), nil
}

// FormatNC renders a Digest nonce-count as the fixed-width 8-digit hex
// value required by RFC 7616 §3.4 (e.g. 1 → "00000001").
func FormatNC(n uint32) string {
	return fmt.Sprintf("%08x", n)
}

// quote wraps a header parameter value in double quotes, escaping the
// characters that would otherwise break the parameter list.
func quote(value string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '"', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(value[i])
		default:
			sb.WriteByte(value[i])
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// Package model — Credentials.
package model

import (
	"crypto/subtle"
	"fmt"
	"strings"
)

// Credentials is an immutable username / realm / password triple used by
// both the auth-verify path (server-side) and the auth-challenge path
// (server-side). The Password is held only in memory; equality is
// constant-time so callers may compare without timing leaks.
//
// Optional SM2 key material enables GB 35114 A/B-grade mutual
// authentication: SM2PrivateKey signs outbound Authorization (device
// side) and SM2PublicKey verifies inbound SecurityInfo (platform side).
// nil means "not configured" and the credential degrades to plain
// MD5/SM3 digest.
type Credentials struct {
	username      string
	realm         string
	password      string
	sm2PrivateKey []byte // raw 32-byte scalar; nil = absent
	sm2PublicKey  []byte // raw 65-byte uncompressed point; nil = absent
}

// NewCredentials constructs Credentials. username and realm MUST be valid
// UTF-8 (RFC 7616 §3.3); password is byte-transparent.
func NewCredentials(username, realm, password string) (Credentials, error) {
	if username == "" {
		return Credentials{}, fmt.Errorf("model: empty username")
	}
	if realm == "" {
		return Credentials{}, fmt.Errorf("model: empty realm")
	}
	if !isValidUTF8(username) {
		return Credentials{}, fmt.Errorf("model: username is not valid UTF-8")
	}
	if !isValidUTF8(realm) {
		return Credentials{}, fmt.Errorf("model: realm is not valid UTF-8")
	}
	return Credentials{
		username: username,
		realm:    realm,
		password: password,
	}, nil
}

// Username returns the verbatim username.
func (c Credentials) Username() string { return c.username }

// Realm returns the verbatim realm.
func (c Credentials) Realm() string { return c.realm }

// Password returns the verbatim password. Handle with care: do not log,
// do not marshal to JSON, do not include in error messages.
func (c Credentials) Password() string { return c.password }

// Equals performs a constant-time equality check on the Password field
// only (username and realm are public). Returns true iff len matches and
// every byte of the password is identical to other.
func (c Credentials) PasswordEquals(other string) bool {
	if subtle.ConstantTimeCompare([]byte(c.password), []byte(other)) != 1 {
		return false
	}
	return true
}

// String redacts the password; safe for log lines.
func (c Credentials) String() string {
	return fmt.Sprintf("Credentials<user=%q realm=%q password=***>", c.username, c.realm)
}

// WithSM2KeyPair returns a copy of c that carries the supplied raw SM2
// key material. priv is the 32-byte scalar and pub is the 65-byte
// uncompressed point. Either argument may be nil to clear that slot.
// Existing callers that don't care about SM2 continue to work unchanged.
func (c Credentials) WithSM2KeyPair(priv, pub []byte) Credentials {
	out := c
	out.sm2PrivateKey = cloneBytes(priv)
	out.sm2PublicKey = cloneBytes(pub)
	return out
}

// SM2PrivateKey returns the raw 32-byte private scalar or nil when
// the credential was not configured with an SM2 identity.
func (c Credentials) SM2PrivateKey() []byte { return cloneBytes(c.sm2PrivateKey) }

// SM2PublicKey returns the raw 65-byte uncompressed SM2 public key
// point or nil when the credential was not configured with an SM2
// identity.
func (c Credentials) SM2PublicKey() []byte { return cloneBytes(c.sm2PublicKey) }

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// Challenge is an immutable WWW-Authenticate header value: nonce, realm,
// algorithm and opaque. The Nonce is opaque to the model layer; callers
// (adapters) decide how to generate it. Note is the optional GB 35114
// SM3-integrity field echoed by the client in Authorization.
type Challenge struct {
	realm     string
	nonce     string
	algorithm string
	opaque    string
	qop       string
	note      string
}

// NewChallenge builds a Challenge. realm and nonce are mandatory.
func NewChallenge(realm, nonce, algorithm, opaque, qop string) (Challenge, error) {
	if realm == "" {
		return Challenge{}, fmt.Errorf("model: empty realm in Challenge")
	}
	if nonce == "" {
		return Challenge{}, fmt.Errorf("model: empty nonce in Challenge")
	}
	if strings.ContainsAny(nonce, "\r\n") {
		return Challenge{}, fmt.Errorf("model: nonce contains CR/LF")
	}
	return Challenge{
		realm:     realm,
		nonce:     nonce,
		algorithm: algorithm,
		opaque:    opaque,
		qop:       qop,
	}, nil
}

// Realm returns the challenge realm.
func (c Challenge) Realm() string { return c.realm }

// Nonce returns the verbatim nonce.
func (c Challenge) Nonce() string { return c.nonce }

// Algorithm returns the algorithm name (e.g. "MD5"); may be empty.
func (c Challenge) Algorithm() string { return c.algorithm }

// Opaque returns the opaque value; may be empty.
func (c Challenge) Opaque() string { return c.opaque }

// Qop returns the quality-of-protection directive (e.g. "auth"); may be empty.
func (c Challenge) Qop() string { return c.qop }

// Note returns the optional GB 35114 SM3-integrity note; empty when absent.
func (c Challenge) Note() string { return c.note }

// WithNote returns a Challenge whose Note field is set. Used only when the
// adapter is in SM3 Note-integrity mode.
func (c Challenge) WithNote(note string) Challenge {
	c.note = note
	return c
}

// isValidUTF8 returns true iff s is valid UTF-8 with no NUL byte. Used by
// NewCredentials to enforce RFC 7616 §3.3.
func isValidUTF8(s string) bool {
	if strings.ContainsRune(s, '\x00') {
		return false
	}
	// utf8.ValidString rejects invalid UTF-8 sequences.
	return utf8Valid(s)
}

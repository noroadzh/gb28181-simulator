package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// ChallengeOption customises a single Challenge call. Currently the only
// option is WithOpaque, which attaches an opaque= parameter to the
// challenge (RFC 7616 §3.1 — opaque is optional but recommended when
// the server wants to keep state across requests).
type ChallengeOption func(*challengeConfig)

type challengeConfig struct {
	withOpaque bool
}

// WithOpaque enables the opaque= parameter. The opaque value is generated
// from 8 random bytes (base64-encoded) so callers don't need to supply it.
func WithOpaque() ChallengeOption {
	return func(c *challengeConfig) { c.withOpaque = true }
}

// Challenger generates WWW-Authenticate challenges. A Challenger is
// stateless from the caller's perspective: every call returns a fresh
// nonce. If the caller needs replay protection, it stores the nonce
// externally (e.g. in-memory map with TTL).
type Challenger struct {
	hash HashFunc
}

// NewChallenger returns a Challenger that uses the supplied HashFunc.
// Pass nil to use MD5Hash (the default).
func NewChallenger(hash HashFunc) *Challenger {
	if hash == nil {
		hash = MD5Hash
	}
	return &Challenger{hash: hash}
}

// Challenge builds the WWW-Authenticate header value for the supplied
// realm. It returns the header value (suitable for placing after
// "WWW-Authenticate: ") and the nonce (so the caller can store it
// against the request for later replay defence).
//
// Format produced:
//
//	Digest realm="<realm>", nonce="<base64 16-byte>",
//	        qop="auth", algorithm=MD5[, opaque="<base64 8-byte>"]
//
// The nonce is exactly 16 bytes of crypto/rand, base64-encoded (24 chars).
func (c *Challenger) Challenge(realm string, opts ...ChallengeOption) (string, string, error) {
	if realm == "" {
		return "", "", fmt.Errorf("auth: empty realm")
	}
	cfg := challengeConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", "", fmt.Errorf("auth: read random: %w", err)
	}
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)

	value := fmt.Sprintf(
		`Digest realm=%q, nonce=%q, qop="auth", algorithm=MD5`,
		realm, nonce,
	)
	if cfg.withOpaque {
		opaqueBytes := make([]byte, 8)
		if _, err := rand.Read(opaqueBytes); err != nil {
			return "", "", fmt.Errorf("auth: read random: %w", err)
		}
		opaque := base64.StdEncoding.EncodeToString(opaqueBytes)
		value += fmt.Sprintf(`, opaque=%q`, opaque)
	}
	return value, nonce, nil
}

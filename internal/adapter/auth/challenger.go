package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ChallengerOption customises a Challenger. The same option type is used
// at construction time and on the per-call Challenge signature so that
// callers can pick one API surface.
type ChallengerOption func(*Challenger)

// ChallengeOption is kept as a historical alias for ChallengerOption so
// that existing tests (WithOpaque()) compile without change.
type ChallengeOption = ChallengerOption

// WithOpaque is a backward-compatible no-op. Opaque is now always emitted
// on MD5 challenges; callers should use WithHashName / WithNoteHash instead.
func WithOpaque() ChallengeOption { return func(*Challenger) {} }

// Challenger generates WWW-Authenticate challenges. A Challenger is
// stateless from the caller's perspective: every call returns a fresh
// nonce. If the caller needs replay protection, it stores the nonce
// externally (e.g. in-memory map with TTL).
type Challenger struct {
	hash     HashFunc
	hashName string // "MD5" (default) or "SM3"
	noteHash bool   // when true, the Note field carries an SM3 integrity hash
}

// NewChallenger returns a Challenger that uses the supplied HashFunc.
// Pass nil to use MD5Hash (the default).
func NewChallenger(hash HashFunc, opts ...ChallengerOption) *Challenger {
	if hash == nil {
		hash = MD5Hash
	}
	c := &Challenger{hash: hash, hashName: DefaultAlgorithm}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithHashName selects the digest algorithm for the produced challenge.
// Accepted values are "MD5" (the default, set by NewChallenger) and "SM3".
// Any other value is accepted here and rejected later by the Responder
// when the peer sends an unsupported algorithm; the Challenger only needs
// to emit the name the operator configured.
func WithHashName(name string) ChallengerOption {
	return func(c *Challenger) { c.hashName = strings.ToUpper(strings.TrimSpace(name)) }
}

// WithNoteHash enables SM3 Note integrity. When enabled and hashName is
// "SM3", the Note field carries the SM3 hash of (nonce + realm + timestamp)
// rather than the existing opaque random string.
func WithNoteHash() ChallengerOption {
	return func(c *Challenger) { c.noteHash = true }
}

// Challenge builds the WWW-Authenticate header value for the supplied
// realm. It returns the header value (suitable for placing after
// "WWW-Authenticate: ") and the nonce (so the caller can store it
// against the request for later replay defence).
//
// Format produced for MD5 (default):
//
//	Digest realm="<realm>", nonce="<base64 16-byte>",
//	        qop="auth", algorithm=MD5[, opaque="<base64 8-byte>"]
//
// Format produced for SM3:
//
//	Digest realm="<realm>", nonce="<base64 16-byte>",
//	        qop="auth", algorithm=SM3,
//	        Note="<sm3(nonce:realm:timestamp)>"
//
// The nonce is exactly 16 bytes of crypto/rand, base64-encoded (24 chars).
// opts are accepted for per-call customisation (e.g. WithHashName("SM3"))
// in addition to construction-time options. They override the Challenger's
// defaults for this single call only.
func (c *Challenger) Challenge(realm string, opts ...ChallengeOption) (string, string, error) {
	if realm == "" {
		return "", "", fmt.Errorf("auth: empty realm")
	}

	// Apply per-call options on a copy so construction defaults are preserved.
	cc := *c
	for _, opt := range opts {
		opt(&cc)
	}

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", "", fmt.Errorf("auth: read random: %w", err)
	}
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)

	parts := []string{
		`Digest realm=` + quote(realm),
		`nonce=` + quote(nonce),
		`qop="auth"`,
		`algorithm=` + cc.hashName,
	}

	if cc.noteHash && strings.EqualFold(cc.hashName, "SM3") {
		// GB 35114 Note integrity: embed an SM3 hash of (nonce + realm +
		// timestamp) as a self-contained value so the server can re-derive
		// and compare it without external state. The leading "ts:" prefix
		// lets the server extract the timestamp for re-hashing.
		// NB: use SM3Hash directly rather than cc.hash — cc.hash follows
		// the configured hash function (default MD5), while the Note
		// integrity per GB 35114 always uses SM3 when algorithm=SM3.
		ts := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		note := ts + ":" + SM3Hash(strings.Join([]string{nonce, realm, ts}, ":"))
		parts = append(parts, `Note=`+quote(note))
	} else {
		// RFC 7616 opaque: random 8-byte opaque token.
		opaqueBytes := make([]byte, 8)
		if _, err := rand.Read(opaqueBytes); err != nil {
			return "", "", fmt.Errorf("auth: read random: %w", err)
		}
		parts = append(parts, `opaque=`+quote(base64.StdEncoding.EncodeToString(opaqueBytes)))
	}
	return strings.Join(parts, ", "), nonce, nil
}

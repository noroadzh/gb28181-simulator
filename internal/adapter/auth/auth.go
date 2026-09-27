// Package auth implements RFC 7616 (and backwards-compatible RFC 2617)
// Digest authentication for GB/T 28181-2016 SIP signalling.
//
// The package is intentionally framework-agnostic: it depends on no SIP
// stack at the public API boundary (only the Request interface, which
// every concrete SIP message will satisfy). That keeps
// internal/sip/parser.go from pulling in crypto, and keeps crypto from
// pulling in the SIP stack.
//
// Two modes are supported on the verification path:
//
//  1. RFC 7616 §3.4 (qop="auth"): the response field is computed as
//     MD5(MD5(user:realm:password) : nonce : nc : cnonce : qop
//     : MD5(method:uri))
//     plus RFC 7616 §3.3 UTF-8 normalisation of `username` and `realm`.
//
//  2. RFC 2617 §3 (no qop): the response field is computed as
//     MD5(MD5(user:realm:password) : nonce : MD5(method:uri))
//
// Mode (1) is the default and matches GB/T 28181 §L.2 wording verbatim.
// Mode (2) is the legacy fallback for old clients (e.g. some Hikvision
// firmwares) that omit the qop field. Mode selection is automatic based
// on the qop field in the supplied Authorization header.
//
// HashFunc is a single point of substitution so Change 12 can swap MD5
// for SM3 without changing the public API.
package auth

import (
	"errors"
)

// Request is the minimal abstraction the Responder needs from the SIP
// stack. Any *sip.Request satisfies it via thin wrapper methods.
type Request interface {
	// Method returns the SIP request method (e.g. "REGISTER", "INVITE").
	Method() string
	// Authorization returns the verbatim value of the Authorization
	// header (without the leading "Authorization: " prefix).
	Authorization() string
}

// ErrInvalidResponse is returned by Responder.Verify when the supplied
// response does not match the recomputed value. Per GB/T 28181 §L.2 the
// server MUST NOT re-issue a challenge on a wrong response.
var ErrInvalidResponse = errors.New("auth: invalid response")

// ErrMalformedAuthorization is returned when the Authorization header
// cannot be parsed at all (e.g. missing username/response). Distinct from
// ErrInvalidResponse so callers can distinguish a parse error from a
// valid-but-wrong credential.
var ErrMalformedAuthorization = errors.New("auth: malformed Authorization header")

// ErrInvalidUTF8 is returned when username/realm contain bytes that do
// not form valid UTF-8 (RFC 7616 §3.3 mandates UTF-8 normalisation).
var ErrInvalidUTF8 = errors.New("auth: invalid UTF-8 in username/realm")

// ErrUnknownAlgorithm is returned when the Authorization header carries
// an algorithm= value we do not recognise (e.g. algorithm=SHA-256). The
// default HashFunc covers MD5 and SHA-1; Change 12 will register SM3.
var ErrUnknownAlgorithm = errors.New("auth: unknown algorithm")

// ErrInvalidSecurityInfo is returned when the security-info directive
// is missing, malformed, or its SM2 signature does not validate.
var ErrInvalidSecurityInfo = errors.New("auth: invalid security-info")

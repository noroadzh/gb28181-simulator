package auth

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// AuthenticatorAdapter implements port.Authenticator by wrapping the legacy
// Responder. It translates model.Message into the auth.Request interface
// (which needs only Method and Authorization) without mutating the model
// envelope.
type AuthenticatorAdapter struct {
	responder *Responder
}

var (
	_ port.Authenticator      = (*AuthenticatorAdapter)(nil)
	_ port.SecurityInfoSigner = (*AuthenticatorAdapter)(nil)
)

// NewAuthenticatorAdapter returns a port.Authenticator backed by the legacy
// Responder. A nil responder is rejected at construction time so callers
// cannot accidentally bypass auth.
func NewAuthenticatorAdapter(r *Responder) (*AuthenticatorAdapter, error) {
	if r == nil {
		return nil, fmt.Errorf("auth: nil Responder")
	}
	return &AuthenticatorAdapter{responder: r}, nil
}

// Verify implements port.Authenticator. It extracts the Authorization header
// from req (case-insensitive lookup) and delegates to the legacy Responder.
// Missing or unparseable headers surface as ErrMalformedAuthorization, which
// maps to port-level "credential parse error".
func (a *AuthenticatorAdapter) Verify(req model.Message, cred model.Credentials) error {
	h, ok := req.Header("Authorization")
	if !ok {
		return fmt.Errorf("%w: %w", port.ErrMalformedCredentials, ErrMalformedAuthorization)
	}
	stub := reqStub{method: req.Method(), auth: h.Value()}
	// VerifyWithCredentials runs the Digest check and, when the header
	// carries a security-info directive, the GB 35114 SM2 mutual-auth
	// signature check against cred's public key.
	err := a.responder.VerifyWithCredentials(stub, cred)
	if err == nil {
		return nil
	}
	// Only a parse failure may be challenged again; everything else —
	// including an algorithm we refuse to compute or a failed SM2
	// signature — is a refusal.
	if errors.Is(err, ErrMalformedAuthorization) {
		return fmt.Errorf("%w: %w", port.ErrMalformedCredentials, err)
	}
	return fmt.Errorf("%w: %w", port.ErrInvalidCredentials, err)
}

// SignSecurityInfo implements port.SecurityInfoSigner: it answers a
// REGISTER whose Authorization advertises algorithm=SM3 with a server-side
// SecurityInfo header signed by the credential's SM2 private key over the
// same Digest response. GB 35114 §5.3.2 requires the port layer to sign
// whenever algorithm=SM3 is used and the node holds an SM2 identity, so
// the signature is emitted independently of whether the client itself
// chose to carry a security-info directive. Returns ("", false) when the
// request uses MD5, cannot be parsed, or the credential lacks an SM2
// identity — the 200 OK stays byte-identical to a plain Digest exchange.
func (a *AuthenticatorAdapter) SignSecurityInfo(req model.Message, cred model.Credentials) (model.Header, bool) {
	h, ok := req.Header("Authorization")
	if !ok {
		return model.Header{}, false
	}
	fields, err := ParseAuthorization(h.Value())
	if err != nil {
		return model.Header{}, false
	}
	if !strings.EqualFold(fields.Alg, "SM3") {
		return model.Header{}, false
	}
	priv := cred.SM2PrivateKey()
	if priv == nil {
		return model.Header{}, false
	}
	sig, err := sm.SignSM2(priv, []byte(fields.Response))
	if err != nil {
		return model.Header{}, false
	}
	return model.NewHeader("SecurityInfo", "SM2,"+hex.EncodeToString(sig)), true
}

// ChallengerAdapter implements port.Challenger by delegating to the legacy
// Challenger. The model.Challenge value is a thin envelope around the raw
// WWW-Authenticate header string.
type ChallengerAdapter struct {
	challenger *Challenger
}

var _ port.Challenger = (*ChallengerAdapter)(nil)

// NewChallengerAdapter returns a port.Challenger backed by the legacy
// Challenger.
func NewChallengerAdapter(c *Challenger) (*ChallengerAdapter, error) {
	if c == nil {
		return nil, fmt.Errorf("auth: nil Challenger")
	}
	return &ChallengerAdapter{challenger: c}, nil
}

// Challenge implements port.Challenger. It produces a fresh challenge via
// the legacy Challenger and parses the generated header back into a
// model.Challenge so the domain layer sees exactly what the wire carries —
// including the algorithm the operator configured and the GB 35114 Note
// when Note-integrity mode is active. The nonce is also returned by the
// legacy layer so callers can build a nonce-store on top.
func (a *ChallengerAdapter) Challenge(realm string) (model.Challenge, error) {
	headerValue, _, err := a.challenger.Challenge(realm)
	if err != nil {
		return model.Challenge{}, err
	}
	ch, err := ParseChallenge(headerValue)
	if err != nil {
		return model.Challenge{}, err
	}
	if ch.Realm() != realm {
		return model.Challenge{}, fmt.Errorf("auth: challenge realm %q does not match request %q", ch.Realm(), realm)
	}
	return ch, nil
}

// AuthorizerAdapter implements port.Authorizer by wrapping the Authorizer
// that renders client-side Digest credentials. It is what lets the app
// layer answer a 401 without importing this package: the app sees only the
// domain port and a model.Header.
type AuthorizerAdapter struct {
	authorizer *Authorizer
}

var _ port.Authorizer = (*AuthorizerAdapter)(nil)

// NewAuthorizerAdapter returns a port.Authorizer backed by a. A nil
// Authorizer is rejected so a mis-wired composition root fails fast
// instead of emitting empty Authorization headers.
func NewAuthorizerAdapter(a *Authorizer) (*AuthorizerAdapter, error) {
	if a == nil {
		return nil, fmt.Errorf("auth: nil Authorizer")
	}
	return &AuthorizerAdapter{authorizer: a}, nil
}

// Authorize implements port.Authorizer. It delegates to the Authorizer and
// wraps the rendered value in a model.Header named "Authorization".
func (a *AuthorizerAdapter) Authorize(challenge string, cred model.Credentials, method, uri string) (model.Header, error) {
	ch, err := ParseChallenge(challenge)
	if err != nil {
		return model.Header{}, err
	}
	value, err := a.authorizer.Authorization(cred, ch, method, uri)
	if err != nil {
		return model.Header{}, err
	}
	// model.NewHeader panics on CR/LF, and a credential is attacker- or
	// operator-controlled text, so refuse it as an error instead.
	if strings.ContainsAny(value, "\r\n") {
		return model.Header{}, fmt.Errorf("%w: rendered Authorization contains CR/LF", ErrMalformedAuthorization)
	}
	return model.NewHeader("Authorization", value), nil
}

// reqStub is the minimum contract the legacy Responder.Verify needs from a
// SIP request. It is never exposed outside this package.
type reqStub struct {
	method string
	auth   string
}

func (r reqStub) Method() string        { return r.method }
func (r reqStub) Authorization() string { return r.auth }

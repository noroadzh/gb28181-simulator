package auth

import (
	"errors"
	"fmt"
	"strings"

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

var _ port.Authenticator = (*AuthenticatorAdapter)(nil)

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
	err := a.responder.Verify(reqStub{method: req.Method(), auth: h.Value()}, cred.Password())
	if err == nil {
		return nil
	}
	// Only a parse failure may be challenged again; everything else —
	// including an algorithm we refuse to compute — is a refusal.
	if errors.Is(err, ErrMalformedAuthorization) {
		return fmt.Errorf("%w: %w", port.ErrMalformedCredentials, err)
	}
	return fmt.Errorf("%w: %w", port.ErrInvalidCredentials, err)
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

// Challenge implements port.Challenger. It produces a fresh nonce via the
// legacy Challenger and wraps the result in a model.Challenge. The nonce
// (returned by the legacy layer) is preserved so callers can build a
// nonce-store on top.
func (a *ChallengerAdapter) Challenge(realm string) (model.Challenge, error) {
	_, nonce, err := a.challenger.Challenge(realm)
	if err != nil {
		return model.Challenge{}, err
	}
	// model.Challenge carries the minimum the domain layer needs to build
	// a WWW-Authenticate header: realm, nonce, algorithm, opaque and qop.
	// The full header value returned by the legacy layer is retained by
	// the caller via port.Challenger metadata.
	return model.NewChallenge(realm, nonce, "MD5", "", "auth")
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

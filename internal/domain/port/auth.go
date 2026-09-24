// Package port — Authenticator / Challenger.
package port

import (
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Authenticator verifies an inbound Authorization header against a
// configured credential. Adapters implementing this port are framework-free:
// they take a model.Message (already stripped of transport headers) and
// return nil on success or a sentinel error on parse/credential failure.
//
// Per GB/T 28181 §L.2 a wrong response MUST NOT trigger a re-challenge;
// callers should map ErrInvalidResponse to 403 Forbidden.
type Authenticator interface {
	// Verify checks the Authorization header in req against cred.
	// Returns:
	//   nil                 — Authorization is valid
	//   ErrInvalidResponse  — header parsed, but response does not match
	//   ErrMalformed        — header could not be parsed
	//   other error         — backend failure (e.g. crypto error)
	Verify(req model.Message, cred model.Credentials) error
}

// Authorizer is the client half of a Digest exchange: given a challenge a
// peer sent and this node's credentials, it renders the Authorization
// header to answer with. It is the outbound counterpart of Authenticator,
// which verifies an inbound header.
type Authorizer interface {
	// Authorize renders an Authorization header answering challenge — the
	// verbatim value of the WWW-Authenticate header a peer sent — for the
	// given request. Parsing the challenge is the adapter's job: the app
	// layer only carries the header value around.
	// Returns:
	//   a header  — ready to be placed on the outgoing request
	//   ErrUnknownAlgorithm — the challenge demands a digest we cannot compute
	//   other error         — the challenge is unusable (no realm / no nonce)
	Authorize(challenge string, cred model.Credentials, method, uri string) (model.Header, error)
}

// Challenger generates a fresh WWW-Authenticate header value. Stateless
// from the caller's perspective: every call returns a new nonce.
type Challenger interface {
	// Challenge builds a Challenge for the supplied realm. The returned
	// value carries a fresh nonce; replay protection (nonce store, TTL)
	// is the caller's responsibility, not the adapter's.
	Challenge(realm string) (model.Challenge, error)
}

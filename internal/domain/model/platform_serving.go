// Package model — PlatformServing.
package model

import (
	"fmt"
	"strings"
)

// PlatformServing is how a platform-large node serves its downstreams: the
// realm it challenges them in, the lifetime window it grants, and whether
// it will accept an empty password from a downstream that also opted into
// the no-auth flow.
//
// It deliberately carries no accounts. A platform's accounts are secrets,
// and everything hanging off a NodeProfile travels to places that must stay
// secret-free (HTTP responses, logs); they live behind a credential store
// instead. AllowNoAuth is a server-side policy bit, not a secret, so it is
// safe to round-trip through places that must remain secret-free.
type PlatformServing struct {
	realm       string
	policy      ExpiresPolicy
	allowNoAuth bool
}

// NewPlatformServing builds the serving side of a platform node. realm is
// mandatory — a challenge without one is unusable; an absent policy takes
// the documented defaults (60 / 3600 / 86400 seconds).
func NewPlatformServing(realm string, policy ExpiresPolicy) (PlatformServing, error) {
	return NewPlatformServingWithNoAuth(realm, policy, false)
}

// NewPlatformServingWithNoAuth is NewPlatformServing with the no-auth
// server-side flag explicit. The flag is a policy choice, not a secret;
// callers that persist the value round-trip it through this constructor.
func NewPlatformServingWithNoAuth(realm string, policy ExpiresPolicy, allowNoAuth bool) (PlatformServing, error) {
	realm = strings.TrimSpace(realm)
	if realm == "" {
		return PlatformServing{}, fmt.Errorf("model: empty realm for a platform node")
	}
	if !policy.HasPolicy() {
		var err error
		if policy, err = NewExpiresPolicy(0, 0, 0); err != nil {
			return PlatformServing{}, err
		}
	}
	return PlatformServing{realm: realm, policy: policy, allowNoAuth: allowNoAuth}, nil
}

// DefaultPlatformServing builds serving for a node that declared nothing:
// the node's own home domain as the realm and the default lifetime window.
func DefaultPlatformServing(domain string) (PlatformServing, error) {
	return NewPlatformServing(domain, ExpiresPolicy{})
}

// Realm returns the realm the platform challenges downstreams in.
func (p PlatformServing) Realm() string { return p.realm }

// Policy returns the lifetime window the platform grants.
func (p PlatformServing) Policy() ExpiresPolicy { return p.policy }

// AllowNoAuth reports whether the platform will accept a downstream that
// omitted its password and flagged itself as no-auth. A store
// round-tripping a node needs to see this so the database row can rebuild
// a PlatformServing without losing the policy bit; String() does not
// render it as a secret because it is a policy flag, not a credential.
func (p PlatformServing) AllowNoAuth() bool { return p.allowNoAuth }

// HasServing reports whether p was produced by NewPlatformServing.
func (p PlatformServing) HasServing() bool { return p.realm != "" }

// String renders a log-safe one-line summary; never contains secrets.
func (p PlatformServing) String() string {
	return fmt.Sprintf("PlatformServing<realm=%s expires=%d/%d/%d no_auth=%v>",
		p.realm, p.policy.Min(), p.policy.Default(), p.policy.Max(), p.allowNoAuth)
}

// Package model — PlatformServing.
package model

import (
	"fmt"
	"strings"
)

// PlatformServing is how a platform-large node serves its downstreams: the
// realm it challenges them in and the lifetime window it grants.
//
// It deliberately carries no accounts. A platform's accounts are secrets,
// and everything hanging off a NodeProfile travels to places that must stay
// secret-free (HTTP responses, logs); they live behind a credential store
// instead.
type PlatformServing struct {
	realm  string
	policy ExpiresPolicy
}

// NewPlatformServing builds the serving side of a platform node. realm is
// mandatory — a challenge without one is unusable; an absent policy takes
// the documented defaults (60 / 3600 / 86400 seconds).
func NewPlatformServing(realm string, policy ExpiresPolicy) (PlatformServing, error) {
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
	return PlatformServing{realm: realm, policy: policy}, nil
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

// HasServing reports whether p was produced by NewPlatformServing.
func (p PlatformServing) HasServing() bool { return p.realm != "" }

// String renders a log-safe one-line summary; never contains secrets.
func (p PlatformServing) String() string {
	return fmt.Sprintf("PlatformServing<realm=%s expires=%d/%d/%d>",
		p.realm, p.policy.Min(), p.policy.Default(), p.policy.Max())
}

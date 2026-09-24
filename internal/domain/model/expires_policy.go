// Package model — ExpiresPolicy.
package model

import (
	"fmt"
	"time"
)

// Defaults for the lifetime a platform grants its downstreams. A minute is
// the shortest worth re-registering for, an hour is the GB/T 28181 norm,
// and a day is the point beyond which a silent device would linger in the
// online table for far too long.
const (
	DefaultPlatformMinExpires uint32 = 60
	DefaultPlatformExpires    uint32 = 3600
	DefaultPlatformMaxExpires uint32 = 86400
)

// ExpiresPolicy is the window a platform grants: the shortest lifetime it
// will accept, the one it grants when a downstream asks for nothing in
// particular, and the longest it will allow.
//
// A platform never grants outside the window — a downstream asking for a
// week gets a day, one asking for a second gets a minute — because the
// window is what bounds how stale the online device table can get.
type ExpiresPolicy struct {
	min uint32
	def uint32
	max uint32
}

// NewExpiresPolicy builds a policy, applying the documented defaults (60 /
// 3600 / 86400 seconds) to any zero field. The window must be sane:
// 0 < min <= def <= max.
func NewExpiresPolicy(min, def, max uint32) (ExpiresPolicy, error) {
	if min == 0 {
		min = DefaultPlatformMinExpires
	}
	if def == 0 {
		def = DefaultPlatformExpires
	}
	if max == 0 {
		max = DefaultPlatformMaxExpires
	}
	if min == 0 {
		return ExpiresPolicy{}, fmt.Errorf("model: non-positive min expires %d", min)
	}
	if def < min {
		return ExpiresPolicy{}, fmt.Errorf("model: default expires %d is shorter than the minimum %d", def, min)
	}
	if max < def {
		return ExpiresPolicy{}, fmt.Errorf("model: max expires %d is shorter than the default %d", max, def)
	}
	return ExpiresPolicy{min: min, def: def, max: max}, nil
}

// Min returns the shortest lifetime the platform grants.
func (p ExpiresPolicy) Min() uint32 { return p.min }

// Default returns the lifetime granted to a downstream that asks for none.
func (p ExpiresPolicy) Default() uint32 { return p.def }

// Max returns the longest lifetime the platform grants.
func (p ExpiresPolicy) Max() uint32 { return p.max }

// HasPolicy reports whether p was produced by NewExpiresPolicy.
func (p ExpiresPolicy) HasPolicy() bool { return p.max != 0 }

// Negotiate clamps a requested lifetime into the window. A request of 0
// means "nothing in particular" and gets Default.
//
// An explicit `Expires: 0` on the wire is an unregistration, not a request
// for the default: callers MUST detect it before calling Negotiate, because
// here 0 is indistinguishable from an absent header.
func (p ExpiresPolicy) Negotiate(requested uint32) uint32 {
	if requested == 0 {
		return p.def
	}
	if requested < p.min {
		return p.min
	}
	if requested > p.max {
		return p.max
	}
	return requested
}

// ExpiresAt turns a granted lifetime into the wall-clock moment it lapses,
// measured from at. It mirrors RegistrationResult.ExpiresAt so the online
// device table and the device's own bookkeeping agree.
func (p ExpiresPolicy) ExpiresAt(granted uint32, at time.Time) time.Time {
	return at.Add(time.Duration(granted) * time.Second)
}

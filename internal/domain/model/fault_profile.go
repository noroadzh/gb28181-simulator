// Package model — FaultProfile: opt-in misbehaviour injected into one
// node's signalling.
package model

import (
	"fmt"
	"strings"
	"time"
)

// FaultAction names the kind of anomaly a fault profile produced. The
// counters exposed through the node detail API are keyed by these values.
type FaultAction string

const (
	// FaultCannedResponse — the node answered with a canned status instead
	// of running the normal handler.
	FaultCannedResponse FaultAction = "canned_response"
	// FaultDelay — the node delayed its response.
	FaultDelay FaultAction = "delay"
	// FaultDrop — the node silently dropped an inbound request.
	FaultDrop FaultAction = "drop"
	// FaultBlackhole — the node ignored a request for a black-holed method.
	FaultBlackhole FaultAction = "blackhole"
	// FaultUnsupportedMethod — the node answered an unknown method with a
	// configured status (usually 501).
	FaultUnsupportedMethod FaultAction = "unsupported_method"
)

// FaultDelayConfig is the response-delay part of a profile: every response
// is withheld for Base, plus up to Jitter of randomness when jitter > 0.
// The zero struct means "no delay".
type FaultDelayConfig struct {
	Base   time.Duration `json:"base" yaml:"base"`
	Jitter time.Duration `json:"jitter" yaml:"jitter"`
}

// IsZero reports whether delay injection is configured off.
func (d FaultDelayConfig) IsZero() bool { return d.Base <= 0 }

// FaultProfile is one node's complete misbehaviour plan. The zero value is
// the "behaves normally" profile: installing it is a no-op by construction,
// which is what keeps default-off byte-identical.
//
// Evaluation order for an inbound request (see design D6):
//  1. blackhole (method listed) → silent ignore
//  2. drop (rand < Drop) → silent ignore
//  3. delay → sleep before answering
//  4. canned (method mapped) → answer with that status, skip the handler
type FaultProfile struct {
	// Canned maps an upper-case SIP method to a response status code
	// (400-699). A matching request is answered by the canned response
	// instead of the normal handler.
	Canned map[string]int `json:"canned,omitempty" yaml:"canned,omitempty"`

	// Delay withholds every response for Base (plus jitter).
	Delay FaultDelayConfig `json:"delay,omitempty" yaml:"delay,omitempty"`

	// Drop is the probability in [0,1] that an inbound request is silently
	// discarded. 0 disables dropping.
	Drop float64 `json:"drop,omitempty" yaml:"drop,omitempty"`

	// Blackhole lists upper-case SIP methods that never receive any
	// response for as long as the profile is installed.
	Blackhole []string `json:"blackhole,omitempty" yaml:"blackhole,omitempty"`

	// UnsupportedMethod is the status (400-699) answered for request
	// methods the node does not serve. 0 keeps the historical behaviour
	// (silent drop with a debug log).
	UnsupportedMethod int `json:"unsupportedMethod,omitempty" yaml:"unsupportedMethod,omitempty"`
}

// Validate checks the profile's invariants. It rejects status codes outside
// 400-699, probabilities outside [0,1], negative durations, and non
// upper-case method names — a fault that cannot be expressed as a valid SIP
// response must never reach the wire.
func (p FaultProfile) Validate() error {
	for method, status := range p.Canned {
		if method != strings.ToUpper(method) || method == "" {
			return fmt.Errorf("faults: canned method %q must be upper-case SIP method", method)
		}
		if status < 400 || status > 699 {
			return fmt.Errorf("faults: canned status %d for %s outside 400-699", status, method)
		}
	}
	if p.Delay.Base < 0 || p.Delay.Jitter < 0 {
		return fmt.Errorf("faults: negative delay")
	}
	if p.Drop < 0 || p.Drop > 1 {
		return fmt.Errorf("faults: drop probability %g outside [0,1]", p.Drop)
	}
	for _, method := range p.Blackhole {
		if method != strings.ToUpper(method) || method == "" {
			return fmt.Errorf("faults: blackhole method %q must be upper-case SIP method", method)
		}
	}
	if p.UnsupportedMethod != 0 && (p.UnsupportedMethod < 400 || p.UnsupportedMethod > 699) {
		return fmt.Errorf("faults: unsupportedMethod status %d outside 400-699", p.UnsupportedMethod)
	}
	return nil
}

// IsZero reports whether the profile changes nothing — the exact shape
// every node has unless a fault profile is installed.
func (p FaultProfile) IsZero() bool {
	return len(p.Canned) == 0 && p.Delay.IsZero() && p.Drop == 0 &&
		len(p.Blackhole) == 0 && p.UnsupportedMethod == 0
}

// blackholeSet caches Blackhole membership; rebuilt per call because
// profiles are tiny and immutable while installed.
func (p FaultProfile) isBlackholed(method string) bool {
	for _, m := range p.Blackhole {
		if m == method {
			return true
		}
	}
	return false
}

// cannedStatus returns the canned status for method and whether one is set.
func (p FaultProfile) cannedStatus(method string) (int, bool) {
	status, ok := p.Canned[method]
	return status, ok
}

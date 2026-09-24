// Package model — Node lifecycle status.
package model

import (
	"errors"
	"fmt"
)

// ErrIllegalTransition is the sentinel error returned when a requested
// status change is not present in the legal-transition table. Callers judge
// it with errors.Is. An illegal transition never changes the status and
// never panics.
var ErrIllegalTransition = errors.New("model: illegal node status transition")

// Status is the lifecycle status of a node. The zero value is Idle, which is
// also the status of a freshly constructed Node.
type Status int

const (
	// StatusIdle: created, not started.
	StatusIdle Status = iota
	// StatusRegistering: listener bound, registration in flight.
	StatusRegistering
	// StatusRegistered: registration accepted, not yet in session.
	StatusRegistered
	// StatusOnline: registered and actively signalling.
	StatusOnline
	// StatusOffline: stopped; listener released.
	StatusOffline
	// StatusFault: recoverable terminal state; only Idle (reset) or Offline
	// (removal) may follow.
	StatusFault
)

// statusNames is the wire/JSON spelling, lower case to match the HTTP API
// contract (GET /v1/nodes reports "registering" after a successful start).
var statusNames = map[Status]string{
	StatusIdle:        "idle",
	StatusRegistering: "registering",
	StatusRegistered:  "registered",
	StatusOnline:      "online",
	StatusOffline:     "offline",
	StatusFault:       "fault",
}

// String returns the lower-case spelling, or "unknown" for a value outside
// the enumeration so log lines never render an empty status.
func (s Status) String() string {
	if n, ok := statusNames[s]; ok {
		return n
	}
	return "unknown"
}

// ParseStatus maps the lower-case spelling back to a Status. The empty
// string is rejected so a missing field cannot pass silently.
func ParseStatus(s string) (Status, error) {
	for st, name := range statusNames {
		if name == s {
			return st, nil
		}
	}
	return StatusIdle, fmt.Errorf("model: unknown node status %q", s)
}

// legalTransitions is the explicit legal-transition table (design D2). A
// pair absent from this table is illegal. Fault is a recoverable terminal
// state: only Idle (reset) or Offline (removal) may follow it.
var legalTransitions = map[Status]map[Status]bool{
	StatusIdle: {
		StatusRegistering: true, // start
		StatusOffline:     true, // remove without starting
	},
	StatusRegistering: {
		StatusRegistered: true, // registration accepted
		StatusFault:      true, // registration failed after a restart
		StatusOffline:    true, // aborted
		StatusIdle:       true, // first start failed; rolled back to fresh
	},
	StatusRegistered: {
		StatusOnline:  true, // session established
		StatusFault:   true, // registration lost
		StatusOffline: true, // stopped
	},
	StatusOnline: {
		StatusOffline: true, // stopped
		StatusFault:   true, // connection lost
	},
	StatusOffline: {
		StatusIdle:        true, // reset
		StatusRegistering: true, // restarted
	},
	StatusFault: {
		StatusIdle:    true, // reset
		StatusOffline: true, // removed
	},
}

// CanTransition reports whether moving from s to to is legal. A transition
// to the same status is always false: "no change" is not a transition.
func (s Status) CanTransition(to Status) bool {
	return legalTransitions[s][to]
}

// LegalTargets returns the statuses reachable from s, sorted by the
// declaration order of the enumeration. Used by error messages and by the
// HTTP layer to explain a 409 conflict.
func (s Status) LegalTargets() []Status {
	out := make([]Status, 0, len(legalTransitions[s]))
	for _, candidate := range []Status{
		StatusIdle, StatusRegistering, StatusRegistered,
		StatusOnline, StatusOffline, StatusFault,
	} {
		if legalTransitions[s][candidate] {
			out = append(out, candidate)
		}
	}
	return out
}

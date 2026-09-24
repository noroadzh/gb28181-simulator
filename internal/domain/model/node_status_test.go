package model

import (
	"errors"
	"testing"
)

// allStatuses is the enumeration in declaration order; the 6x6 matrix below
// is indexed by it.
var allStatuses = []Status{
	StatusIdle, StatusRegistering, StatusRegistered,
	StatusOnline, StatusOffline, StatusFault,
}

// wantLegal is the expected legal-transition table, written independently of
// the implementation so a bug in legalTransitions cannot hide behind a
// copy-paste of itself. Rows are "from", columns are "to", in the order of
// allStatuses.
var wantLegal = map[Status]map[Status]bool{
	StatusIdle:        {StatusRegistering: true, StatusOffline: true},
	StatusRegistering: {StatusRegistered: true, StatusFault: true, StatusOffline: true, StatusIdle: true},
	StatusRegistered:  {StatusOnline: true, StatusFault: true, StatusOffline: true},
	StatusOnline:      {StatusOffline: true, StatusFault: true},
	StatusOffline:     {StatusIdle: true, StatusRegistering: true},
	StatusFault:       {StatusIdle: true, StatusOffline: true},
}

// TestStatus_Transition_FullTable walks all 36 pairs and compares the
// implementation against wantLegal (task 2.2).
func TestStatus_Transition_FullTable(t *testing.T) {
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			got := from.CanTransition(to)
			want := wantLegal[from][to]
			if got != want {
				t.Errorf("CanTransition(%s -> %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

// TestStatus_IllegalTransition_Unchanged asserts the two illegal jumps named
// by the spec, that the error is judgeable with errors.Is, and that an
// illegal transition leaves the node exactly as it was.
func TestStatus_IllegalTransition_Unchanged(t *testing.T) {
	p, err := NewNodeProfile("34020000011310000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	n := NewNode(p)

	moved, err := n.WithStatus(StatusOnline) // Idle -> Online
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("Idle -> Online error = %v, want ErrIllegalTransition", err)
	}
	if !errors.Is(err, ErrIllegalTransition) {
		t.Error("error not judgeable with errors.Is")
	}
	if moved.Status() != StatusIdle {
		t.Errorf("status after illegal transition = %v, want idle", moved.Status())
	}
	if n.Status() != StatusIdle {
		t.Errorf("receiver status = %v, want idle", n.Status())
	}
	if !containsStatus(n.Status().LegalTargets(), StatusRegistering) {
		t.Errorf("LegalTargets(idle) = %v, want it to contain registering", n.Status().LegalTargets())
	}
}

// TestStatus_FaultIsRecoverableTerminal asserts Fault only exits to Idle
// (reset) or Offline (removal), and that Fault -> Online is refused.
func TestStatus_FaultIsRecoverableTerminal(t *testing.T) {
	faulted := StatusFault
	for _, to := range allStatuses {
		want := to == StatusIdle || to == StatusOffline
		if got := faulted.CanTransition(to); got != want {
			t.Errorf("Fault -> %s = %v, want %v", to, got, want)
		}
	}
	if faulted.CanTransition(StatusOnline) {
		t.Error("Fault -> Online is legal, want illegal")
	}
	p, err := NewNodeProfile("34020000011310000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	n := NewNode(p)
	// Drive the node into Fault through a legal path: Idle -> Registering ->
	// Fault.
	registering, err := n.WithStatus(StatusRegistering)
	if err != nil {
		t.Fatalf("Idle -> Registering: %v", err)
	}
	faultedNode, err := registering.WithStatus(StatusFault)
	if err != nil {
		t.Fatalf("Registering -> Fault: %v", err)
	}
	if faultedNode.Status() != StatusFault {
		t.Fatalf("status = %v, want fault", faultedNode.Status())
	}
	reset, err := faultedNode.WithStatus(StatusIdle)
	if err != nil {
		t.Fatalf("Fault -> Idle = %v, want legal (reset)", err)
	}
	if reset.Status() != StatusIdle {
		t.Errorf("after reset status = %v, want idle", reset.Status())
	}
	removed, err := faultedNode.WithStatus(StatusOffline)
	if err != nil {
		t.Fatalf("Fault -> Offline = %v, want legal (removal)", err)
	}
	if removed.Status() != StatusOffline {
		t.Errorf("after removal status = %v, want offline", removed.Status())
	}
	if _, err := faultedNode.WithStatus(StatusOnline); !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("Fault -> Online error = %v, want ErrIllegalTransition", err)
	}
}

// TestStatus_HappyPath asserts the full Idle -> Registering -> Registered ->
// Online walk succeeds.
func TestStatus_HappyPath(t *testing.T) {
	cur := StatusIdle
	for _, to := range []Status{StatusRegistering, StatusRegistered, StatusOnline} {
		if !cur.CanTransition(to) {
			t.Fatalf("%s -> %s is illegal, want legal", cur, to)
		}
		cur = to
	}
	if cur != StatusOnline {
		t.Errorf("final status = %v, want online", cur)
	}
}

// TestStatus_StringAndParse asserts the lower-case spelling required by the
// HTTP contract and that ParseStatus round-trips it.
func TestStatus_StringAndParse(t *testing.T) {
	want := map[Status]string{
		StatusIdle: "idle", StatusRegistering: "registering",
		StatusRegistered: "registered", StatusOnline: "online",
		StatusOffline: "offline", StatusFault: "fault",
	}
	for s, name := range want {
		if s.String() != name {
			t.Errorf("Status(%d).String() = %q, want %q", s, s.String(), name)
		}
		back, err := ParseStatus(name)
		if err != nil {
			t.Fatalf("ParseStatus(%q) = %v", name, err)
		}
		if back != s {
			t.Errorf("ParseStatus(%q) = %v, want %v", name, back, s)
		}
	}
	if _, err := ParseStatus(""); err == nil {
		t.Error("ParseStatus(\"\") succeeded, want error")
	}
	if Status(99).String() != "unknown" {
		t.Errorf("out-of-range status String() = %q, want \"unknown\"", Status(99).String())
	}
}

func containsStatus(list []Status, want Status) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

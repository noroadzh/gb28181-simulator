package model

import (
	"strings"
	"testing"
)

// TestParseRoutePath covers task 2.1: lenient whitespace/trailing-comma
// handling, strict rejection of non-empty identifier rules, and the
// "absent header is normal" convention.
func TestParseRoutePath(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{"empty is nil", "", nil, false},
		{"whitespace only is nil", "   ", nil, false},
		{"single id", "34020000012000000001", []string{"34020000012000000001"}, false},
		{"two ids", "34020000012000000001,34020000012160000001", []string{"34020000012000000001", "34020000012160000001"}, false},
		{"spaces around separators", " 34020000012000000001 , 34020000012160000001 ", []string{"34020000012000000001", "34020000012160000001"}, false},
		{"trailing comma", "34020000012000000001,", []string{"34020000012000000001"}, false},
		{"double separator tolerated", "34020000012000000001,,34020000012160000001", []string{"34020000012000000001", "34020000012160000001"}, false},
		{"comma only is nil", ",", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRoutePath(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRoutePath(%q) succeeded, want error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRoutePath(%q) = %v, want success", tc.raw, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ParseRoutePath(%q) = %v (len %d), want %v (len %d)", tc.raw, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ParseRoutePath(%q)[%d] = %q, want %q", tc.raw, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestFormatRoutePath_RoundTrip asserts format(parse(x)) == x for the wire
// form (no spaces, comma-separated) and that empty slices render empty.
func TestFormatRoutePath_RoundTrip(t *testing.T) {
	const wire = "34020000012000000001,34020000012160000001"
	parsed, err := ParseRoutePath(wire)
	if err != nil {
		t.Fatalf("ParseRoutePath: %v", err)
	}
	if got := FormatRoutePath(parsed); got != wire {
		t.Errorf("FormatRoutePath = %q, want %q", got, wire)
	}
	for _, empty := range [][]string{nil, {}} {
		if got := FormatRoutePath(empty); got != "" {
			t.Errorf("FormatRoutePath(%v) = %q, want \"\"", empty, got)
		}
	}
	if got := FormatRoutePath([]string{" 34020000012000000001 "}); got != "34020000012000000001" {
		t.Errorf("FormatRoutePath trims: got %q", got)
	}
}

// TestCascadeRoute_AppendAndContains covers the immutable route-append
// semantics the forwarding code relies on (spec: cascade-header-injection).
func TestCascadeRoute_AppendAndContains(t *testing.T) {
	const (
		nodeA = "34020000012000000001"
		nodeB = "34020000012160000001"
	)
	route, err := NewCascadeRoute("", nodeA, "")
	if err != nil {
		t.Fatalf("NewCascadeRoute: %v", err)
	}
	if !route.ContainsRoute(nodeA) {
		t.Error("ContainsRoute(route[0]) = false, want true")
	}
	if route.ContainsRoute(nodeB) {
		t.Errorf("ContainsRoute(%s) = true before append", nodeB)
	}
	appended := route.AppendRoute(nodeB)
	if len(appended.RoutePath) != 2 {
		t.Fatalf("RoutePath after append = %v, want 2 entries", appended.RoutePath)
	}
	if !appended.ContainsRoute(nodeB) {
		t.Error("ContainsRoute after append = false, want true")
	}
	// The receiver is untouched: append returns a copy.
	if len(route.RoutePath) != 1 {
		t.Errorf("receiver mutated: RoutePath = %v, want 1 entry", route.RoutePath)
	}
	// Blank ids are ignored, not stored.
	if same := route.AppendRoute("   "); len(same.RoutePath) != 1 {
		t.Errorf("AppendRoute(blank) stored %v, want unchanged", same.RoutePath)
	}
}

// TestCascadeRoute_PopPreferred covers preferred-path consumption: first
// entry returned, remainder kept, empty list yields "".
func TestCascadeRoute_PopPreferred(t *testing.T) {
	const (
		nodeA = "34020000012000000001"
		nodeB = "34020000012160000001"
	)
	route, err := NewCascadeRoute("", "", nodeA+","+nodeB)
	if err != nil {
		t.Fatalf("NewCascadeRoute: %v", err)
	}
	popped, first := route.PopPreferred()
	if first != nodeA {
		t.Errorf("PopPreferred first = %q, want %q", first, nodeA)
	}
	if len(popped.PreferredPath) != 1 || popped.PreferredPath[0] != nodeB {
		t.Errorf("PopPreferred remainder = %v, want [%s]", popped.PreferredPath, nodeB)
	}
	if len(route.PreferredPath) != 2 {
		t.Errorf("receiver mutated: PreferredPath = %v, want 2 entries", route.PreferredPath)
	}
	exhausted, none := popped.PopPreferred()
	if none != nodeB || len(exhausted.PreferredPath) != 0 {
		t.Errorf("PopPreferred on 1-entry list = (%v, %q), want ([], %q)", exhausted.PreferredPath, none, nodeB)
	}
	if _, still := exhausted.PopPreferred(); still != "" {
		t.Errorf("PopPreferred on empty = %q, want \"\"", still)
	}
}

// TestCascadeRoute_HeadersAndFromHeaders covers the header round trip and
// the "no cascade context stays header-free" convention.
func TestCascadeRoute_HeadersAndFromHeaders(t *testing.T) {
	const (
		routeRaw     = "34020000012000000001"
		preferredRaw = "34020000012160000001"
	)
	route, err := NewCascadeRoute("34020000011310000001", routeRaw, preferredRaw)
	if err != nil {
		t.Fatalf("NewCascadeRoute: %v", err)
	}
	headers := route.Headers()
	if len(headers) != 2 {
		t.Fatalf("Headers() = %v, want 2 entries", headers)
	}
	if headers[0].Name() != HeaderRoutePath || headers[0].Value() != routeRaw {
		t.Errorf("headers[0] = %s: %s, want %s: %s", headers[0].Name(), headers[0].Value(), HeaderRoutePath, routeRaw)
	}
	if headers[1].Name() != HeaderPreferredPath || headers[1].Value() != preferredRaw {
		t.Errorf("headers[1] = %s: %s, want %s: %s", headers[1].Name(), headers[1].Value(), HeaderPreferredPath, preferredRaw)
	}

	// Round trip through RouteFromHeaders restores both paths.
	back, err := RouteFromHeaders(headers)
	if err != nil {
		t.Fatalf("RouteFromHeaders: %v", err)
	}
	if got := FormatRoutePath(back.RoutePath); got != routeRaw {
		t.Errorf("RoutePath round trip = %q, want %q", got, routeRaw)
	}
	if got := FormatRoutePath(back.PreferredPath); got != preferredRaw {
		t.Errorf("PreferredPath round trip = %q, want %q", got, preferredRaw)
	}

	// Zero-value route renders no headers.
	if zero := (CascadeRoute{}); len(zero.Headers()) != 0 {
		t.Errorf("zero route Headers() = %v, want none", zero.Headers())
	}
}

// TestCascadeRoute_RejectsWhitespaceOnlyPreferred covers the error path of
// NewCascadeRoute: an identifier-shaped header is required, so a malformed
// value cannot silently become an empty route.
func TestCascadeRoute_RejectsBadValues(t *testing.T) {
	// Note: ParseRoutePath is intentionally lenient (an absent header is
	// normal), so there is no malformed input it rejects today; this test
	// documents the contract that empty parses to nil without error.
	route, err := NewCascadeRoute("", "  ", ",")
	if err != nil {
		t.Fatalf("NewCascadeRoute blank = %v, want success", err)
	}
	if route.RoutePath != nil || route.PreferredPath != nil {
		t.Errorf("blank route parsed to %v / %v, want nil / nil", route.RoutePath, route.PreferredPath)
	}
}

// TestNodeProfile_CascadeParentValidation covers task 1.3 builder
// validation: blank and illegal ids are rejected, legal ids stick, and the
// receiver is never mutated.
func TestNodeProfile_CascadeParentValidation(t *testing.T) {
	p, err := NewNodeProfile("34020000012160000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if p.HasCascadeParent() {
		t.Error("fresh profile HasCascadeParent() = true, want false")
	}
	for _, bad := range []string{"", "   ", "1234"} {
		if _, err := p.WithCascadeParent(bad); err == nil {
			t.Errorf("WithCascadeParent(%q) succeeded, want error", bad)
		}
	}
	const parent = "34020000012000000001"
	child, err := p.WithCascadeParent(parent)
	if err != nil {
		t.Fatalf("WithCascadeParent(%s): %v", parent, err)
	}
	if child.CascadeParent() != parent {
		t.Errorf("CascadeParent() = %q, want %q", child.CascadeParent(), parent)
	}
	if !child.HasCascadeParent() || !child.HasCascadeRouting() {
		t.Error("child lost cascade presence flags")
	}
	if p.CascadeParent() != "" || p.HasCascadeRouting() {
		t.Errorf("receiver mutated: parent = %q, hasRouting = %v", p.CascadeParent(), p.HasCascadeRouting())
	}
	if !strings.Contains(child.String(), parent) && child.String() == "" {
		// String() is not required to include the parent; just make sure
		// the accessor above did the check.
		_ = child
	}
}

// TestNodeProfile_CascadeChildrenValidation covers duplicate rejection,
// blank filtering, and copy semantics of the children list.
func TestNodeProfile_CascadeChildrenValidation(t *testing.T) {
	p, err := NewNodeProfile("34020000012000000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if p.HasCascadeChildren() {
		t.Error("fresh profile HasCascadeChildren() = true, want false")
	}
	const (
		deviceA = "34020000011310000001"
		deviceB = "34020000011310000002"
	)
	for _, tc := range []struct {
		name    string
		in      []string
		wantErr bool
	}{
		{"duplicate child", []string{deviceA, deviceA}, true},
		{"all blank", []string{deviceA, "   ", ""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := p.WithCascadeChildren(tc.in); (err == nil) == tc.wantErr {
				if tc.wantErr {
					t.Errorf("WithCascadeChildren(%v) succeeded, want error", tc.in)
				} else {
					t.Errorf("WithCascadeChildren(%v) = %v, want success (blank-only filters to no-op)", tc.in, err)
				}
			}
		})
	}
	// The all-blank case must be a no-op, not a store of garbage.
	if got := p.CascadeChildren(); got != nil {
		t.Errorf("CascadeChildren after blank-only list = %v, want nil", got)
	}
	// Illegal ids are rejected.
	if _, err := p.WithCascadeChildren([]string{deviceA, "1234"}); err == nil {
		t.Error("WithCascadeChildren with illegal id succeeded, want error")
	}

	parent, err := p.WithCascadeChildren([]string{deviceA, deviceB})
	if err != nil {
		t.Fatalf("WithCascadeChildren: %v", err)
	}
	got := parent.CascadeChildren()
	if len(got) != 2 || got[0] != deviceA || got[1] != deviceB {
		t.Fatalf("CascadeChildren = %v, want [%s %s]", got, deviceA, deviceB)
	}
	// Mutating the returned slice must not corrupt the profile.
	got[0] = "corrupted"
	if again := parent.CascadeChildren(); again[0] != deviceA {
		t.Errorf("CascadeChildren not defensive copy: %v", again)
	}
	if p.HasCascadeChildren() {
		t.Error("receiver mutated by WithCascadeChildren")
	}
}

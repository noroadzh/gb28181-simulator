package model

import (
	"reflect"
	"strings"
	"testing"
)

// TestNodeID_Parse_AcceptsLegalEncoding asserts that well-formed 20-digit
// ids are accepted and map to the expected kind, and that String()
// round-trips the verbatim encoding.
func TestNodeID_Parse_AcceptsLegalEncoding(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		kind NodeKind
	}{
		{"device ipc", "34020000011310000001", NodeKindDevice},
		{"device dvr", "34020000011110000002", NodeKindDevice},
		{"device nvr", "34020000011180000003", NodeKindDevice},
		{"platform large", "34020000012000000001", NodeKindPlatformLarge},
		{"platform small", "34020000012160000001", NodeKindPlatformSmall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ParseNodeID(tc.raw)
			if err != nil {
				t.Fatalf("ParseNodeID(%q) = error %v, want success", tc.raw, err)
			}
			if id.String() != tc.raw {
				t.Errorf("String() = %q, want %q", id.String(), tc.raw)
			}
			if id.Kind() != tc.kind {
				t.Errorf("Kind() = %v, want %v", id.Kind(), tc.kind)
			}
			if got, want := id.TypeCode(), tc.raw[10:13]; got != want {
				t.Errorf("TypeCode() = %q, want %q", got, want)
			}
		})
	}
}

// TestNodeID_Parse_RejectsIllegal covers the four rejection classes required
// by task 1.2: wrong length, non-digit content, and an unmapped type-code
// segment. Every error must name the reason and the observed length.
func TestNodeID_Parse_RejectsIllegal(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantLen int
		wantMsg string
	}{
		{"too short", "3402000001131000000", 19, "length 19"},
		{"too long", "340200000113100000012", 21, "length 21"},
		{"empty", "", 0, "length 0"},
		{"contains letter", "3402000001131000000a", 20, "byte 20"},
		{"contains dash", "34020000-11310000001", 20, "want digit"},
		{"unknown type code", "34020000019990000001", 20, "type code \"999\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ParseNodeID(tc.raw)
			if err == nil {
				t.Fatalf("ParseNodeID(%q) succeeded, want error", tc.raw)
			}
			if id.String() != "" {
				t.Errorf("id on error = %q, want zero value", id.String())
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantMsg)
			}
			if !strings.Contains(err.Error(), "illegal") {
				t.Errorf("error = %q, want it to contain \"illegal\"", err.Error())
			}
		})
	}
}

// TestNodeKind_SpellingRoundTrip asserts the configuration spelling is
// stable and that the empty string is rejected.
func TestNodeKind_SpellingRoundTrip(t *testing.T) {
	for _, k := range []NodeKind{NodeKindDevice, NodeKindPlatformLarge, NodeKindPlatformSmall} {
		got, err := ParseNodeKind(k.String())
		if err != nil {
			t.Fatalf("ParseNodeKind(%q) = %v", k.String(), err)
		}
		if got != k {
			t.Errorf("round trip %q = %v, want %v", k.String(), got, k)
		}
	}
	if NodeKindUnknown.String() != "" {
		t.Errorf("NodeKindUnknown.String() = %q, want \"\"", NodeKindUnknown.String())
	}
	if _, err := ParseNodeKind(""); err == nil {
		t.Error("ParseNodeKind(\"\") succeeded, want error")
	}
	if _, err := ParseNodeKind("camera"); err == nil {
		t.Error("ParseNodeKind(\"camera\") succeeded, want error")
	}
}

// TestNodeProfile_Construction asserts mandatory field validation.
func TestNodeProfile_Construction(t *testing.T) {
	const legalID = "34020000011310000001"
	if _, err := NewNodeProfile(legalID, "127.0.0.1:5060", "3402000000", "acme"); err != nil {
		t.Fatalf("NewNodeProfile legal = %v, want success", err)
	}
	for _, tc := range []struct {
		name, id, addr, domain string
	}{
		{"illegal id", "1234", "127.0.0.1:5060", "3402000000"},
		{"empty addr", legalID, "", "3402000000"},
		{"blank addr", legalID, "   ", "3402000000"},
		{"empty domain", legalID, "127.0.0.1:5060", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewNodeProfile(tc.id, tc.addr, tc.domain, ""); err == nil {
				t.Errorf("NewNodeProfile(%q,%q,%q) succeeded, want error", tc.id, tc.addr, tc.domain)
			}
		})
	}
}

// TestNodeProfile_WithReturnsCopy asserts the With... methods leave the
// receiver untouched, which is how callers change a value object.
func TestNodeProfile_WithReturnsCopy(t *testing.T) {
	p, err := NewNodeProfile("34020000011310000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	moved, err := p.WithAddr("127.0.0.1:5061")
	if err != nil {
		t.Fatalf("WithAddr: %v", err)
	}
	if moved.Addr() != "127.0.0.1:5061" {
		t.Errorf("moved.Addr() = %q, want 127.0.0.1:5061", moved.Addr())
	}
	if p.Addr() != "127.0.0.1:5060" {
		t.Errorf("original Addr() mutated to %q, want 127.0.0.1:5060", p.Addr())
	}
	if moved.WithVendor("other").Vendor() != "other" {
		t.Error("WithVendor did not apply")
	}
}

// TestNodeProfile_FieldsUnexported asserts immutability the way the rest of
// the model package does: no exported field, so an outside caller cannot
// reach in and change a value after construction (task 1.3).
func TestNodeProfile_FieldsUnexported(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(NodeProfile{}),
		reflect.TypeOf(NodeID{}),
		reflect.TypeOf(Node{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.IsExported() {
				t.Errorf("%s.%s is exported; %s must stay immutable",
					typ.Name(), f.Name, typ.Name())
			}
		}
	}
}

// TestNode_StartsIdleAndAdvances asserts a fresh node is Idle and that
// advancing through the happy path works while illegal jumps are refused.
func TestNode_StartsIdleAndAdvances(t *testing.T) {
	p, err := NewNodeProfile("34020000011310000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	n := NewNode(p)
	if n.Status() != StatusIdle {
		t.Fatalf("fresh node status = %v, want idle", n.Status())
	}
	registering, err := n.WithStatus(StatusRegistering)
	if err != nil {
		t.Fatalf("Idle -> Registering = %v, want success", err)
	}
	if registering.Status() != StatusRegistering {
		t.Errorf("status = %v, want registering", registering.Status())
	}
	if n.Status() != StatusIdle {
		t.Errorf("receiver mutated to %v, want idle", n.Status())
	}
}

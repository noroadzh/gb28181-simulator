package model

import (
	"testing"
)

func TestFaultProfile_Validate_DefaultProfileIsZero(t *testing.T) {
	var p FaultProfile
	if !p.IsZero() {
		t.Fatalf("zero profile should report IsZero, got configured")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("zero profile should validate cleanly: %v", err)
	}
}

func TestFaultProfile_Validate_InvalidCanned(t *testing.T) {
	cases := []struct {
		name string
		p    FaultProfile
	}{
		{"empty method", FaultProfile{Canned: map[string]int{"": 403}}},
		{"lower-case method", FaultProfile{Canned: map[string]int{"invite": 403}}},
		{"status below 400", FaultProfile{Canned: map[string]int{"INVITE": 200}}},
		{"status above 699", FaultProfile{Canned: map[string]int{"INVITE": 700}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.p.Validate(); err == nil {
				t.Fatal("expected validation error for invalid canned, got nil")
			}
		})
	}
}

func TestFaultProfile_Validate_Valid(t *testing.T) {
	p := FaultProfile{
		Canned:            map[string]int{"INVITE": 403},
		Drop:              0.25,
		UnsupportedMethod: 501,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
}

func TestFaultProfile_Validate_InvalidDrop(t *testing.T) {
	cases := []struct {
		name string
		drop float64
	}{
		{"negative", -0.1},
		{"above 1", 1.1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := FaultProfile{Drop: tc.drop}
			if err := p.Validate(); err == nil {
				t.Fatalf("expected validation error for drop=%g", tc.drop)
			}
		})
	}
}

func TestFaultProfile_Validate_BlackholeLowercase(t *testing.T) {
	p := FaultProfile{Blackhole: []string{"invite"}}
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error for lower-case blackhole method")
	}
}

func TestFaultProfile_Validate_NegativeDelay(t *testing.T) {
	p := FaultProfile{Delay: FaultDelayConfig{Base: -1}}
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error for negative base delay")
	}
}

func TestFaultProfile_Validate_InvalidUnsupportedMethod(t *testing.T) {
	cases := []struct {
		name string
		code int
	}{
		{"below 400", 300},
		{"above 699", 800},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := FaultProfile{UnsupportedMethod: tc.code}
			if err := p.Validate(); err == nil {
				t.Fatalf("expected validation error for unsupportedMethod=%d", tc.code)
			}
		})
	}
}

func TestFaultProfile_BlackholeSet(t *testing.T) {
	p := FaultProfile{Blackhole: []string{"MESSAGE", "KEEPALIVE"}}
	cases := []struct {
		method   string
		expected bool
	}{
		{"MESSAGE", true},
		{"KEEPALIVE", true},
		{"REGISTER", false},
		{"INVITE", false},
		{"message", false},
	}
	for _, tc := range cases {
		got := p.isBlackholed(tc.method)
		if got != tc.expected {
			t.Errorf("isBlackholed(%q) = %v, want %v", tc.method, got, tc.expected)
		}
	}
}

func TestFaultProfile_CannedStatus(t *testing.T) {
	p := FaultProfile{Canned: map[string]int{"INVITE": 403, "REGISTER": 404}}
	status, ok := p.cannedStatus("INVITE")
	if !ok || status != 403 {
		t.Errorf("cannedStatus(INVITE) = %d %v, want 403 true", status, ok)
	}
	status, ok = p.cannedStatus("BYE")
	if ok || status != 0 {
		t.Errorf("cannedStatus(BYE) = %d %v, want 0 false", status, ok)
	}
}

package model

import (
	"testing"
	"time"
)

// The window a platform grants is the only thing standing between a silent
// device and a table that keeps it forever, so the clamping rules are
// pinned down case by case.
func TestNewExpiresPolicy_Defaults(t *testing.T) {
	p, err := NewExpiresPolicy(0, 0, 0)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	if p.Min() != DefaultPlatformMinExpires || p.Default() != DefaultPlatformExpires || p.Max() != DefaultPlatformMaxExpires {
		t.Errorf("window = %d/%d/%d, want %d/%d/%d",
			p.Min(), p.Default(), p.Max(),
			DefaultPlatformMinExpires, DefaultPlatformExpires, DefaultPlatformMaxExpires)
	}
	if !p.HasPolicy() {
		t.Error("HasPolicy() = false, want true")
	}
}

func TestNewExpiresPolicy_Validation(t *testing.T) {
	cases := []struct {
		name    string
		min     uint32
		def     uint32
		max     uint32
		wantErr bool
	}{
		{"sane", 60, 3600, 86400, false},
		{"default below min", 600, 60, 86400, true},
		{"max below default", 60, 3600, 600, true},
		{"equal edges", 60, 60, 60, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewExpiresPolicy(tc.min, tc.def, tc.max)
			if tc.wantErr && err == nil {
				t.Fatalf("NewExpiresPolicy(%d,%d,%d) = nil, want an error", tc.min, tc.def, tc.max)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("NewExpiresPolicy(%d,%d,%d) = %v, want nil", tc.min, tc.def, tc.max, err)
			}
		})
	}
}

func TestExpiresPolicy_Negotiate(t *testing.T) {
	p, err := NewExpiresPolicy(60, 3600, 7200)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	cases := []struct {
		name      string
		requested uint32
		want      uint32
	}{
		{"absent takes the default", 0, 3600},
		{"below the minimum", 1, 60},
		{"at the minimum", 60, 60},
		{"inside the window", 1800, 1800},
		{"above the maximum", 86400, 7200},
		{"at the maximum", 7200, 7200},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Negotiate(tc.requested); got != tc.want {
				t.Errorf("Negotiate(%d) = %d, want %d", tc.requested, got, tc.want)
			}
		})
	}
}

// An explicit `Expires: 0` is an unregistration, so Negotiate must not be
// the place that turns it into a granted lifetime — callers intercept it
// first, and this test documents why the distinction matters.
func TestExpiresPolicy_NegotiateZeroIsUnspecified(t *testing.T) {
	p, err := NewExpiresPolicy(0, 0, 0)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	if got := p.Negotiate(0); got != DefaultPlatformExpires {
		t.Errorf("Negotiate(0) = %d, want the default %d", got, DefaultPlatformExpires)
	}
}

func TestExpiresPolicy_ExpiresAt(t *testing.T) {
	p, err := NewExpiresPolicy(0, 0, 0)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	if got := p.ExpiresAt(600, at); !got.Equal(at.Add(10 * time.Minute)) {
		t.Errorf("ExpiresAt(600) = %v, want %v", got, at.Add(10*time.Minute))
	}
}

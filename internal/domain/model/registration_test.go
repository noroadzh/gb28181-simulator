package model

import (
	"strings"
	"testing"
	"time"
)

const (
	testDeviceID   = "34020000001320000001"
	testPlatformID = "34020000002000000001"
)

func mustNodeID(t *testing.T, raw string) NodeID {
	t.Helper()
	id, err := ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

func TestNewRegistration_Defaults(t *testing.T) {
	t.Parallel()
	reg, err := NewRegistration(RegistrationParams{
		Server:   "127.0.0.1:5060",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	if reg.Expires() != DefaultExpires {
		t.Errorf("expires: got %d want %d", reg.Expires(), DefaultExpires)
	}
	if reg.Timeout() != DefaultTimeout {
		t.Errorf("timeout: got %v want %v", reg.Timeout(), DefaultTimeout)
	}
	if reg.Transport() != DefaultTransport {
		t.Errorf("transport: got %q want %q", reg.Transport(), DefaultTransport)
	}
	if reg.ServerID() != "" || reg.Username() != "" || reg.GBVersion() != "" {
		t.Errorf("optional fields must default to empty: %s", reg)
	}
}

// Everything that would make a registration impossible must be rejected at
// construction time rather than discovered as a silent no-op later.
func TestNewRegistration_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		params  RegistrationParams
		wantErr bool
	}{
		{
			name:   "minimal valid",
			params: RegistrationParams{Server: "127.0.0.1:5060", Password: "secret"},
		},
		{
			name: "fully specified",
			params: RegistrationParams{
				Server: "127.0.0.1:5060", ServerID: testPlatformID,
				Username: "34020000001320000001", Password: "secret",
				GBVersion: "2022", Expires: 600, Timeout: 2 * time.Second,
				Transport: "TCP",
			},
		},
		{
			name:    "empty server",
			params:  RegistrationParams{Password: "secret"},
			wantErr: true,
		},
		{
			name:    "server without port",
			params:  RegistrationParams{Server: "127.0.0.1", Password: "secret"},
			wantErr: true,
		},
		{
			name:    "no password",
			params:  RegistrationParams{Server: "127.0.0.1:5060"},
			wantErr: true,
		},
		{
			name:    "illegal server id",
			params:  RegistrationParams{Server: "127.0.0.1:5060", Password: "s", ServerID: "123"},
			wantErr: true,
		},
		{
			name:    "unknown transport",
			params:  RegistrationParams{Server: "127.0.0.1:5060", Password: "s", Transport: "sctp"},
			wantErr: true,
		},
		{
			name:    "negative timeout",
			params:  RegistrationParams{Server: "127.0.0.1:5060", Password: "s", Timeout: -time.Second},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, err := NewRegistration(tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %s", reg)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewRegistration: %v", err)
			}
			if !reg.HasRegistration() {
				t.Errorf("HasRegistration: false for %s", reg)
			}
		})
	}
}

// Transport is normalised so a config reading "TCP" and one reading "tcp"
// behave identically.
func TestNewRegistration_TransportNormalised(t *testing.T) {
	t.Parallel()
	reg, err := NewRegistration(RegistrationParams{
		Server: "127.0.0.1:5060", Password: "s", Transport: " TCP ",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	if reg.Transport() != "tcp" {
		t.Errorf("transport: got %q want tcp", reg.Transport())
	}
}

func TestRegistration_CredentialsFor(t *testing.T) {
	t.Parallel()
	id := mustNodeID(t, testDeviceID)

	// Without an explicit username the node id is used, which is what
	// GB/T 28181 platforms expect.
	reg, err := NewRegistration(RegistrationParams{Server: "127.0.0.1:5060", Password: "secret"})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	cred, err := reg.CredentialsFor("3402000000", id)
	if err != nil {
		t.Fatalf("CredentialsFor: %v", err)
	}
	if cred.Username() != testDeviceID {
		t.Errorf("username: got %q want the node id %q", cred.Username(), testDeviceID)
	}
	if !cred.PasswordEquals("secret") {
		t.Error("password not carried into the credentials")
	}

	// An explicit username wins.
	named, err := NewRegistration(RegistrationParams{
		Server: "127.0.0.1:5060", Password: "secret", Username: "operator",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	cred2, err := named.CredentialsFor("3402000000", id)
	if err != nil {
		t.Fatalf("CredentialsFor: %v", err)
	}
	if cred2.Username() != "operator" {
		t.Errorf("username: got %q want operator", cred2.Username())
	}

	// An empty realm cannot produce usable credentials.
	if _, err := reg.CredentialsFor("", id); err == nil {
		t.Error("expected an error for an empty realm")
	}
}

// A Registration must never leak the password through logs.
func TestRegistration_StringRedactsPassword(t *testing.T) {
	t.Parallel()
	reg, err := NewRegistration(RegistrationParams{
		Server: "127.0.0.1:5060", Password: "super-secret",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	if strings.Contains(reg.String(), "super-secret") {
		t.Errorf("String() leaks the password: %s", reg.String())
	}
}

func TestRegistrationResult_Validation(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	res, err := NewRegistrationResult("127.0.0.1:5060", 600, at)
	if err != nil {
		t.Fatalf("NewRegistrationResult: %v", err)
	}
	if res.Server() != "127.0.0.1:5060" || res.GrantedExpiry() != 600 || !res.RegisteredAt().Equal(at) {
		t.Errorf("unexpected result: %s", res)
	}
	if !res.HasResult() {
		t.Error("HasResult: false for a constructed result")
	}
	if (RegistrationResult{}).HasResult() {
		t.Error("HasResult: true for the zero value")
	}
	if _, err := NewRegistrationResult("", 600, at); err == nil {
		t.Error("expected an error for an empty server")
	}
	if _, err := NewRegistrationResult("127.0.0.1:5060", 600, time.Time{}); err == nil {
		t.Error("expected an error for a zero time")
	}
	// "Not stated" is legal: 0 means the platform did not say.
	if _, err := NewRegistrationResult("127.0.0.1:5060", 0, at); err != nil {
		t.Errorf("granted expiry 0 must be allowed: %v", err)
	}
}

func TestNodeProfile_Registration(t *testing.T) {
	t.Parallel()
	profile, err := NewNodeProfile(testDeviceID, "127.0.0.1:5061", "3402000000", "")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if _, ok := profile.Registration(); ok {
		t.Error("a fresh profile must not report a registration")
	}
	reg, err := NewRegistration(RegistrationParams{Server: "127.0.0.1:5060", Password: "secret"})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	withReg, err := profile.WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	got, ok := withReg.Registration()
	if !ok || got.Server() != reg.Server() {
		t.Errorf("registration not carried: %v %v", got, ok)
	}
	// The receiver is untouched (immutability).
	if _, ok := profile.Registration(); ok {
		t.Error("WithRegistration mutated the receiver")
	}
	if _, err := profile.WithRegistration(Registration{}); err == nil {
		t.Error("expected an error for an unconstructed Registration")
	}
}

// Changing the listening address must not drop the registration settings.
func TestNodeProfile_WithAddrKeepsRegistration(t *testing.T) {
	t.Parallel()
	profile, err := NewNodeProfile(testDeviceID, "127.0.0.1:5061", "3402000000", "")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	reg, err := NewRegistration(RegistrationParams{Server: "127.0.0.1:5060", Password: "secret"})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	withReg, err := profile.WithRegistration(reg)
	if err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	moved, err := withReg.WithAddr("127.0.0.1:5071")
	if err != nil {
		t.Fatalf("WithAddr: %v", err)
	}
	if _, ok := moved.Registration(); !ok {
		t.Error("WithAddr dropped the registration")
	}
	if _, err := withReg.WithAddr(""); err == nil {
		t.Error("expected an error for an empty address")
	}
}

func TestNode_RegistrationResult(t *testing.T) {
	t.Parallel()
	profile, err := NewNodeProfile(testDeviceID, "127.0.0.1:5061", "3402000000", "")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	node := NewNode(profile)
	if _, ok := node.RegistrationResult(); ok {
		t.Error("a fresh node must not report a registration result")
	}
	res, err := NewRegistrationResult("127.0.0.1:5060", 600, time.Now())
	if err != nil {
		t.Fatalf("NewRegistrationResult: %v", err)
	}
	updated, err := node.WithRegistrationResult(res)
	if err != nil {
		t.Fatalf("WithRegistrationResult: %v", err)
	}
	got, ok := updated.RegistrationResult()
	if !ok || got.GrantedExpiry() != 600 {
		t.Errorf("result not recorded: %v %v", got, ok)
	}
	// Recording a result is data, not a lifecycle step.
	if updated.Status() != node.Status() {
		t.Errorf("status changed: %s -> %s", node.Status(), updated.Status())
	}
	if _, ok := node.RegistrationResult(); ok {
		t.Error("WithRegistrationResult mutated the receiver")
	}
	if _, err := node.WithRegistrationResult(RegistrationResult{}); err == nil {
		t.Error("expected an error for an unconstructed result")
	}
}

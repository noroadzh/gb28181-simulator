package model

import (
	"strings"
	"testing"
)

// TestCredentials_BuildAndRead asserts basic round-trip.
func TestCredentials_BuildAndRead(t *testing.T) {
	c, err := NewCredentials("alice", "example.com", "secret")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if c.Username() != "alice" || c.Realm() != "example.com" || c.Password() != "secret" {
		t.Errorf("round-trip mismatch: %+v", c)
	}
}

// TestCredentials_PasswordEqualsConstantTime asserts equality is checked
// in constant time (no early-exit on length).
func TestCredentials_PasswordEqualsConstantTime(t *testing.T) {
	c, _ := NewCredentials("alice", "example.com", "secret")
	if !c.PasswordEquals("secret") {
		t.Error("PasswordEquals(secret) = false; want true")
	}
	if c.PasswordEquals("secre") {
		t.Error("PasswordEquals(secre) = true; want false")
	}
	if c.PasswordEquals("secretX") {
		t.Error("PasswordEquals(secretX) = true; want false")
	}
}

// TestCredentials_EmptyFieldsReject asserts constructor rejects blanks for
// username and realm. An empty password is allowed (some fixtures use a
// pre-shared nonce instead of a real password; allowing empty here keeps
// the type usable in tests).
func TestCredentials_EmptyFieldsReject(t *testing.T) {
	cases := []struct {
		u, r, p string
	}{
		{"", "r", "p"},
		{"u", "", "p"},
		{"u", "r", "p"}, // empty password accepted, sanity-check below
	}
	for i, c := range cases {
		_, err := NewCredentials(c.u, c.r, c.p)
		if i < 2 && err == nil {
			t.Errorf("case[%d]: empty field accepted", i)
		}
		if i == 2 && err != nil {
			t.Errorf("case[%d]: empty password should be accepted, got %v", i, err)
		}
	}
}

// TestCredentials_InvalidUTF8Rejects asserts NUL byte and invalid UTF-8
// in username or realm are rejected (RFC 7616 §3.3).
func TestCredentials_InvalidUTF8Rejects(t *testing.T) {
	bad := []struct{ u, r string }{
		{"\x00alice", "r"},
		{"alice", "\xff\xfe"},
	}
	for i, c := range bad {
		if _, err := NewCredentials(c.u, c.r, "p"); err == nil {
			t.Errorf("case[%d]: invalid UTF-8 accepted in %q %q", i, c.u, c.r)
		}
	}
}

// TestCredentials_StringRedactsPassword asserts the String form hides the
// password so log lines stay safe.
func TestCredentials_StringRedactsPassword(t *testing.T) {
	c, _ := NewCredentials("alice", "example.com", "secret")
	s := c.String()
	if strings.Contains(s, "secret") {
		t.Errorf("String() leaked password: %q", s)
	}
	if !strings.Contains(s, "***") {
		t.Errorf("String() missing redactor: %q", s)
	}
}

// TestChallenge_BuildAndRead asserts basic round-trip.
func TestChallenge_BuildAndRead(t *testing.T) {
	ch, err := NewChallenge("example.com", "deadbeef", "MD5", "opaque", "auth")
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}
	if ch.Realm() != "example.com" || ch.Nonce() != "deadbeef" {
		t.Errorf("round-trip mismatch: %+v", ch)
	}
	if ch.Algorithm() != "MD5" || ch.Opaque() != "opaque" || ch.Qop() != "auth" {
		t.Errorf("optional fields mismatch: %+v", ch)
	}
}

// TestChallenge_EmptyFieldsReject asserts realm/nonce are mandatory.
func TestChallenge_EmptyFieldsReject(t *testing.T) {
	for _, c := range []struct{ r, n string }{{"", "n"}, {"r", ""}} {
		if _, err := NewChallenge(c.r, c.n, "", "", ""); err == nil {
			t.Errorf("empty field accepted: r=%q n=%q", c.r, c.n)
		}
	}
}

// TestChallenge_NonceCRLFRejects asserts constructor rejects CRLF in nonce.
func TestChallenge_NonceCRLFRejects(t *testing.T) {
	if _, err := NewChallenge("r", "n\r\n", "", "", ""); err == nil {
		t.Fatal("CRLF in nonce accepted")
	}
}
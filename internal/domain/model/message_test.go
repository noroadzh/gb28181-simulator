package model

import (
	"strings"
	"testing"
)

// TestMessage_Request_BasicBuildRoundTrip asserts that a request can be
// built and read back without mutation.
func TestMessage_Request_BasicBuildRoundTrip(t *testing.T) {
	hs := []struct{ n, v string }{
		{"Via", "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-abc"},
		{"From", "<sip:alice@example.com>;tag=1"},
		{"To", "<sip:bob@example.com>"},
	}
	headers := make([]Header, len(hs))
	for i, h := range hs {
		headers[i] = NewHeader(h.n, h.v)
	}
	msg, err := NewRequest("INVITE", "sip:bob@example.com", headers, "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if msg.Method() != "INVITE" {
		t.Errorf("Method() = %q, want INVITE", msg.Method())
	}
	if msg.URI().String() != "sip:bob@example.com" {
		t.Errorf("URI() = %q, want sip:bob@example.com", msg.URI().String())
	}
	if got, ok := msg.Header("via"); !ok || got.Value() != hs[0].v {
		t.Errorf("Header(via) = %v, %v; want %q", got, ok, hs[0].v)
	}
	if !msg.IsRequest() || msg.IsResponse() {
		t.Errorf("IsRequest/IsResponse mismatch: %v %v", msg.IsRequest(), msg.IsResponse())
	}
	if !strings.Contains(msg.Body(), "v=0") {
		t.Errorf("Body() = %q, missing v=0", msg.Body())
	}
}

// TestMessage_NewRequest_LowercaseMethodUppercased asserts the constructor
// normalises the method.
func TestMessage_NewRequest_LowercaseMethodUppercased(t *testing.T) {
	msg, err := NewRequest("register", "sip:x@y", nil, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if msg.Method() != "REGISTER" {
		t.Errorf("Method() = %q, want REGISTER", msg.Method())
	}
}

// TestMessage_NewRequest_RejectsEmptyMethod ensures the constructor
// rejects an empty method (immutability gate: any structural error is fatal
// at build time so downstream code never has to check).
func TestMessage_NewRequest_RejectsEmptyMethod(t *testing.T) {
	if _, err := NewRequest("", "sip:x@y", nil, ""); err == nil {
		t.Fatal("NewRequest with empty method accepted, want error")
	}
}

// TestMessage_NewRequest_RejectsBadURI ensures the constructor rejects a
// URI without a scheme.
func TestMessage_NewRequest_RejectsBadURI(t *testing.T) {
	if _, err := NewRequest("REGISTER", "no-scheme", nil, ""); err == nil {
		t.Fatal("NewRequest with no-scheme URI accepted, want error")
	}
}

// TestMessage_Headers_DefensiveCopy ensures the header slice returned by
// Headers() is a copy; mutating it must not affect the original.
func TestMessage_Headers_DefensiveCopy(t *testing.T) {
	msg, _ := NewRequest("REGISTER", "sip:x@y",
		[]Header{NewHeader("Via", "x")}, "")
	got := msg.Headers()
	got[0] = NewHeader("Y", "tampered")
	again := msg.Headers()
	if again[0].Name() != "Via" {
		t.Errorf("Headers() returned a live slice; got %q", again[0].Name())
	}
}

// TestMessage_NewResponse_OutOfRangeStatusCode asserts constructor rejects
// status codes outside 100..999.
func TestMessage_NewResponse_OutOfRangeStatusCode(t *testing.T) {
	for _, code := range []int{0, 99, 1000} {
		if _, err := NewResponse(code, "x", nil, ""); err == nil {
			t.Errorf("NewResponse status %d accepted, want error", code)
		}
	}
}

// TestNewHeader_RejectsCRLF asserts constructor panics on CR/LF in value.
func TestNewHeader_RejectsCRLF(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewHeader accepted CR/LF in value; want panic")
		}
	}()
	_ = NewHeader("Via", "x\r\ny")
}

// TestNewHeader_RejectsEmptyName asserts constructor panics on empty name.
func TestNewHeader_RejectsEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewHeader accepted empty name; want panic")
		}
	}()
	_ = NewHeader("", "value")
}

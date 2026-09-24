package sip_test

import (
	"testing"

	"github.com/ghettovoice/gosip/sip"

	isip "github.com/your-org/gb28181-simulator/internal/adapter/sip"
)

// firstHeader renders the only occurrence of name, or "" when absent.
func firstHeader(msg sip.Message, name string) string {
	hdrs := msg.GetHeaders(name)
	if len(hdrs) == 0 {
		return ""
	}
	return hdrs[0].String()
}

// TestBuildRequest_NewOptionsAbsent is the regression guard for the new
// options: a call that does not use WithContact / WithExpires /
// WithTransport must produce exactly what it produced before — Via over
// UDP, and neither Contact nor Expires present.
func TestBuildRequest_NewOptionsAbsent(t *testing.T) {
	t.Parallel()
	req, err := isip.BuildRequest(
		sip.RequestMethod("REGISTER"),
		"sip:34020000002000000001@127.0.0.1:5060",
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got, want := firstHeader(req, "Via"),
		"Via: SIP/2.0/UDP 127.0.0.1;branch=z9hG4bK-testbranch"; got != want {
		t.Errorf("Via changed: got %q want %q", got, want)
	}
	if got := firstHeader(req, "Contact"); got != "" {
		t.Errorf("unexpected Contact header: %q", got)
	}
	if got := firstHeader(req, "Expires"); got != "" {
		t.Errorf("unexpected Expires header: %q", got)
	}
}

func TestBuildRequest_WithContact(t *testing.T) {
	t.Parallel()
	req, err := isip.BuildRequest(
		sip.RequestMethod("REGISTER"),
		"sip:34020000002000000001@127.0.0.1:5060",
		isip.WithContact("sip:34020000001320000001@192.168.1.10:5060"),
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	hdrs := req.GetHeaders("Contact")
	if len(hdrs) != 1 {
		t.Fatalf("expected exactly 1 Contact header, got %d", len(hdrs))
	}
	if got, want := hdrs[0].String(),
		"Contact: <sip:34020000001320000001@192.168.1.10:5060>"; got != want {
		t.Errorf("Contact: got %q want %q", got, want)
	}
}

// A malformed Contact must surface as an error, not a silently dropped header.
func TestBuildRequest_WithContactInvalid(t *testing.T) {
	t.Parallel()
	if _, err := isip.BuildRequest(
		sip.RequestMethod("REGISTER"),
		"sip:34020000002000000001@127.0.0.1:5060",
		isip.WithContact("not a uri"),
	); err == nil {
		t.Fatal("expected an error for a malformed Contact address")
	}
}

func TestBuildRequest_WithExpires(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   uint32
		want string
	}{
		{"lifetime", 3600, "Expires: 3600"},
		// 0 is legal: RFC 3261 §20.19 uses it to request de-registration.
		{"zero", 0, "Expires: 0"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := isip.BuildRequest(
				sip.RequestMethod("REGISTER"),
				"sip:34020000002000000001@127.0.0.1:5060",
				isip.WithExpires(tc.in),
			)
			if err != nil {
				t.Fatalf("BuildRequest: %v", err)
			}
			if got := firstHeader(req, "Expires"); got != tc.want {
				t.Errorf("Expires: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestBuildRequest_WithTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		proto string
		want  string
	}{
		{"explicit TCP", "TCP", "Via: SIP/2.0/TCP 127.0.0.1;branch=z9hG4bK-testbranch"},
		{"explicit UDP", "UDP", "Via: SIP/2.0/UDP 127.0.0.1;branch=z9hG4bK-testbranch"},
		{"empty falls back to UDP", "", "Via: SIP/2.0/UDP 127.0.0.1;branch=z9hG4bK-testbranch"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := isip.BuildRequest(
				sip.RequestMethod("REGISTER"),
				"sip:34020000002000000001@127.0.0.1:5060",
				isip.WithTransport(tc.proto),
			)
			if err != nil {
				t.Fatalf("BuildRequest: %v", err)
			}
			if got := firstHeader(req, "Via"); got != tc.want {
				t.Errorf("Via: got %q want %q", got, tc.want)
			}
		})
	}
}

// A REGISTER and its 200 OK both carry Contact / Expires, so the response
// builder must honour the same options.
func TestBuildResponse_WithContactAndExpires(t *testing.T) {
	t.Parallel()
	resp, err := isip.BuildResponse(
		sip.StatusCode(200),
		isip.WithContact("sip:34020000001320000001@192.168.1.10:5060"),
		isip.WithExpires(600),
	)
	if err != nil {
		t.Fatalf("BuildResponse: %v", err)
	}
	if got, want := firstHeader(resp, "Contact"),
		"Contact: <sip:34020000001320000001@192.168.1.10:5060>"; got != want {
		t.Errorf("Contact: got %q want %q", got, want)
	}
	if got, want := firstHeader(resp, "Expires"), "Expires: 600"; got != want {
		t.Errorf("Expires: got %q want %q", got, want)
	}
}

package siptransport

import (
	"strings"
	"testing"

	"github.com/ghettovoice/gosip/sip"

	sipbuild "github.com/your-org/gb28181-simulator/internal/adapter/sip"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// registerModel is a REGISTER carrying every header a device identity puts
// on it. Before Change 5's fix, the conversion dropped all of these.
func registerModel(t *testing.T) model.Message {
	t.Helper()
	m, err := model.NewRequest("REGISTER", "sip:34020000002000000001@3402000000",
		[]model.Header{
			model.NewHeader("From", "<sip:34020000001320000001@3402000000>;tag=fromtag"),
			model.NewHeader("To", "<sip:34020000001320000001@3402000000>"),
			model.NewHeader("Call-ID", "register-callid-1"),
			model.NewHeader("CSeq", "1 REGISTER"),
			model.NewHeader("Contact", "<sip:34020000001320000001@127.0.0.1:5060>"),
			model.NewHeader("Expires", "3600"),
			model.NewHeader("Authorization",
				`Digest username="34020000001320000001", realm="3402000000", nonce="abc", response="def"`),
			model.NewHeader("X-GB-Ver", "2022"),
			model.NewHeader("Max-Forwards", "70"),
		}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return m
}

func headerValue(t *testing.T, msg sip.Message, name string) string {
	t.Helper()
	hdrs := msg.GetHeaders(name)
	if len(hdrs) != 1 {
		t.Fatalf("expected exactly 1 %q header, got %d", name, len(hdrs))
	}
	return hdrs[0].String()
}

// TestModelToGosip_PreservesCallerHeaders is the regression guard for the
// header-dropping defect: caller-supplied headers must reach the wire, and
// headers the builder generates must not be duplicated.
func TestModelToGosip_PreservesCallerHeaders(t *testing.T) {
	t.Parallel()
	msg, err := modelToGosip(registerModel(t), sipbuild.WithTransport("UDP"))
	if err != nil {
		t.Fatalf("modelToGosip: %v", err)
	}
	want := map[string]string{
		"Contact":      "Contact: <sip:34020000001320000001@127.0.0.1:5060>",
		"Expires":      "Expires: 3600",
		"Call-ID":      "Call-ID: register-callid-1",
		"CSeq":         "CSeq: 1 REGISTER",
		"Max-Forwards": "Max-Forwards: 70",
		"X-GB-Ver":     "X-GB-Ver: 2022",
	}
	for name, expected := range want {
		if got := headerValue(t, msg, name); got != expected {
			t.Errorf("%s: got %q want %q", name, got, expected)
		}
	}
	auth := headerValue(t, msg, "Authorization")
	if !strings.HasPrefix(auth, "Authorization: Digest username=") {
		t.Errorf("Authorization lost or malformed: %q", auth)
	}
	// From / To are supplied by the caller, so the builder must not add a
	// second, generated one.
	if got := headerValue(t, msg, "From"); !strings.Contains(got, "34020000001320000001") {
		t.Errorf("From: got %q", got)
	}
	if got := headerValue(t, msg, "To"); !strings.Contains(got, "34020000001320000001") {
		t.Errorf("To: got %q", got)
	}
	if got := headerValue(t, msg, "Via"); !strings.Contains(got, "SIP/2.0/UDP") {
		t.Errorf("Via: got %q, want UDP transport", got)
	}
}

// A node listening on TCP must tell its peer to answer over TCP.
func TestModelToGosip_ViaTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		proto string
		want  string
	}{
		{"UDP", "SIP/2.0/UDP"},
		{"TCP", "SIP/2.0/TCP"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.proto, func(t *testing.T) {
			t.Parallel()
			msg, err := modelToGosip(registerModel(t), sipbuild.WithTransport(tc.proto))
			if err != nil {
				t.Fatalf("modelToGosip: %v", err)
			}
			if got := headerValue(t, msg, "Via"); !strings.Contains(got, tc.want) {
				t.Errorf("Via: got %q want %q", got, tc.want)
			}
		})
	}
}

// A response echoes the request's transaction identifiers, so they must
// survive the conversion too — and its Via is the caller's, not a generated
// one.
func TestModelToGosip_ResponsePreservesHeaders(t *testing.T) {
	t.Parallel()
	m, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-orig"),
		model.NewHeader("From", "<sip:34020000001320000001@3402000000>;tag=fromtag"),
		model.NewHeader("To", "<sip:34020000001320000001@3402000000>;tag=totag"),
		model.NewHeader("Call-ID", "register-callid-1"),
		model.NewHeader("CSeq", "1 REGISTER"),
		model.NewHeader("Contact", "<sip:34020000001320000001@127.0.0.1:5060>"),
		model.NewHeader("Expires", "600"),
	}, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	msg, err := modelToGosip(m)
	if err != nil {
		t.Fatalf("modelToGosip: %v", err)
	}
	want := map[string]string{
		"Via":     "Via: SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-orig",
		"Call-ID": "Call-ID: register-callid-1",
		"CSeq":    "CSeq: 1 REGISTER",
		"Contact": "Contact: <sip:34020000001320000001@127.0.0.1:5060>",
		"Expires": "Expires: 600",
	}
	for name, expected := range want {
		if got := headerValue(t, msg, name); got != expected {
			t.Errorf("%s: got %q want %q", name, got, expected)
		}
	}
}

// A malformed numeric header must surface as an error rather than a message
// that silently loses its Expires value.
func TestModelToGosip_InvalidExpires(t *testing.T) {
	t.Parallel()
	m, err := model.NewRequest("REGISTER", "sip:34020000002000000001@3402000000",
		[]model.Header{model.NewHeader("Expires", "soon")}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := modelToGosip(m); err == nil {
		t.Fatal("expected an error for a non-numeric Expires header")
	}
}

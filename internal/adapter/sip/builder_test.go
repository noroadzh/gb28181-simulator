package sip_test

import (
	"strings"
	"testing"

	"github.com/ghettovoice/gosip/sip"

	isip "github.com/your-org/gb28181-simulator/internal/adapter/sip"
)

// TestMain installs a deterministic branch generator and the package's
// declared Version so tests don't race on globals and produce stable output.
func TestMain(m *testing.M) {
	isip.SetDefaultBranchGenerator(isip.NewBranchGeneratorWith(func() string {
		return "z9hG4bK-testbranch"
	}))
	isip.SetVersion("test-1.0")
	m.Run()
}

func TestBuildRequest_MandatoryHeaders(t *testing.T) {
	t.Parallel()
	req, err := isip.BuildRequest(
		sip.RequestMethod("INVITE"),
		"sip:34020000001320000001@127.0.0.1:5060",
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	startLine := req.String()
	if !strings.HasPrefix(startLine, "INVITE sip:34020000001320000001@127.0.0.1:5060 SIP/2.0") {
		t.Fatalf("unexpected start line: %q", startLine)
	}
	// Mandatory headers per RFC 3261 / GB/T 28181 §L.1
	for _, name := range []string{"Via", "CSeq", "From", "To", "Call-ID", "Max-Forwards", "User-Agent"} {
		if req.GetHeaders(name) == nil {
			t.Errorf("BuildRequest produced request missing required %q header", name)
		}
	}
}

func TestBuildRequest_XGBVer(t *testing.T) {
	t.Parallel()
	req, err := isip.BuildRequest(
		sip.RequestMethod("REGISTER"),
		"sip:34020000002000000001@127.0.0.1:5060",
		isip.WithXGBVer("2022"),
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	hdrs := req.GetHeaders("X-GB-Ver")
	if len(hdrs) != 1 {
		t.Fatalf("expected 1 X-GB-Ver header, got %d", len(hdrs))
	}
	if hdrs[0].String() != "X-GB-Ver: 2022" {
		t.Fatalf("unexpected X-GB-Ver: %q", hdrs[0].String())
	}
}

func TestBuildRequest_ContentTypeAndBody(t *testing.T) {
	t.Parallel()
	req, err := isip.BuildRequest(
		sip.RequestMethod("MESSAGE"),
		"sip:34020000001320000001@127.0.0.1:5060",
		isip.WithBody("v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n"),
		isip.WithContentType("application/sdp"),
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if !strings.Contains(req.String(), "Content-Type: application/sdp") {
		t.Fatalf("missing Content-Type in output: %q", req.String())
	}
	if !strings.Contains(req.String(), "v=0\r\n") {
		t.Fatalf("missing body in output")
	}
	if !strings.Contains(req.String(), "Content-Length: 31") {
		t.Fatalf("missing Content-Length in output: %q", req.String())
	}
}

func TestBuildResponse_DefaultReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code   sip.StatusCode
		reason string
	}{
		{200, "OK"},
		{401, "Unauthorized"},
		{404, "Not Found"},
		{407, "Proxy Authentication Required"},
		{486, "Busy Here"},
		{500, "Server Internal Error"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.reason, func(t *testing.T) {
			resp, err := isip.BuildResponse(tc.code)
			if err != nil {
				t.Fatalf("BuildResponse: %v", err)
			}
			if !strings.Contains(resp.String(), tc.reason) {
				t.Fatalf("expected reason %q in response: %s", tc.reason, resp.String())
			}
		})
	}
}

func TestCSeqNo(t *testing.T) {
	t.Parallel()
	req, err := isip.BuildRequest(
		sip.RequestMethod("INVITE"),
		"sip:34020000001320000001@127.0.0.1:5060",
		isip.WithCSeq(7),
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got := isip.CSeqNo(req); got != 7 {
		t.Fatalf("expected CSeq=7, got %d", got)
	}
}

func TestStartLineAndContentLength(t *testing.T) {
	t.Parallel()
	req, err := isip.BuildRequest(
		sip.RequestMethod("INVITE"),
		"sip:34020000001320000001@127.0.0.1:5060",
		isip.WithBody("hello"),
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got := isip.StartLine(req); !strings.HasPrefix(got, "INVITE ") {
		t.Fatalf("expected StartLine to begin with INVITE, got %q", got)
	}
	if got := isip.ContentLengthString(req); got != "5" {
		t.Fatalf("expected Content-Length 5, got %q", got)
	}
}
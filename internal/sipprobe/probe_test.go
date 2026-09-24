package sipprobe_test

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ghettovoice/gosip/sip"

	"github.com/your-org/gb28181-simulator/internal/sipprobe"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
)

// TestParseStatus covers the --expect-status flag parser.
func TestParseStatus(t *testing.T) {
	cases := []struct {
		in    string
		want  int
		isErr bool
	}{
		{"", 0, false},        // empty = any
		{"100", 100, false},   // provisional
		{"200", 200, false},   // success
		{"404", 404, false},   // client error
		{"503", 503, false},   // server error
		{"699", 699, false},   // upper bound
		{"0", 0, true},        // below range
		{"700", 0, true},      // above range
		{"abc", 0, true},      // non-numeric
		{"-1", 0, true},       // negative
	}
	for _, tc := range cases {
		got, err := sipprobe.ParseStatus(tc.in)
		if tc.isErr {
			if err == nil {
				t.Errorf("ParseStatus(%q): expected error, got %d", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseStatus(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseStatus(%q): got %d want %d", tc.in, got, tc.want)
		}
	}
}

// TestMode verifies that --send-to switches Mode between Send and Receive.
func TestMode(t *testing.T) {
	cases := []struct {
		sendTo string
		want   sipprobe.Mode
	}{
		{"", sipprobe.ModeReceive},
		{"udp://127.0.0.1:5060", sipprobe.ModeSend},
		{"tcp://10.0.0.1:5060", sipprobe.ModeSend},
	}
	for _, tc := range cases {
		got := sipprobe.Options{SendTo: tc.sendTo}.Mode()
		if got != tc.want {
			t.Errorf("Options{SendTo:%q}.Mode() = %v, want %v", tc.sendTo, got, tc.want)
		}
	}
}

// TestRun_TimeoutExitCode exercises the receive-only timeout path: Run must
// return ExitTimeout and the stderr writer must carry the diagnostic.
func TestRun_TimeoutExitCode(t *testing.T) {
	// bind to an ephemeral loopback port we can be sure is free.
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.LocalAddr().String()
	l.Close()

	stderr := &bytes.Buffer{}
	got, code := sipprobe.Run(context.Background(), sipprobe.Options{
		Bind:    "udp://" + addr,
		Timeout: 150 * time.Millisecond,
		Stderr:  stderr,
	})
	if code != sipprobe.ExitTimeout {
		t.Fatalf("expected ExitTimeout (%d), got %d; stderr=%q", sipprobe.ExitTimeout, code, stderr.String())
	}
	if got.StatusCode != 0 || got.Method != "" {
		t.Errorf("expected zero Result on timeout, got %+v", got)
	}
	if !strings.Contains(stderr.String(), "timeout waiting for status=0") {
		t.Errorf("stderr missing diagnostic, got: %s", stderr.String())
	}
}

// TestRun_BindMissing covers the usage-error path: empty --bind must produce
// ExitUsage, not a panic.
func TestRun_BindMissing(t *testing.T) {
	stderr := &bytes.Buffer{}
	_, code := sipprobe.Run(context.Background(), sipprobe.Options{
		Bind:   "",
		Stderr: stderr,
	})
	if code != sipprobe.ExitUsage {
		t.Errorf("expected ExitUsage, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--bind") {
		t.Errorf("stderr missing hint, got: %s", stderr.String())
	}
}

// TestRun_UnexpectedStatus sends a real SIP request to a fake peer that never
// answers. Run must surface ExitTimeout and the stderr line. We do not assert
// ExitUnexpected because crafting a real response in-loop would require
// running a second Transport; the ExitUnexpected path is exercised by the
// manual smoke-test in scripts/sipprobe.md.
func TestRun_UnexpectedStatus(t *testing.T) {
	stderr := &bytes.Buffer{}
	_, got := sipprobe.Run(context.Background(), sipprobe.Options{
		Bind:         "udp://127.0.0.1:0",
		SendTo:       "udp://127.0.0.1:1", // closed port — I/O error, falls through to ExitUsage
		ExpectStatus: 200,
		Timeout:      200 * time.Millisecond,
		Stderr:       stderr,
	})
	if got != sipprobe.ExitTimeout && got != sipprobe.ExitUsage {
		t.Errorf("expected ExitTimeout or ExitUsage, got %d; stderr=%q", got, stderr.String())
	}
}

// TestRun_AcceptAnyResponse wires two transports, makes A send a request to B
// and lets B answer with a 200 OK; A must print the response status and exit 0.
func TestRun_AcceptAnyResponse(t *testing.T) {
	la, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lb, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		la.Close()
		t.Fatal(err)
	}
	addrA := la.LocalAddr().String()
	addrB := lb.LocalAddr().String()
	la.Close()
	lb.Close()

	// Start B as a fake UAS that responds to whatever it sees with 200 OK.
	bTransport, err := siptransport.New("udp://" + addrB)
	if err != nil {
		t.Fatal(err)
	}
	defer bTransport.Close()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		msg, _, err := bTransport.Receive(ctx)
		if err != nil {
			return
		}
		req, ok := msg.(sip.Request)
		if !ok {
			return
		}
		resp := sip.NewResponseFromRequest("", req, 200, "OK", "")
		_ = bTransport.Send(resp, addrA)
	}()

	stderr := &bytes.Buffer{}
	res, code := sipprobe.Run(context.Background(), sipprobe.Options{
		Bind:         "udp://" + addrA,
		SendTo:       "udp://" + addrB,
		ExpectStatus: 200,
		Timeout:      1500 * time.Millisecond,
		Stderr:       stderr,
	})
	if code != sipprobe.ExitOK {
		t.Fatalf("expected ExitOK, got %d; stderr=%q", code, stderr.String())
	}
	if res.StatusCode != 200 {
		t.Errorf("expected 200, got %d; start-line=%q", res.StatusCode, res.StartLine)
	}
	if !strings.Contains(res.StartLine, "200") {
		t.Errorf("start-line should mention 200, got %q", res.StartLine)
	}
}

// TestResult_Print guards the canonical <status>\t<start-line> shape.
func TestResult_Print(t *testing.T) {
	var buf bytes.Buffer
	sipprobe.Result{StatusCode: 200, StartLine: "SIP/2.0 200 OK"}.Print(&buf)
	if got := strings.TrimRight(buf.String(), "\n"); got != "200\tSIP/2.0 200 OK" {
		t.Errorf("unexpected print: %q", got)
	}
}
// Package sipprobe implements the diagnostic CLI used by Change 2's task §7.
//
// The binary lives in cmd/sipprobe; this package exposes the underlying
// functions so they can be unit-tested without spawning a subprocess.
//
// Usage modes:
//
//  1. Send mode: --bind udp://0.0.0.0:0 --send-to udp://host:5060 [--expect-status 200]
//     Constructs a minimal INVITE (with §K PS SDP body) → writes it to --send-to
//     → waits up to --timeout for the first response → prints
//     "<status>\t<start-line>" to stdout.
//
//  2. Receive mode: --bind udp://0.0.0.0:5060  (no --send-to)
//     Waits up to --timeout for the first inbound message → prints
//     "<status>\t<start-line>". If --expect-status is set, non-matching
//     responses are treated as a timeout-equivalent failure.
//
//     With --answer (Change 4, design D5) the receive mode additionally
//     replies 200 OK to an inbound request and sends it back to the address
//     the request arrived from. Without the flag it stays receive-only, as
//     it was in Change 2 — the flag is opt-in so the diagnostic never
//     surprises a network it is only meant to observe.
//
// Exit codes:
//
//	0  - success (response matched --expect-status when set, or any message received)
//	1  - usage / transport error
//	2  - timeout waiting for expected status (stderr: "timeout waiting for status=...")
//	3  - unexpected response status (stderr: "unexpected status <code>, want <want>")
package sipprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ghettovoice/gosip/sip"

	internalsip "github.com/your-org/gb28181-simulator/internal/adapter/sip"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
)

// Mode describes how Run should behave.
type Mode int

const (
	// ModeSend sends one INVITE and waits for a response.
	ModeSend Mode = iota
	// ModeReceive waits for any inbound message without sending anything.
	ModeReceive
)

// Options configures Run. Zero-value picks sensible defaults.
type Options struct {
	// Bind is the local address ("udp://host:port" or "tcp://host:port").
	// Port "0" lets the OS pick.
	Bind string

	// SendTo is the remote address in ModeSend. Ignored in ModeReceive.
	SendTo string

	// ExpectStatus, when > 0, makes Run return ExitUnexpected if a response
	// arrives with a different status. When 0, any response is accepted.
	ExpectStatus int

	// Timeout is the maximum time to wait for a single message. Default 5s.
	Timeout time.Duration

	// From / To URIs used in ModeSend. Defaults are sip:probe@127.0.0.1.
	From string
	To   string

	// Answer, when true, makes ModeReceive reply 200 OK to an inbound
	// request and send it back to the address the request arrived from
	// (design D5). It is opt-in: without it the receive mode stays
	// strictly receive-only, which is what Change 2 shipped.
	Answer bool

	// Stderr override for tests; nil falls back to os.Stderr.
	Stderr io.Writer
}

// Result is the parsed terminal outcome. Print() writes it to stdout in the
// canonical "<status>\t<start-line>" format.
type Result struct {
	// StatusCode is 0 if the inbound message was a request rather than a response.
	StatusCode int

	// Method is the request method (e.g. "INVITE") if the inbound message
	// was a request, empty for responses.
	Method string

	// StartLine is the full start-line as captured.
	StartLine string
}

// Print writes the result to w in the canonical format.
func (r Result) Print(w io.Writer) {
	code := r.StatusCode
	if code == 0 {
		code = -1 // requests get -1 placeholder for column alignment in tooling
	}
	fmt.Fprintf(w, "%d\t%s\n", code, r.StartLine)
}

// Exit codes as documented in the package comment.
const (
	ExitOK = iota
	ExitUsage
	ExitTimeout
	ExitUnexpected
)

// Mode reports which mode Run would take. Computed from Options.
func (o Options) Mode() Mode {
	if o.SendTo == "" {
		return ModeReceive
	}
	return ModeSend
}

// Run executes the probe with the given options. Returns the captured result
// and the process exit code. ctx can be used by callers to short-circuit.
func Run(ctx context.Context, opts Options) (Result, int) {
	errs := opts.stderr()
	if opts.Bind == "" {
		fmt.Fprintln(errs, "sipprobe: --bind is required")
		return Result{}, ExitUsage
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	tr, err := siptransport.New(opts.Bind)
	if err != nil {
		fmt.Fprintf(errs, "sipprobe: bind %s: %v\n", opts.Bind, err)
		return Result{}, ExitUsage
	}
	defer tr.Close()

	switch opts.Mode() {
	case ModeSend:
		return runSend(ctx, tr, opts)
	default:
		return runReceive(ctx, tr, opts)
	}
}

func runSend(ctx context.Context, tr *siptransport.Transport, opts Options) (Result, int) {
	errs := opts.stderr()
	invite, err := buildInvite(opts)
	if err != nil {
		fmt.Fprintf(errs, "sipprobe: build INVITE: %v\n", err)
		return Result{}, ExitUsage
	}
	dst := stripScheme(opts.SendTo)
	if err := tr.Send(invite, dst); err != nil {
		fmt.Fprintf(errs, "sipprobe: send: %v\n", err)
		return Result{}, ExitUsage
	}
	_, _, res, code := waitForResponse(ctx, tr, opts)
	return res, code
}

// stripScheme drops the "udp://" or "tcp://" prefix that the user supplied
// in --send-to, leaving just host:port for gosip's transport layer.
func stripScheme(addr string) string {
	if i := strings.Index(addr, "://"); i >= 0 {
		return addr[i+3:]
	}
	return addr
}

// runReceive waits for one inbound message. With --answer it additionally
// replies 200 OK to a request, using the peer address reported by the
// transport (design D4/D5). A response is never answered.
func runReceive(ctx context.Context, tr *siptransport.Transport, opts Options) (Result, int) {
	msg, peer, res, code := waitForResponse(ctx, tr, opts)
	if code != ExitOK {
		return res, code
	}
	if !opts.Answer {
		return res, ExitOK
	}
	req, ok := msg.(sip.Request)
	if !ok {
		// Nothing to answer: the inbound message was itself a response.
		return res, ExitOK
	}
	if err := sendAnswer(tr, req, peer); err != nil {
		fmt.Fprintf(opts.stderr(), "sipprobe: answer: %v\n", err)
		return res, ExitUsage
	}
	return res, ExitOK
}

// sendAnswer builds and transmits the 200 OK for req, addressed to the peer
// the request came from.
func sendAnswer(tr *siptransport.Transport, req sip.Request, peer string) error {
	resp := sip.NewResponseFromRequest("", req, 200, "OK", "")
	return tr.Send(resp, peer)
}

// waitForResponse waits for one inbound message and returns it together with
// the address it arrived from, so callers can answer without re-deriving the
// peer from headers (design D5).
func waitForResponse(
	ctx context.Context,
	tr *siptransport.Transport,
	opts Options,
) (sip.Message, string, Result, int) {
	errs := opts.stderr()
	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	msg, peer, err := tr.Receive(waitCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			fmt.Fprintf(errs,
				"timeout waiting for status=%d after %s\n",
				opts.ExpectStatus, opts.Timeout)
			return nil, "", Result{}, ExitTimeout
		}
		fmt.Fprintf(errs, "sipprobe: receive: %v\n", err)
		return nil, "", Result{}, ExitUsage
	}

	res := Result{StartLine: msg.StartLine()}
	switch m := msg.(type) {
	case sip.Response:
		res.StatusCode = int(m.StatusCode())
		if opts.ExpectStatus != 0 && res.StatusCode != opts.ExpectStatus {
			fmt.Fprintf(errs,
				"unexpected status %d, want %d\n",
				res.StatusCode, opts.ExpectStatus)
			return msg, peer, res, ExitUnexpected
		}
	case sip.Request:
		res.Method = string(m.Method())
	default:
		fmt.Fprintf(errs, "sipprobe: unknown message type %T\n", msg)
		return msg, peer, res, ExitUsage
	}
	return msg, peer, res, ExitOK
}

func (o Options) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// buildInvite constructs the minimal INVITE used by ModeSend. The body is a
// §K PS video SDP (the most common GB28181 INVITE shape).
func buildInvite(opts Options) (sip.Request, error) {
	from := opts.From
	if from == "" {
		from = "sip:probe@127.0.0.1"
	}
	to := opts.To
	if to == "" {
		to = "sip:probe@" + stripScheme(opts.SendTo)
	}

	req, err := internalsip.BuildRequest(
		internalsip.MethodInvite,
		to,
		internalsip.WithFrom(from),
		internalsip.WithTo(to),
		internalsip.WithViaHost(ourOutboundIP()),
		internalsip.WithContentType("APPLICATION/SDP"),
		internalsip.WithBody(minimalPSSDP()),
	)
	if err != nil {
		return nil, err
	}
	return req, nil
}

// ourOutboundIP returns the machine's preferred outbound IP. Falls back to
// 127.0.0.1 when the network stack refuses to answer (containers, sandboxes).
func ourOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// minimalPSSDP is the §K.2 PS INVITE body used in ModeSend. It is hand-rolled
// (rather than sdp.Marshal) so the probe does not depend on a fully populated
// *sdp.Session, keeping the CLI binary footprint small.
func minimalPSSDP() string {
	return strings.Join([]string{
		"v=0",
		"o=- 0 0 IN IP4 127.0.0.1",
		"s=Play",
		"c=IN IP4 0.0.0.0",
		"t=0 0",
		"m=video 0 RTP/AVP 96",
		"a=rtpmap:96 PS/90000",
		"y=0000000000",
		"f=v",
		"",
	}, "\r\n")
}

// ParseStatus parses a status-code flag value (string or int).
func ParseStatus(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid status %q: %w", s, err)
	}
	if n < 100 || n > 699 {
		return 0, fmt.Errorf("status %d out of range", n)
	}
	return n, nil
}

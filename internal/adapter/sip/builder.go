// Package sip provides SIP message build/parse helpers for GB/T 28181-2016.
package sip

import (
	"fmt"

	"github.com/ghettovoice/gosip/sip"
	"github.com/ghettovoice/gosip/sip/parser"
)

// buildConfig accumulates options for BuildRequest / BuildResponse.
type buildConfig struct {
	headers     []sip.Header
	body        string
	contentType string
	viaHost     string
	from        string
	to          string
	callID      string
	seqNo       uint32
	maxForwards uint32
	userAgent   string
	xGBVer      string
}

// BuildOption customises a single BuildRequest / BuildResponse call.
type BuildOption func(*buildConfig)

// WithHeader appends a header to the message. Use it to inject X-GB-Ver
// (Change 11) or any other custom header.
func WithHeader(h sip.Header) BuildOption {
	return func(c *buildConfig) { c.headers = append(c.headers, h) }
}

// WithBody sets the message body. Content-Length is auto-appended when
// the request is built (via gosip's SetBody(true) path).
func WithBody(body string) BuildOption {
	return func(c *buildConfig) { c.body = body }
}

// WithContentType sets the Content-Type header. Should always be paired
// with WithBody.
func WithContentType(ct string) BuildOption {
	return func(c *buildConfig) { c.contentType = ct }
}

// WithViaHost overrides the 127.0.0.1 host used in the generated Via
// header. Production code should set this to the machine's outbound IP.
func WithViaHost(host string) BuildOption {
	return func(c *buildConfig) { c.viaHost = host }
}

// WithFrom sets the From header value (e.g. "sip:alice@example.com").
func WithFrom(addr string) BuildOption {
	return func(c *buildConfig) { c.from = addr }
}

// WithTo sets the To header value.
func WithTo(addr string) BuildOption {
	return func(c *buildConfig) { c.to = addr }
}

// WithCallID sets the Call-ID header. If unset, gosip generates one.
func WithCallID(id string) BuildOption {
	return func(c *buildConfig) { c.callID = id }
}

// WithCSeq sets the CSeq number. Defaults to 1.
func WithCSeq(n uint32) BuildOption {
	return func(c *buildConfig) { c.seqNo = n }
}

// WithMaxForwards sets the Max-Forwards value. Defaults to 70.
func WithMaxForwards(n uint32) BuildOption {
	return func(c *buildConfig) { c.maxForwards = n }
}

// WithUserAgent overrides the default User-Agent ("gb28181-simulator/<Version>").
func WithUserAgent(ua string) BuildOption {
	return func(c *buildConfig) { c.userAgent = ua }
}

// WithXGBVer adds the X-GB-Ver header used by Change 11's 2022 extension.
func WithXGBVer(v string) BuildOption {
	return func(c *buildConfig) { c.xGBVer = v }
}

// DefaultBranchGenerator is shared by all BuildRequest calls. Replace it
// via SetDefaultBranchGenerator in tests to obtain deterministic Via
// branches.
var DefaultBranchGenerator = NewBranchGenerator()

// SetDefaultBranchGenerator swaps the global BranchGenerator. Intended
// for tests only.
func SetDefaultBranchGenerator(g *BranchGenerator) {
	if g != nil {
		DefaultBranchGenerator = g
	}
}

// Version is the User-Agent suffix (declared in sip.go, re-exported here
// for documentation). The canonical definition lives in sip.go and is
// overridable via SetVersion in tests.
var _versionDoc = Version // re-export reference (unused at runtime)

// --- Request --------------------------------------------------------------

// BuildRequest constructs a SIP request message. The method and target
// URI are mandatory; all other parameters are supplied via opts.
//
// Mandatory GB/T 28181 §L.1 headers auto-filled when not provided:
//   - Via (with unique branch param)
//   - Max-Forwards (default 70)
//   - User-Agent (default "gb28181-simulator/<Version>")
//   - Content-Length (when body is set)
func BuildRequest(method sip.RequestMethod, target string, opts ...BuildOption) (sip.Request, error) {
	if method == "" {
		return nil, fmt.Errorf("sip: empty method")
	}
	if target == "" {
		return nil, fmt.Errorf("sip: empty target URI")
	}
	cfg := buildConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	recipient, err := parser.ParseUri(target)
	if err != nil {
		return nil, fmt.Errorf("sip: target URI %q: %w", target, err)
	}

	hdrs := make([]sip.Header, 0, 10)

	// CSeq — required by RFC 3261 §20.10.
	seqNo := cfg.seqNo
	if seqNo == 0 {
		seqNo = 1
	}
	hdrs = append(hdrs, &sip.CSeq{SeqNo: seqNo, MethodName: method})

	// From — mandatory. Use a fresh address with default tag.
	if cfg.from != "" {
		addr, err := parseAddressValue(cfg.from)
		if err != nil {
			return nil, fmt.Errorf("sip: From %q: %w", cfg.from, err)
		}
		hdrs = append(hdrs, &sip.FromHeader{Address: addr})
	} else {
		defaultFrom := &sip.FromHeader{
			Address: defaultSipUri(target),
			Params:  sip.NewParams(),
		}
		defaultFrom.Params.Add("tag", sip.String{Str: NewBranchGenerator().Next()})
		hdrs = append(hdrs, defaultFrom)
	}

	// To — mandatory.
	if cfg.to != "" {
		addr, err := parseAddressValue(cfg.to)
		if err != nil {
			return nil, fmt.Errorf("sip: To %q: %w", cfg.to, err)
		}
		hdrs = append(hdrs, &sip.ToHeader{Address: addr})
	} else {
		hdrs = append(hdrs, &sip.ToHeader{
			Address: defaultSipUri(target),
			Params:  sip.NewParams(),
		})
	}

	// Call-ID.
	if cfg.callID != "" {
		callID := sip.CallID(cfg.callID)
		hdrs = append(hdrs, &callID)
	}

	// Via — mandatory. Port is intentionally nil: gosip's transport.Layer.Send
	// rewrites the sent-by port from the actual listening socket (see
	// transport/layer.go:208). Hard-coding 5060 here would make tests that
	// bind to an ephemeral port fail with "connection on port X not found".
	viaHost := cfg.viaHost
	if viaHost == "" {
		viaHost = "127.0.0.1"
	}
	branch := DefaultBranchGenerator.Next()
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.String{Str: branch})
	viaHop := &sip.ViaHop{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "UDP",
		Host:            viaHost,
		Params:          viaParams,
	}
	// ViaHeader is [] *ViaHop — a slice value that itself implements Header.
	vh := sip.ViaHeader{viaHop}
	hdrs = append(hdrs, vh)

	// Max-Forwards — default 70.
	if cfg.maxForwards == 0 {
		cfg.maxForwards = 70
	}
	mf := sip.MaxForwards(cfg.maxForwards)
	hdrs = append(hdrs, &mf)

	// User-Agent.
	ua := cfg.userAgent
	if ua == "" {
		ua = "gb28181-simulator/" + Version
	}
	uaHeader := sip.UserAgentHeader(ua)
	hdrs = append(hdrs, &uaHeader)

	// Content-Type.
	if cfg.contentType != "" {
		ct := sip.ContentType(cfg.contentType)
		hdrs = append(hdrs, &ct)
	}

	// Extra headers (e.g. X-GB-Ver).
	for _, h := range cfg.headers {
		hdrs = append(hdrs, h)
	}
	if cfg.xGBVer != "" {
		hdrs = append(hdrs, newXGBVer(cfg.xGBVer))
	}

	req := sip.NewRequest(
		"",
		method,
		recipient,
		"SIP/2.0",
		hdrs,
		cfg.body,
		nil,
	)
	if cfg.body != "" {
		req.SetBody(cfg.body, true)
	}
	return req, nil
}

// --- Response -------------------------------------------------------------

// BuildResponse constructs a SIP response message with a sensible default
// reason phrase.
func BuildResponse(status sip.StatusCode, opts ...BuildOption) (sip.Response, error) {
	cfg := buildConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	reason := defaultReasonFor(status)

	hdrs := make([]sip.Header, 0, 10)
	if cfg.xGBVer != "" {
		hdrs = append(hdrs, newXGBVer(cfg.xGBVer))
	}
	for _, h := range cfg.headers {
		hdrs = append(hdrs, h)
	}

	resp := sip.NewResponse(
		"",
		"SIP/2.0",
		status,
		reason,
		hdrs,
		cfg.body,
		nil,
	)
	if cfg.body != "" {
		resp.SetBody(cfg.body, true)
	}
	return resp, nil
}

// --- Helpers --------------------------------------------------------------

func defaultReasonFor(code sip.StatusCode) string {
	switch int(code) {
	case 100:
		return "Trying"
	case 180:
		return "Ringing"
	case 200:
		return "OK"
	case 401:
		return "Unauthorized"
	case 404:
		return "Not Found"
	case 407:
		return "Proxy Authentication Required"
	case 486:
		return "Busy Here"
	case 487:
		return "Request Terminated"
	case 500:
		return "Server Internal Error"
	}
	return "OK"
}

// parseAddressValue wraps gosip's parser.ParseAddressValue and converts
// its three return values into a sip.Uri suitable for From/To headers.
// Gosip's FromHeader.Address field is of type sip.Uri (interface), so we
// return the parsed Uri directly.
func parseAddressValue(s string) (sip.Uri, error) {
	_, uri, _, err := parser.ParseAddressValue(s)
	if err != nil {
		return nil, err
	}
	return uri, nil
}

// defaultSipUri falls back to parser.ParseUri on parse failure so that
// BuildRequest never returns an error for a missing/default URI.
func defaultSipUri(target string) sip.Uri {
	u, err := parser.ParseUri(target)
	if err != nil {
		return nil
	}
	return u
}

// CSeqNo extracts the numeric portion of a CSeq header on a message.
func CSeqNo(msg sip.Message) uint32 {
	cseq, ok := msg.CSeq()
	if !ok {
		return 0
	}
	return cseq.SeqNo
}

// StartLine returns the start-line of a message (e.g. "INVITE sip:x@y SIP/2.0").
func StartLine(msg sip.Message) string {
	return msg.StartLine()
}

// ContentLengthString returns Content-Length as a string, "-" if absent.
func ContentLengthString(msg sip.Message) string {
	cl, ok := msg.ContentLength()
	if !ok || cl == nil {
		return "-"
	}
	return fmt.Sprintf("%d", uint32(*cl))
}

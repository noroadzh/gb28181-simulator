// Package sip provides SIP message build/parse helpers for GB/T 28181-2016.
package sip

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/ghettovoice/gosip/sip"
	"github.com/ghettovoice/gosip/sip/parser"
)

// buildConfig accumulates options for BuildRequest / BuildResponse.
type buildConfig struct {
	headers      []sip.Header
	body         string
	contentType  string
	viaHost      string
	viaTransport string
	via          string
	from         string
	to           string
	contact      string
	callID       string
	seqNo        uint32
	cseqMethod   string
	maxForwards  uint32
	userAgent    string
	expires      *uint32
	xGBVer       string
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

// WithVia supplies a complete Via header value, e.g.
// "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-...". A response echoes the
// Via of the request it answers, so BuildResponse honours it; BuildRequest
// generates its own Via (see WithViaHost / WithTransport) and ignores this.
func WithVia(value string) BuildOption {
	return func(c *buildConfig) { c.via = value }
}

// WithFrom sets the From header value (e.g. "sip:alice@example.com").
func WithFrom(addr string) BuildOption {
	return func(c *buildConfig) { c.from = addr }
}

// WithTo sets the To header value.
func WithTo(addr string) BuildOption {
	return func(c *buildConfig) { c.to = addr }
}

// WithContact sets the Contact header value, e.g.
// "sip:34020000001320000001@192.168.1.10:5060". REGISTER carries the
// address a platform must use to reach this node back.
func WithContact(addr string) BuildOption {
	return func(c *buildConfig) { c.contact = addr }
}

// WithExpires sets the Expires header (seconds). It is optional; 0 is a
// legal value (RFC 3261 §20.19 uses it to request de-registration), so the
// builder distinguishes "unset" from "zero".
func WithExpires(seconds uint32) BuildOption {
	return func(c *buildConfig) {
		v := seconds
		c.expires = &v
	}
}

// WithTransport overrides the transport token written into the generated
// Via header. It defaults to "UDP"; a node listening on TCP must produce
// "SIP/2.0/TCP" or peers will answer it on the wrong protocol.
func WithTransport(proto string) BuildOption {
	return func(c *buildConfig) { c.viaTransport = proto }
}

// WithCallID sets the Call-ID header. If unset, gosip generates one.
func WithCallID(id string) BuildOption {
	return func(c *buildConfig) { c.callID = id }
}

// WithCSeq sets the CSeq number. Defaults to 1.
func WithCSeq(n uint32) BuildOption {
	return func(c *buildConfig) { c.seqNo = n }
}

// WithCSeqMethod supplies the method token of a CSeq header. BuildRequest
// knows the method already and ignores it; BuildResponse needs it because a
// response's CSeq echoes the request method ("1 REGISTER").
func WithCSeqMethod(m string) BuildOption {
	return func(c *buildConfig) { c.cseqMethod = m }
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

	// From — mandatory. A request's From carries a tag
	// (RFC 3261 §8.1.1.7); the address parser returns only the URI, so a
	// tag supplied by the caller is re-attached rather than dropped.
	if cfg.from != "" {
		addr, err := parseAddressValue(cfg.from)
		if err != nil {
			return nil, fmt.Errorf("sip: From %q: %w", cfg.from, err)
		}
		hdr := &sip.FromHeader{Address: addr, Params: sip.NewParams()}
		if tag, ok := headerParam(cfg.from, "tag"); ok {
			hdr.Params.Add("tag", sip.String{Str: tag})
		}
		hdrs = append(hdrs, hdr)
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
		hdr := &sip.ToHeader{Address: addr, Params: sip.NewParams()}
		if tag, ok := headerParam(cfg.to, "tag"); ok {
			hdr.Params.Add("tag", sip.String{Str: tag})
		}
		hdrs = append(hdrs, hdr)
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
	viaTransport := cfg.viaTransport
	if viaTransport == "" {
		viaTransport = "UDP"
	}
	branch := DefaultBranchGenerator.Next()
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.String{Str: branch})
	viaHop := &sip.ViaHop{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       viaTransport,
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

	// Contact / Expires — optional, carried by REGISTER and its 200 OK.
	hdrs, err = appendOptionalHeaders(hdrs, &cfg)
	if err != nil {
		return nil, err
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
	if cfg.via != "" {
		via, err := parseViaHeader(cfg.via)
		if err != nil {
			return nil, fmt.Errorf("sip: Via %q: %w", cfg.via, err)
		}
		hdrs = append(hdrs, via)
	}
	// Call-ID / CSeq / From / To — a response echoes the request's
	// transaction identifiers. All are optional so a bare response keeps
	// exactly the shape it had before they existed.
	if cfg.callID != "" {
		callID := sip.CallID(cfg.callID)
		hdrs = append(hdrs, &callID)
	}
	if cfg.seqNo != 0 && cfg.cseqMethod != "" {
		hdrs = append(hdrs, &sip.CSeq{
			SeqNo:      cfg.seqNo,
			MethodName: sip.RequestMethod(cfg.cseqMethod),
		})
	}
	if cfg.from != "" {
		addr, err := parseAddressValue(cfg.from)
		if err != nil {
			return nil, fmt.Errorf("sip: From %q: %w", cfg.from, err)
		}
		hdr := &sip.FromHeader{Address: addr, Params: sip.NewParams()}
		if tag, ok := headerParam(cfg.from, "tag"); ok {
			hdr.Params.Add("tag", sip.String{Str: tag})
		}
		hdrs = append(hdrs, hdr)
	}
	if cfg.to != "" {
		addr, err := parseAddressValue(cfg.to)
		if err != nil {
			return nil, fmt.Errorf("sip: To %q: %w", cfg.to, err)
		}
		hdr := &sip.ToHeader{Address: addr, Params: sip.NewParams()}
		if tag, ok := headerParam(cfg.to, "tag"); ok {
			hdr.Params.Add("tag", sip.String{Str: tag})
		}
		hdrs = append(hdrs, hdr)
	}
	hdrs, err := appendOptionalHeaders(hdrs, &cfg)
	if err != nil {
		return nil, err
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

// appendOptionalHeaders appends the optional Contact and Expires headers
// when the caller supplied them. Both are legal on requests (REGISTER) and
// on responses (its 200 OK), so BuildRequest and BuildResponse share it.
func appendOptionalHeaders(hdrs []sip.Header, cfg *buildConfig) ([]sip.Header, error) {
	if cfg.contact != "" {
		addr, err := parseAddressValue(cfg.contact)
		if err != nil {
			return nil, fmt.Errorf("sip: Contact %q: %w", cfg.contact, err)
		}
		hdrs = append(hdrs, &sip.ContactHeader{Address: addr})
	}
	if cfg.expires != nil {
		ex := sip.Expires(*cfg.expires)
		hdrs = append(hdrs, &ex)
	}
	return hdrs, nil
}

// headerParam extracts a header parameter ("tag=abc") from an address
// value. Address parsing hands back only the URI, so parameters that live
// on the header itself have to be read separately.
func headerParam(value, key string) (string, bool) {
	for _, part := range strings.Split(value, ";")[1:] {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

// parseViaHeader turns a Via header value ("SIP/2.0/UDP host:port;branch=x")
// into the gosip ViaHeader the stack recognises. Gosip looks Via up by
// type, so a generic "Via: ..." header would not satisfy it.
func parseViaHeader(value string) (sip.ViaHeader, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty value")
	}
	parts := strings.Split(fields[0], "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed protocol %q", fields[0])
	}
	hop := &sip.ViaHop{
		ProtocolName:    parts[0],
		ProtocolVersion: parts[1],
		Transport:       parts[2],
		Params:          sip.NewParams(),
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), fields[0]))
	if rest == "" {
		return sip.ViaHeader{hop}, nil
	}
	segs := strings.Split(rest, ";")
	host := strings.TrimSpace(segs[0])
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if n, cerr := strconv.ParseUint(p, 10, 16); cerr == nil {
			port := sip.Port(n)
			hop.Port = &port
		}
	}
	hop.Host = host
	for _, seg := range segs[1:] {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		key, val, _ := strings.Cut(seg, "=")
		hop.Params.Add(strings.TrimSpace(key), sip.String{Str: strings.TrimSpace(val)})
	}
	return sip.ViaHeader{hop}, nil
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

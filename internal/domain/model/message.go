// Package model — Message.
package model

import (
	"fmt"
	"net/url"
	"strings"
)

// Header is an immutable SIP header (name, value pair). Value MUST NOT
// contain CR/LF; constructors panic on a header line that would break the
// wire format. The same name may legitimately appear multiple times in a
// SIP message (e.g. Via, Route); each occurrence is its own Header so the
// slice preserves order.
type Header struct {
	name  string
	value string
}

// NewHeader constructs a Header. Panics if name is empty, contains a colon,
// CR or LF, or if value contains CR or LF.
func NewHeader(name, value string) Header {
	if name == "" {
		panic("model: header name is empty")
	}
	if strings.ContainsAny(name, ":\r\n") {
		panic(fmt.Sprintf("model: invalid header name %q", name))
	}
	if strings.ContainsAny(value, "\r\n") {
		panic(fmt.Sprintf("model: header value for %q contains CR/LF", name))
	}
	return Header{name: name, value: value}
}

// Name returns the header name (case-insensitive equality is the caller's
// responsibility; SIP normalises to title-case in the wire format).
func (h Header) Name() string { return h.name }

// Value returns the verbatim header value.
func (h Header) Value() string { return h.value }

// String renders the header in RFC 3261 §7 form: "Name: value".
func (h Header) String() string {
	return h.name + ": " + h.value
}

// Message is an immutable SIP request or response. Constructors panic on any
// structural violation (empty method, malformed URI, empty required
// header). Adapters copy the underlying byte buffers so callers cannot
// observe mutations of the wire after the message has been built.
type Message struct {
	method     string
	uri        *url.URL
	statusCode int
	statusText string
	headers    []Header
	body       string
}

// NewRequest builds a request Message. uriStr is parsed with url.Parse
// (absolute form required). Method is upper-cased on the wire so callers
// may pass "register" or "REGISTER" interchangeably.
func NewRequest(method, uriStr string, headers []Header, body string) (Message, error) {
	if method == "" {
		return Message{}, fmt.Errorf("model: empty method")
	}
	u, err := url.Parse(uriStr)
	if err != nil {
		return Message{}, fmt.Errorf("model: parse URI %q: %w", uriStr, err)
	}
	if u.Scheme == "" {
		return Message{}, fmt.Errorf("model: URI %q missing scheme", uriStr)
	}
	cp := append([]Header(nil), headers...)
	return Message{
		method:  strings.ToUpper(method),
		uri:     u,
		headers: cp,
		body:    body,
	}, nil
}

// NewResponse builds a response Message. statusText may be empty (RFC 3261
// §7.2 allows the reason phrase to be empty).
func NewResponse(statusCode int, statusText string, headers []Header, body string) (Message, error) {
	if statusCode < 100 || statusCode > 999 {
		return Message{}, fmt.Errorf("model: status code %d out of range", statusCode)
	}
	cp := append([]Header(nil), headers...)
	return Message{
		statusCode: statusCode,
		statusText: statusText,
		headers:    cp,
		body:       body,
	}, nil
}

// Method returns the upper-case method for requests, or "" for responses.
func (m Message) Method() string { return m.method }

// URI returns the request URI; nil for responses.
func (m Message) URI() *url.URL { return m.uri }

// StatusCode returns 0 for requests.
func (m Message) StatusCode() int { return m.statusCode }

// StatusText returns the reason phrase; may be empty.
func (m Message) StatusText() string { return m.statusText }

// Headers returns a copy of the header slice (defensive copy so callers
// cannot mutate the underlying array).
func (m Message) Headers() []Header {
	cp := append([]Header(nil), m.headers...)
	return cp
}

// Header returns the first header whose name equals name (case-insensitive),
// or the zero Header and false if no such header is present.
func (m Message) Header(name string) (Header, bool) {
	for _, h := range m.headers {
		if strings.EqualFold(h.name, name) {
			return h, true
		}
	}
	return Header{}, false
}

// Body returns the verbatim message body.
func (m Message) Body() string { return m.body }

// IsRequest reports whether the message is a request (Method != "").
func (m Message) IsRequest() bool { return m.method != "" }

// IsResponse reports whether the message is a response (StatusCode != 0).
func (m Message) IsResponse() bool { return m.statusCode != 0 }

// String returns a one-line debug representation; never used on the wire.
func (m Message) String() string {
	if m.IsRequest() {
		return fmt.Sprintf("REQUEST %s %s (%d headers, %d-byte body)",
			m.method, m.uri.String(), len(m.headers), len(m.body))
	}
	return fmt.Sprintf("RESPONSE %d %s (%d headers, %d-byte body)",
		m.statusCode, m.statusText, len(m.headers), len(m.body))
}
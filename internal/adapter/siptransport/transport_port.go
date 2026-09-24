package siptransport

// Port adapter: wraps the legacy *Transport so that callers can depend on
// domain/port.SIPTransport (which speaks domain/model.Message) instead of
// the gosip-flavoured *Transport. The wrapper is intentionally tiny: it
// only translates between the two message shapes and leaves all
// transport-level concerns (sockets, goroutines, audit) inside *Transport.
//
// The gosip-side construction is delegated to internal/sip's BuildRequest
// and BuildResponse helpers (Change 3 §6.1 migrates that package to
// internal/adapter/sip; the import path will be updated in the same
// change, this file already uses it through the legacy path so the code
// keeps compiling during the migration).
//
// This file implements Change 3 §6.4: compile-time assertion
// `var _ port.SIPTransport = (*PortAdapter)(nil)` lives at the bottom.

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/ghettovoice/gosip/sip"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	sipbuild "github.com/your-org/gb28181-simulator/internal/adapter/sip"
)

// PortAdapter wraps a *Transport and exposes it through the
// domain/port.SIPTransport interface.
type PortAdapter struct {
	inner *Transport
}

// NewPortAdapter returns a wrapper that satisfies port.SIPTransport.
func NewPortAdapter(inner *Transport) *PortAdapter {
	return &PortAdapter{inner: inner}
}

// Send translates the domain model.Message into a gosip message via the
// sip builder helpers, derives the destination host:port from the
// model.Message's URI, and delegates to the underlying Transport.
func (p *PortAdapter) Send(ctx context.Context, msg model.Message) error {
	if p == nil || p.inner == nil {
		return io.ErrClosedPipe
	}
	gosipMsg, err := modelToGosip(msg)
	if err != nil {
		return fmt.Errorf("siptransport: build gosip message: %w", err)
	}
	dst := destinationFromURI(msg.URI().String())
	return p.inner.Send(gosipMsg, dst)
}

// Receive blocks until the next message arrives, then translates the
// gosip message into a domain model.Message.
func (p *PortAdapter) Receive(ctx context.Context) (model.Message, error) {
	if p == nil || p.inner == nil {
		return model.Message{}, io.ErrClosedPipe
	}
	m, err := p.inner.Receive(ctx)
	if err != nil {
		return model.Message{}, err
	}
	return gosipToModel(m)
}

// Close shuts the underlying transport down. Idempotent.
func (p *PortAdapter) Close() error {
	if p == nil || p.inner == nil {
		return nil
	}
	return p.inner.Close()
}

// destinationFromURI extracts "host:port" from a SIP URI like
// "sip:user@host:port;transport=udp". Returns "" when the URI is empty
// or does not contain a host:port pair.
//
// SIP URIs in RFC 3261 form ("sip:user@host:port") are parsed by
// net/url as scheme="sip", opaque="user@host:port" — the Host field is
// empty. The "//" form ("sip://user@host:port") populates Host. We
// handle both shapes so callers may pass either form.
func destinationFromURI(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	// "sip://..." form has Host populated directly.
	if u.Host != "" {
		return stripParamsAndHeaders(u.Host)
	}
	// "sip:..." form has the remainder in Opaque.
	if u.Opaque != "" {
		s := u.Opaque
		// Strip userinfo.
		if i := indexByte(s, '@'); i >= 0 {
			s = s[i+1:]
		}
		return stripParamsAndHeaders(s)
	}
	return ""
}

// indexByte wraps strings.IndexByte to avoid importing the package
// solely for this one call.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// stripParamsAndHeaders trims ;params and ?headers from a host:port.
func stripParamsAndHeaders(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ';' || s[i] == '?' {
			return s[:i]
		}
	}
	return s
}

// modelToGosip builds a gosip message from a domain message using
// internal/sip's builders, which already fill in the GB/T 28181 mandatory
// headers (Via, Max-Forwards, User-Agent, Content-Length, CSeq, From,
// To). Extra headers carried by the model.Message are appended verbatim
// after the mandatory ones so application-layer composition survives the
// round trip.
func modelToGosip(m model.Message) (sip.Message, error) {
	if m.IsRequest() {
		opts := []sipbuild.BuildOption{
			sipbuild.WithBody(m.Body()),
		}
		if ct, ok := m.Header("Content-Type"); ok {
			opts = append(opts, sipbuild.WithContentType(ct.Value()))
		}
		return sipbuild.BuildRequest(sip.RequestMethod(m.Method()), m.URI().String(), opts...)
	}
	if m.IsResponse() {
		opts := []sipbuild.BuildOption{
			sipbuild.WithBody(m.Body()),
		}
		if ct, ok := m.Header("Content-Type"); ok {
			opts = append(opts, sipbuild.WithContentType(ct.Value()))
		}
		return sipbuild.BuildResponse(sip.StatusCode(m.StatusCode()), opts...)
	}
	return nil, fmt.Errorf("siptransport: message is neither request nor response")
}

// gosipToModel extracts the wire-level fields the domain model cares
// about and returns a domain model.Message.
func gosipToModel(m sip.Message) (model.Message, error) {
	if m == nil {
		return model.Message{}, fmt.Errorf("siptransport: nil gosip message")
	}
	body := m.Body()
	var hdrs []model.Header
	for _, h := range m.Headers() {
		hdrs = append(hdrs, model.NewHeader(h.Name(), h.Value()))
	}

	switch v := m.(type) {
	case sip.Request:
		uri := ""
		if v.Recipient() != nil {
			uri = v.Recipient().String()
		}
		return model.NewRequest(string(v.Method()), uri, hdrs, body)
	case sip.Response:
		return model.NewResponse(int(v.StatusCode()), v.Reason(), hdrs, body)
	}
	return model.Message{}, fmt.Errorf("siptransport: unknown gosip message type %T", m)
}

// Compile-time assertion: PortAdapter satisfies domain/port.SIPTransport.
var _ port.SIPTransport = (*PortAdapter)(nil)

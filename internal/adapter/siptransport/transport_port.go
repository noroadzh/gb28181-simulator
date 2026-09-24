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
	"strconv"
	"strings"

	"github.com/ghettovoice/gosip/sip"

	sipbuild "github.com/your-org/gb28181-simulator/internal/adapter/sip"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
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

// Send translates the domain model.Message into a gosip message via the sip
// builder helpers and transmits it to dst ("host:port"). The destination is
// supplied by the caller and is never inferred from the message URI
// (design D4). An empty dst is an error.
func (p *PortAdapter) Send(ctx context.Context, msg model.Message, dst string) error {
	if p == nil || p.inner == nil {
		return io.ErrClosedPipe
	}
	if strings.TrimSpace(dst) == "" {
		return fmt.Errorf("siptransport: empty destination")
	}
	// The Via hop must name the protocol this socket actually speaks,
	// otherwise a TCP peer is told to answer over UDP.
	gosipMsg, err := modelToGosip(msg, sipbuild.WithTransport(strings.ToUpper(p.inner.Protocol())))
	if err != nil {
		return fmt.Errorf("siptransport: build gosip message: %w", err)
	}
	return p.inner.Send(gosipMsg, dst)
}

// Receive blocks until the next message arrives, then translates the gosip
// message into a domain model.Message and reports the address it arrived
// from. The address may be handed straight back to Send to answer the peer.
func (p *PortAdapter) Receive(ctx context.Context) (model.Message, string, error) {
	if p == nil || p.inner == nil {
		return model.Message{}, "", io.ErrClosedPipe
	}
	m, src, err := p.inner.Receive(ctx)
	if err != nil {
		return model.Message{}, "", err
	}
	msg, err := gosipToModel(m)
	if err != nil {
		return model.Message{}, "", err
	}
	return msg, src, nil
}

// Close shuts the underlying transport down. Idempotent.
func (p *PortAdapter) Close() error {
	if p == nil || p.inner == nil {
		return nil
	}
	return p.inner.Close()
}

// modelToGosip builds a gosip message from a domain message, preserving
// every header the caller put on the model.Message.
//
// Headers the builder understands are handed to it as options (From, To,
// Call-ID, CSeq, Contact, Expires, Max-Forwards, User-Agent, Content-Type)
// so the generated value is *replaced* rather than duplicated; anything
// else — Contact parameters, Authorization, X-GB-Ver, Route — is appended
// verbatim. Two headers are deliberately not copied:
//
//   - Via on a request: it is hop-specific, so the builder emits a fresh
//     one with its own branch; copying the caller's would produce a second
//     Via. A response has no Via of its own — it echoes the request's — so
//     there the caller's Via is kept.
//   - Content-Length: derived from the body; an echoed value would only
//     risk contradicting the computed one.
func modelToGosip(m model.Message, extra ...sipbuild.BuildOption) (sip.Message, error) {
	opts, err := modelBuildOptions(m, m.IsRequest())
	if err != nil {
		return nil, err
	}
	opts = append(opts, extra...)
	if m.IsRequest() {
		return sipbuild.BuildRequest(sip.RequestMethod(m.Method()), m.URI().String(), opts...)
	}
	if m.IsResponse() {
		return sipbuild.BuildResponse(sip.StatusCode(m.StatusCode()), opts...)
	}
	return nil, fmt.Errorf("siptransport: message is neither request nor response")
}

// modelBuildOptions translates a domain message's headers into builder
// options. isRequest selects the Via handling described on modelToGosip.
func modelBuildOptions(m model.Message, isRequest bool) ([]sipbuild.BuildOption, error) {
	opts := []sipbuild.BuildOption{sipbuild.WithBody(m.Body())}
	for _, h := range m.Headers() {
		name, value := h.Name(), h.Value()
		switch {
		case strings.EqualFold(name, "Content-Type"):
			opts = append(opts, sipbuild.WithContentType(value))
		case strings.EqualFold(name, "Content-Length"):
			// Derived from the body by the builder; see modelToGosip.
		case strings.EqualFold(name, "Via") && isRequest:
			// Regenerated by the builder; see modelToGosip.
		case strings.EqualFold(name, "Via"):
			opts = append(opts, sipbuild.WithVia(value))
		case strings.EqualFold(name, "From"):
			opts = append(opts, sipbuild.WithFrom(value))
		case strings.EqualFold(name, "To"):
			opts = append(opts, sipbuild.WithTo(value))
		case strings.EqualFold(name, "Call-ID"):
			opts = append(opts, sipbuild.WithCallID(value))
		case strings.EqualFold(name, "Contact"):
			opts = append(opts, sipbuild.WithContact(value))
		case strings.EqualFold(name, "CSeq"):
			seqNo, method, err := parseCSeq(value)
			if err != nil {
				return nil, fmt.Errorf("siptransport: CSeq %q: %w", value, err)
			}
			opts = append(opts, sipbuild.WithCSeq(seqNo), sipbuild.WithCSeqMethod(method))
		case strings.EqualFold(name, "Expires"):
			n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("siptransport: Expires %q: %w", value, err)
			}
			opts = append(opts, sipbuild.WithExpires(uint32(n)))
		case strings.EqualFold(name, "Max-Forwards"):
			n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("siptransport: Max-Forwards %q: %w", value, err)
			}
			opts = append(opts, sipbuild.WithMaxForwards(uint32(n)))
		case strings.EqualFold(name, "User-Agent"):
			opts = append(opts, sipbuild.WithUserAgent(value))
		default:
			opts = append(opts, sipbuild.WithHeader(&sip.GenericHeader{
				HeaderName: name,
				Contents:   value,
			}))
		}
	}
	return opts, nil
}

// parseCSeq splits a CSeq value ("1 REGISTER") into its number and method.
// The method may be absent; the number is mandatory.
func parseCSeq(value string) (uint32, string, error) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 0, "", fmt.Errorf("empty value")
	}
	n, err := strconv.ParseUint(fields[0], 10, 32)
	if err != nil {
		return 0, "", err
	}
	if len(fields) > 1 {
		return uint32(n), fields[1], nil
	}
	return uint32(n), "", nil
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

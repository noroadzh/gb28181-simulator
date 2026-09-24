// Package siptransport wraps ghettovoice/gosip's transport.Layer with a
// thin, multi-instance friendly facade.
//
// It provides a single Transport type that owns one underlying socket and
// exposes bounded-channel Send/Receive plus an audit hook (see
// internal/sip/audit). Multiple Transport instances may coexist inside one
// process — for example Change 4's Node abstraction creates one Transport
// per listening port, while Change 14's HTTP admin plane owns another.
//
// The package is deliberately small (≈250 LoC). All complex SIP/SDP logic
// lives in internal/sip, internal/sdp and internal/auth.
package siptransport

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ghettovoice/gosip/log"
	"github.com/ghettovoice/gosip/sip"
	"github.com/ghettovoice/gosip/transport"

	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
)

// DefaultReceiveBuffer is the receive-channel capacity. 64 is the spec
// value (tasks §6.2). Tests can pass a smaller buffer via WithReceiveBuffer.
const DefaultReceiveBuffer = 64

// Transport owns one listening socket and provides a Send/Receive API
// suitable for GB/T 28181 server UACs/UASes. It is safe to call Send from
// multiple goroutines; Receive serialises access to a single
// <-chan sip.Message.
//
// Lifecycle: New → (Send | Receive)* → Close. After Close any further
// Send/Receive returns io.ErrClosedPipe.
type Transport struct {
	layer    transport.Layer
	bind     string
	protocol string
	addr     string

	out    chan sip.Message
	stop   chan struct{}
	once   sync.Once
	closed bool
	mu     sync.Mutex // guards closed
}

// New creates a Transport bound to bind. bind must be of the form
// "udp://host:port" or "tcp://host:port" or "tls://host:port".
//
// If host is "0.0.0.0" or empty the system picks one; if port is "0" the
// system picks one (use LocalAddr() to discover it). bind with port "0" is
// commonly used by tests to avoid collisions.
//
// TLS scheme currently behaves like TCP — full TLS wiring is delegated to
// Change 12 (GB35114). The shape is preserved now so callers can be written
// in advance.
func New(bind string, opts ...Option) (*Transport, error) {
	scheme, addr, err := parseBind(bind)
	if err != nil {
		return nil, err
	}
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	layer := transport.NewLayer(
		net.ParseIP("0.0.0.0"),
		nil,          // default DNS resolver
		nil,          // no message mapper
		newNoopLogger(), // gosip requires non-nil logger
	)
	if err := layer.Listen(scheme, addr); err != nil {
		return nil, fmt.Errorf("siptransport: listen %s %s: %w", scheme, addr, err)
	}
	// Use a bounded receive channel; Messages() from layer is unbounded,
	// so we forward non-blockingly and drop under back-pressure (design R5).
	out := make(chan sip.Message, cfg.receiveBuffer)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case msg, ok := <-layer.Messages():
				if !ok {
					return
				}
				select {
				case out <- msg:
					audit.Global().Emit(audit.WireEvent{
						Direction: audit.DirReceive,
						Local:     addr,
						Bytes:     []byte(msg.String()),
						Timestamp: time.Now(),
					})
				default:
					// Channel full — drop silently per design R5.
				}
			}
		}
	}()
	return &Transport{
		layer:    layer,
		bind:     bind,
		protocol: scheme,
		addr:     addr,
		out:      out,
		stop:     stop,
	}, nil
}

// LocalAddr returns the local network address the transport is bound to.
func (t *Transport) LocalAddr() string { return t.addr }

// Protocol returns the lower-cased scheme ("udp"/"tcp"/"tls").
func (t *Transport) Protocol() string { return t.protocol }

// Receive returns the next incoming message. ctx cancellation aborts the
// wait. The remote endpoint is not carried by gosip's Messages() channel;
// callers needing the peer address should parse it from the topmost Via
// header (Change 4+).
func (t *Transport) Receive(ctx context.Context) (sip.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-t.out:
		if !ok {
			return nil, io.ErrClosedPipe
		}
		return msg, nil
	}
}

// Send writes msg to dst. dst is "host:port". The message is serialised via
// gosip's String() method; UDP/TCP framing is the layer's concern. msg may be
// either a Request or a Response — server UACs/UASes need to send both.
func (t *Transport) Send(msg sip.Message, dst string) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return io.ErrClosedPipe
	}
	t.mu.Unlock()
	if dst != "" && !strings.Contains(dst, ":") {
		return fmt.Errorf("siptransport: destination %q lacks :port", dst)
	}
	// gosip's transport layer uses Destination() to pick the outbound
	// connection; without it the Send falls back to the Via port and
	// returns "connection on port X not found". Only Request has it, so we
	// guard with a type assertion.
	if req, ok := msg.(sip.Request); ok {
		req.SetDestination(dst)
	}
	if err := t.layer.Send(msg); err != nil {
		return err
	}
	audit.Global().Emit(audit.WireEvent{
		Direction: audit.DirTransmit,
		Local:     t.addr,
		Remote:    dst,
		Bytes:     []byte(msg.String()),
		Timestamp: time.Now(),
	})
	return nil
}

// Close releases the socket and the receive-forwarding goroutine. Idempotent.
func (t *Transport) Close() error {
	t.once.Do(func() {
		t.mu.Lock()
		t.closed = true
		close(t.stop)
		// Closing out unblocks any pending Receive calls with io.ErrClosedPipe.
		// Safe because the forwarding goroutine has exited (stop is closed)
		// and Send rejects new requests via the closed flag above.
		close(t.out)
		t.mu.Unlock()
		t.layer.Cancel()
	})
	return nil
}

// parseBind accepts "scheme://host:port" and returns scheme ("udp"/"tcp"/"tls")
// and host:port. A bare "host:port" defaults to UDP for backwards
// compatibility with Change 1's literal address strings.
func parseBind(bind string) (scheme, addr string, err error) {
	bind = strings.TrimSpace(bind)
	if bind == "" {
		return "", "", fmt.Errorf("empty bind")
	}
	switch {
	case strings.HasPrefix(bind, "udp://"):
		return "udp", strings.TrimPrefix(bind, "udp://"), nil
	case strings.HasPrefix(bind, "tcp://"):
		return "tcp", strings.TrimPrefix(bind, "tcp://"), nil
	case strings.HasPrefix(bind, "tls://"):
		return "tls", strings.TrimPrefix(bind, "tls://"), nil
	}
	if _, _, e := net.SplitHostPort(bind); e == nil {
		return "udp", bind, nil
	}
	return "", "", fmt.Errorf("siptransport: bind %q: must be udp|tcp|tls scheme", bind)
}

// noopLogger satisfies gosip's log.Logger without emitting anything.
// gosip's transport.NewLayer panics if logger is nil; use this instead.
type noopLogger struct{}

func newNoopLogger() log.Logger { return &noopLogger{} }

func (n *noopLogger) Print(...interface{})                         {}
func (n *noopLogger) Printf(string, ...interface{})                {}
func (n *noopLogger) Trace(...interface{})                         {}
func (n *noopLogger) Tracef(string, ...interface{})                {}
func (n *noopLogger) Debug(...interface{})                         {}
func (n *noopLogger) Debugf(string, ...interface{})                {}
func (n *noopLogger) Info(...interface{})                          {}
func (n *noopLogger) Infof(string, ...interface{})                 {}
func (n *noopLogger) Warn(...interface{})                          {}
func (n *noopLogger) Warnf(string, ...interface{})                 {}
func (n *noopLogger) Error(...interface{})                         {}
func (n *noopLogger) Errorf(string, ...interface{})                {}
func (n *noopLogger) Fatal(...interface{})                         {}
func (n *noopLogger) Fatalf(string, ...interface{})                {}
func (n *noopLogger) Panic(...interface{})                         {}
func (n *noopLogger) Panicf(string, ...interface{})                {}
func (n *noopLogger) WithPrefix(string) log.Logger                 { return n }
func (n *noopLogger) Prefix() string                               { return "" }
func (n *noopLogger) WithFields(map[string]interface{}) log.Logger { return n }
func (n *noopLogger) Fields() log.Fields                           { return nil }
func (n *noopLogger) SetLevel(uint32)                              {}

// compile-time assertion: keep time import honest for callers that wire
// timeout contexts alongside Transport.
var _ = time.Second
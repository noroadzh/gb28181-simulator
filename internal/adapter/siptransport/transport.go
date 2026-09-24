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
		nil,             // default DNS resolver
		nil,             // no message mapper
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

// Receive returns the next incoming message together with the address it
// arrived from, in "host:port" form. ctx cancellation aborts the wait.
//
// gosip's Messages() channel does not carry a peer address, but its
// connection handler records one on each message via SetSource before
// publishing it (transport/connection_pool.go handleMessage: the UDP remote
// address from ReadFromUDP, or conn.RemoteAddr() for streamed protocols).
// We read it back here, which is the "keep it in the receive loop" approach
// design D4 asks for without re-implementing the socket layer.
//
// An address that is missing or lacks a port is an error: silently
// returning "" would let a caller reply to the wrong destination (spec:
// "A transport that cannot determine the peer address MUST surface an
// explicit error").
func (t *Transport) Receive(ctx context.Context) (sip.Message, string, error) {
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case msg, ok := <-t.out:
		if !ok {
			return nil, "", io.ErrClosedPipe
		}
		src := strings.TrimSpace(msg.Source())
		if src == "" {
			return nil, "", fmt.Errorf("siptransport: peer address unavailable for %T", msg)
		}
		if !strings.Contains(src, ":") {
			return nil, "", fmt.Errorf("siptransport: peer address %q lacks :port", src)
		}
		return msg, src, nil
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
	// returns "connection on port X not found". Set it on every message
	// (the method lives on the shared sip.Message interface) so the
	// caller's explicit dst wins for responses too — design D4 requires the
	// destination to be explicit, never inferred from the URI or Via.
	msg.SetDestination(dst)
	// gosip picks the outbound connection by the port in Source(). A
	// request falls back to the topmost Via when Source is empty, but a
	// response has no such fallback and fails with "resolve source port
	// failed", so set it explicitly for every message we originate.
	if msg.Source() == "" {
		msg.SetSource(t.addr)
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
		// gosip's layer.Cancel() only closes a "canceled" channel; the
		// listening socket is released by its own goroutine afterwards, so
		// Close can return while the port is still bound. Wait for the port
		// to become bindable again, otherwise a node that is stopped and
		// immediately restarted on the same address (Offline -> Registering)
		// fails with "address already in use". A timeout is not fatal: the
		// caller sees the same bind error it would have seen before, just
		// later.
		_ = waitForPortRelease(t.protocol, t.addr, portReleaseTimeout)
	})
	return nil
}

// portReleaseTimeout bounds how long Close waits for the listening socket to
// be released. It is short because the usual case completes in a few
// milliseconds.
const portReleaseTimeout = 500 * time.Millisecond

// waitForPortRelease polls until addr can be bound again, proving the
// previous listener is gone. It returns immediately for addresses the OS
// assigned (port 0), which are never reused.
func waitForPortRelease(protocol, addr string, timeout time.Duration) error {
	if addr == "" {
		return nil
	}
	if _, port, err := net.SplitHostPort(addr); err != nil || port == "0" {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := probeBind(protocol, addr); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("siptransport: port %s still bound %s after Cancel", addr, timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// probeBind tries to bind addr and immediately releases it. Success means
// the address is free.
func probeBind(protocol, addr string) error {
	switch protocol {
	case "tcp", "tls":
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return l.Close()
	default:
		l, err := net.ListenPacket("udp", addr)
		if err != nil {
			return err
		}
		return l.Close()
	}
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

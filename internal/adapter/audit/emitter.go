// Package audit provides a process-global, thread-safe emitter for SIP
// wire-level events observed by the transport layer.
//
// The package is intentionally tiny (≤100 LoC) and has no dependency on the
// gosip SIP stack or the rest of the simulator. Callers in
// internal/siptransport call Global().Emit on every Send/Receive and the
// production / test wiring decides what to do with the events (logging,
// pcap dump, replay, …).
//
// Concurrency: the global emitter is guarded by an RWMutex. SetEmitter may
// be called from main() at startup and from tests, but never concurrently
// with a Send/Receive that is being audited in another goroutine that observes
// the swap. The recommended pattern is to set the emitter once before any
// transport is created.
package audit

import (
	"sync"
	"time"
)

// Direction constants used by WireEvent.Direction.
const (
	// DirTransmit indicates bytes sent to the wire (Send path).
	DirTransmit = "t"
	// DirReceive indicates bytes received from the wire (Receive path).
	DirReceive = "r"
)

// WireEvent is the unit of audit information captured per SIP message.
//
// Bytes is the raw on-wire serialisation produced by gosip's String()
// method. Callers in tests can decode it with sip.Parse to assert headers.
// Sensitive credentials are not redacted at this layer — see RedactAuthHeader
// for a helper that strips the response="…" field from WWW-Authenticate
// before persisting or logging.
type WireEvent struct {
	// Direction is "t" for transmit, "r" for receive.
	Direction string

	// Local is the transport's local endpoint (host:port). Optional; the
	// transport layer may leave it empty if it does not track it.
	Local string

	// Remote is the peer endpoint (host:port). Optional; gosip's
	// transport.Layer does not expose per-message source addresses on its
	// Messages() channel, so this is typically empty in the default
	// wiring. Callers may populate it when they have a custom mapper.
	Remote string

	// Bytes is the raw serialised SIP message.
	Bytes []byte

	// Timestamp is when the event was captured.
	Timestamp time.Time

	// NodeID is the owning node's identifier, if the transport was constructed
	// with one. Empty means the event is not associated with a specific node
	// (for example, the HTTP admin plane or a transport created before this
	// field was introduced).
	NodeID string
}

// Emitter consumes WireEvents. Implementations must be safe for concurrent
// use; the transport calls Emit from the forwarder goroutine (for Receive)
// and from arbitrary caller goroutines (for Send).
type Emitter interface {
	Emit(WireEvent)
}

// EmitterFunc adapts an ordinary function to the Emitter interface. This
// matches the http.HandlerFunc pattern and makes test wiring trivial:
//
//	emitter := audit.EmitterFunc(func(e audit.WireEvent) { … })
//	audit.SetEmitter(emitter)
type EmitterFunc func(WireEvent)

// Emit calls f(e).
func (f EmitterFunc) Emit(e WireEvent) { f(e) }

// NopEmitter discards every event. It is the default emitter when nothing is
// installed and the equivalent of io.Discard for SIP auditing.
type NopEmitter struct{}

// Emit drops the event.
func (NopEmitter) Emit(WireEvent) {}

// global is the process-wide emitter. It is RWMutex-protected so that
// SetEmitter in main / hot reload does not race with Emit in the transport.
var (
	globalMu sync.RWMutex
	global   Emitter = NopEmitter{}
)

// Global returns the currently installed emitter. It is always non-nil —
// the zero value is a NopEmitter, never a literal nil.
func Global() Emitter {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// SetEmitter replaces the global emitter. Pass nil to reset to the no-op
// emitter. SetEmitter is intended to be called once at startup or once per
// test; calling it from a hot path is supported but not recommended.
func SetEmitter(e Emitter) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if e == nil {
		global = NopEmitter{}
		return
	}
	global = e
}

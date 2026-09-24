package nodereg

import (
	"context"
	"fmt"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time assertion: Lifecycle satisfies the domain lifecycle port.
var _ port.NodeLifecycle = (*Lifecycle)(nil)

// TransportFactory binds a signalling listener on addr and returns the
// transport. It is the seam that keeps this package free of any concrete
// transport: the caller (app layer) injects a factory that already knows
// how to build one.
type TransportFactory func(addr string) (port.SIPTransport, error)

// Lifecycle drives registered nodes through their status machine, owning
// one listener per node. Starting or stopping one node never touches
// another: the per-node transport is tracked separately and released
// individually.
type Lifecycle struct {
	reg     *Registry
	factory TransportFactory

	mu    sync.Mutex
	bound map[string]port.SIPTransport
}

// NewLifecycle returns a lifecycle over reg that binds listeners through
// factory. factory must be non-nil.
func NewLifecycle(reg *Registry, factory TransportFactory) *Lifecycle {
	return &Lifecycle{
		reg:     reg,
		factory: factory,
		bound:   make(map[string]port.SIPTransport),
	}
}

// Start binds the node's signalling listener and advances it to
// StatusRegistering. It deliberately does not advance further: registration
// itself is driven by the identity implementations in Change 5/6/7, via
// NodeService.MarkRegistered / MarkOnline (design D9).
//
// On a bind failure the node is rolled back to StatusIdle (first start) or
// StatusFault (restart after a previous run), the port is released, and the
// error names the address and the cause — a half-started node is never left
// behind.
func (l *Lifecycle) Start(ctx context.Context, id model.NodeID) error {
	if l.factory == nil {
		return fmt.Errorf("nodereg: lifecycle has no transport factory")
	}
	current, ok := l.reg.Get(ctx, id)
	if !ok {
		return fmt.Errorf("nodereg: unknown node %s", id)
	}
	// A faulted node is reset first: fault → idle → registering is the
	// recovery path the status machine allows, and demanding a separate
	// reset call would only add an endpoint the operator cannot act on
	// differently. Every other status jumps to registering directly.
	if current.Status() == model.StatusFault {
		if _, err := l.reg.Advance(ctx, id, model.StatusIdle); err != nil {
			return err
		}
	}
	// Advance first: it is atomic and refuses an illegal jump, so two
	// concurrent Start calls cannot both proceed to bind.
	if _, err := l.reg.Advance(ctx, id, model.StatusRegistering); err != nil {
		return err
	}
	node, ok := l.reg.Get(ctx, id)
	if !ok {
		return fmt.Errorf("nodereg: unknown node %s", id)
	}
	addr := node.Profile().Addr()
	tr, err := l.factory(addr)
	if err != nil {
		// Roll the status back and make sure nothing stays bound.
		rollback := model.StatusIdle
		if current.Status() == model.StatusOffline {
			// A restart that fails to rebind is a fault, not a fresh idle.
			rollback = model.StatusFault
		}
		if _, rerr := l.reg.Advance(ctx, id, rollback); rerr != nil {
			return fmt.Errorf("nodereg: bind %s for node %s: %w (rollback to %s failed: %v)",
				addr, id, err, rollback, rerr)
		}
		l.reg.log().Warn("node bind failed", "node_id", id.String(),
			"addr", addr, "rollback", rollback, "error", err)
		return fmt.Errorf("nodereg: bind %s for node %s: %w", addr, id, err)
	}
	l.mu.Lock()
	l.bound[id.String()] = tr
	l.mu.Unlock()
	l.reg.log().Info("node listener bound", "node_id", id.String(), "addr", addr)
	return nil
}

// Stop releases the node's listener and advances it to StatusOffline.
// Stopping is always legal from a reachable status, so a node can be
// removed cleanly whether it was idle, registering, online or faulted.
func (l *Lifecycle) Stop(ctx context.Context, id model.NodeID) error {
	if _, ok := l.reg.Get(ctx, id); !ok {
		return fmt.Errorf("nodereg: unknown node %s", id)
	}
	if _, err := l.reg.Advance(ctx, id, model.StatusOffline); err != nil {
		return err
	}
	l.release(id)
	l.reg.log().Info("node listener released", "node_id", id.String())
	return nil
}

// Fail ends a start that cannot complete — in practice a registration that
// was refused or timed out. It advances the node to StatusFault and
// releases its listener, so the port can be rebound straight away and the
// node started again once the cause is fixed.
//
// Calling it for a node that bound no listener (or that was stopped in the
// meantime) is safe: releasing is idempotent, and an illegal status jump is
// reported as an error rather than panicking.
func (l *Lifecycle) Fail(ctx context.Context, id model.NodeID, reason error) error {
	if _, ok := l.reg.Get(ctx, id); !ok {
		return fmt.Errorf("nodereg: unknown node %s", id)
	}
	if _, err := l.reg.Advance(ctx, id, model.StatusFault); err != nil {
		return err
	}
	l.release(id)
	args := []any{"node_id", id.String(), "status", model.StatusFault}
	if reason != nil {
		args = append(args, "error", reason.Error())
	}
	l.reg.log().Warn("node start failed", args...)
	return nil
}

// Status returns the node's current status.
func (l *Lifecycle) Status(ctx context.Context, id model.NodeID) (model.Status, error) {
	node, ok := l.reg.Get(ctx, id)
	if !ok {
		return model.StatusIdle, fmt.Errorf("nodereg: unknown node %s", id)
	}
	return node.Status(), nil
}

// Transport returns the listener bound to id, or nil when the node is not
// running. Used by the identity implementations that will drive
// registration in later changes.
func (l *Lifecycle) Transport(id model.NodeID) port.SIPTransport {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bound[id.String()]
}

// release closes and forgets the node's listener. Idempotent.
func (l *Lifecycle) release(id model.NodeID) {
	key := id.String()
	l.mu.Lock()
	tr := l.bound[key]
	delete(l.bound, key)
	l.mu.Unlock()
	if tr != nil {
		_ = tr.Close()
	}
}

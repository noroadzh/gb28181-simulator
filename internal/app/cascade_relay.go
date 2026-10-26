// Cascade message relay: the bookkeeping that lets one node hand a MANSCDP
// request to a cascade neighbour and carry the answer back to whoever asked.
//
// The relay is deliberately not a SIP proxy. It keeps no branch state and
// retransmits nothing; it remembers one thing per in-flight request — who is
// waiting and until when — so a downstream answer can be matched to its
// requester by the transaction identifiers SIP already carries.
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// relayTimeout bounds how long a relayed request waits for its downstream
// answer. When it lapses the node answers the requester itself (504) rather
// than leaving it to its own timeout: a silent hang is the one outcome the
// cascade-routing spec forbids.
const relayTimeout = 5 * time.Second

// relayKey is the transaction identity of a relayed request. The node is part
// of it because one Acceptor serves every node in the process, and a relayed
// request keeps its Call-ID/CSeq across hops: without the node, the second
// hop's hand-off would overwrite the first's.
type relayKey struct {
	node   string
	callID string
	cseq   string
}

// pendingRelay is one outstanding hand-off: who asked, what they asked, and
// when to give up.
type pendingRelay struct {
	peer     string
	request  model.Message
	deadline time.Time
}

// relayTable holds the outstanding hand-offs. It is safe for concurrent use:
// the read loop stores, the timeout goroutines remove.
type relayTable struct {
	mu      sync.Mutex
	pending map[relayKey]pendingRelay
	now     func() time.Time
}

func newRelayTable(now func() time.Time) *relayTable {
	if now == nil {
		now = time.Now
	}
	return &relayTable{pending: map[relayKey]pendingRelay{}, now: now}
}

// relayKeyOf extracts the transaction identity from a message. A message
// missing either header cannot be correlated and is not relayable.
func relayKeyOf(node model.NodeID, msg model.Message) (relayKey, bool) {
	callID, ok := msg.Header("Call-ID")
	if !ok {
		return relayKey{}, false
	}
	cseq, ok := msg.Header("CSeq")
	if !ok {
		return relayKey{}, false
	}
	return relayKey{
		node:   node.String(),
		callID: strings.TrimSpace(callID.Value()),
		cseq:   strings.TrimSpace(cseq.Value()),
	}, true
}

// put registers a request handed to a neighbour. A second request with the
// same transaction identity replaces the first: GB/T 28181 MESSAGEs use a
// fresh Call-ID per request, so a collision means a retransmission, and the
// newer waiter is the one still listening.
func (t *relayTable) put(node model.NodeID, req model.Message, peer string, timeout time.Duration) (relayKey, bool) {
	key, ok := relayKeyOf(node, req)
	if !ok {
		return relayKey{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[key] = pendingRelay{
		peer:     peer,
		request:  req,
		deadline: t.now().Add(timeout),
	}
	return key, true
}

// take removes and returns the hand-off a downstream answer belongs to. A
// second answer for the same transaction finds nothing: the first one already
// released the waiter.
func (t *relayTable) take(node model.NodeID, msg model.Message) (pendingRelay, bool) {
	key, ok := relayKeyOf(node, msg)
	if !ok {
		return pendingRelay{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.pending[key]
	if !ok {
		return pendingRelay{}, false
	}
	delete(t.pending, key)
	return entry, true
}

// takeKey removes the hand-off by key, for the timeout goroutine. It returns
// false when the answer already released it, which is how a request that just
// made it in time avoids a spurious 504.
func (t *relayTable) takeKey(key relayKey) (pendingRelay, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.pending[key]
	if !ok {
		return pendingRelay{}, false
	}
	delete(t.pending, key)
	return entry, true
}

// size reports how many hand-offs are outstanding. Tests use it to prove the
// table drains; production code has no reason to ask.
func (t *relayTable) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// forwardRequest prepares a request for the next hop: the original headers
// untouched, plus the cascade headers appended. Call-ID, CSeq, From/To and the
// body are preserved so the answer can be matched by the relay table.
//
// No Via is added here: the outbound adapter regenerates Via for every request
// it sends, so a model-level Via would never reach the wire. Delivery does not
// depend on it either — each node answers the UDP source address it saw.
func forwardRequest(msg model.Message, extra []model.Header) (model.Message, error) {
	if msg.StatusCode() != 0 {
		return model.Message{}, fmt.Errorf("app: relay expects a request, got a response")
	}
	if len(extra) == 0 {
		return msg, nil
	}
	hdrs := make([]model.Header, 0, len(msg.Headers())+len(extra))
	hdrs = append(hdrs, msg.Headers()...)
	hdrs = append(hdrs, extra...)
	return rebuildMessage(msg, hdrs)
}

// rebuildRelayResponse turns a downstream answer into the response the
// original requester can match: built from the request this node stored, so it
// carries that request's Via, Call-ID and CSeq, with the downstream status and
// body. Forwarding the downstream bytes instead would echo a Via the requester
// never sent.
func rebuildRelayResponse(entry pendingRelay, downstream model.Message) (model.Message, error) {
	var extra []model.Header
	if ct, ok := downstream.Header("Content-Type"); ok {
		extra = append(extra, ct)
	}
	return buildResponse(downstream.StatusCode(), downstream.StatusText(), entry.request, extra, downstream.Body())
}

// rebuildMessage returns msg with a different header list, preserving its
// kind, status, URI and body.
func rebuildMessage(msg model.Message, hdrs []model.Header) (model.Message, error) {
	if msg.StatusCode() != 0 {
		return model.NewResponse(msg.StatusCode(), msg.StatusText(), hdrs, msg.Body())
	}
	uri := ""
	if u := msg.URI(); u != nil {
		uri = u.String()
	}
	return model.NewRequest(msg.Method(), uri, hdrs, msg.Body())
}

// relayToChild hands an inbound MANSCDP request to the cascade neighbour the
// handler selects, and reports whether it took the message off the normal
// path. It returns false when this node should answer locally: no cascade
// wiring, an unreadable or self-addressed body, no hop, or a hop that is not
// one of this node's own children.
//
// Only a child counts as a target. The handler also returns the upstream
// parent when no child matches, and relaying there would hijack requests
// meant for devices this node serves itself.
func (a *Acceptor) relayToChild(ctx context.Context, p *platform, req model.Message, peer string) bool {
	if a.cascade == nil || a.registry == nil || a.relay == nil {
		return false
	}
	notify, err := a.manscdp.DecodeNotify(req.Body())
	if err != nil {
		return false
	}
	target := strings.TrimSpace(notify.DeviceID())
	if target == "" || target == p.id.String() {
		return false
	}

	next, extra, _, ferr := a.cascade.Forward(p.id, req, target)
	if ferr != nil {
		status, reason := 500, "Server Internal Error"
		if errors.Is(ferr, port.ErrCascadeLoop) {
			status, reason = 482, "Loop Detected"
		}
		a.answerRelayFailure(ctx, p, req, peer, status, reason, ferr)
		return true
	}
	if next == "" {
		return false
	}

	self, ok := a.registry.Get(ctx, p.id)
	if !ok || !slices.Contains(self.Profile().CascadeChildren(), next) {
		return false
	}
	nextID, err := model.ParseNodeID(next)
	if err != nil {
		a.answerRelayFailure(ctx, p, req, peer, 500, "Server Internal Error",
			fmt.Errorf("next hop %q is not a node id: %w", next, err))
		return true
	}
	child, ok := a.registry.Get(ctx, nextID)
	if !ok {
		a.answerRelayFailure(ctx, p, req, peer, 500, "Server Internal Error",
			fmt.Errorf("next hop %s is not a known node", next))
		return true
	}
	addr := strings.TrimSpace(child.Profile().Addr())
	if addr == "" {
		a.answerRelayFailure(ctx, p, req, peer, 500, "Server Internal Error",
			fmt.Errorf("next hop %s has no signalling address", next))
		return true
	}

	key, ok := a.relay.put(p.id, req, peer, a.relayWait)
	if !ok {
		a.answerRelayFailure(ctx, p, req, peer, 500, "Server Internal Error",
			errors.New("relayed request carries no Call-ID/CSeq"))
		return true
	}
	forwarded, err := forwardRequest(req, extra)
	if err != nil {
		a.relay.takeKey(key)
		a.answerRelayFailure(ctx, p, req, peer, 500, "Server Internal Error", err)
		return true
	}
	if err := p.tr.Send(ctx, forwarded, addr); err != nil {
		a.relay.takeKey(key)
		a.answerRelayFailure(ctx, p, req, peer, 500, "Server Internal Error",
			fmt.Errorf("send to %s: %w", addr, err))
		return true
	}
	a.log.Debug("cascade relay forwarded",
		"node_id", p.id.String(), "next", next, "dst", addr, "from", peer)
	a.armRelayTimeout(ctx, p, key)
	return true
}

// forwardRelayResponse hands a downstream answer back to whoever asked,
// without the Via this node added on the way down.
func (a *Acceptor) forwardRelayResponse(ctx context.Context, p *platform, resp model.Message, entry pendingRelay) {
	out, err := rebuildRelayResponse(entry, resp)
	if err != nil {
		a.log.Warn("cascade relay: cannot rebuild the answer",
			"node_id", p.id.String(), "peer", entry.peer, "error", err.Error())
		return
	}
	if err := p.tr.Send(ctx, out, entry.peer); err != nil {
		a.log.Warn("cascade relay: cannot return the downstream answer",
			"node_id", p.id.String(), "peer", entry.peer, "error", err.Error())
		return
	}
	a.log.Debug("cascade relay returned the answer",
		"node_id", p.id.String(), "peer", entry.peer, "status", out.StatusCode())
}

// armRelayTimeout ends an unanswered hand-off with a 504 for the requester.
// The goroutine exits with the node: a stopped node must not write to a
// closed transport.
func (a *Acceptor) armRelayTimeout(ctx context.Context, p *platform, key relayKey) {
	go func() {
		timer := time.NewTimer(a.relayWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		entry, ok := a.relay.takeKey(key)
		if !ok {
			return // answered in time, or the node went away
		}
		a.log.Warn("cascade relay timed out",
			"node_id", p.id.String(), "peer", entry.peer)
		a.answerRelayFailure(ctx, p, entry.request, entry.peer, 504, "Server Time-out",
			fmt.Errorf("no answer within %s", a.relayWait))
	}()
}

// answerRelayFailure logs why a hand-off was refused and tells the requester
// in SIP terms, so it never sits waiting for a reply that will not come.
func (a *Acceptor) answerRelayFailure(ctx context.Context, p *platform, req model.Message, peer string, status int, reason string, cause error) {
	a.log.Warn("cascade relay refused",
		"node_id", p.id.String(), "peer", peer,
		"status", status, "reason", reason, "error", cause.Error())
	resp, err := buildResponse(status, reason, req, nil, "")
	if err != nil {
		a.log.Warn("cascade relay: cannot build the failure response",
			"node_id", p.id.String(), "error", err.Error())
		return
	}
	if err := p.tr.Send(ctx, resp, peer); err != nil {
		a.log.Warn("cascade relay: cannot send the failure response",
			"node_id", p.id.String(), "peer", peer, "error", err.Error())
	}
}

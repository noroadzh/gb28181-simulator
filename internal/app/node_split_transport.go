// Package app — one socket, two halves.
package app

import (
	"context"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// half names one half of a two-halved node: the half that serves its own
// downstreams, or the half that is a subordinate of an upstream.
type half int

const (
	// halfServing wants what arrives unasked: registrations, keepalives,
	// catalogue queries.
	halfServing half = iota
	// halfUpstream wants the answers to what it sent: challenges, grants,
	// the reply to a keepalive.
	halfUpstream
)

// splitQueue is how many messages a half may have waiting for it. It is
// small on purpose: a half that lets messages pile up is a half that is not
// reading, and a socket that keeps taking them would only hide that.
const splitQueue = 32

// splitTransport reads one socket and hands each message to the half that
// can use it.
//
// A node that is both halves of a cascade — a platform-small — serves and
// registers over the same listener. But the socket gives each datagram to
// whoever reads first, so the serving half, which reads all the time, would
// take the answers the registering half is waiting for and drop them: the
// registration would never finish. The splitter reads the socket once, in
// one place, and sorts what it reads: requests go to the serving half,
// responses to the registering one. Both halves still send over the same
// socket, so the peer sees one node on one address, as it must.
type splitTransport struct {
	tr port.SIPTransport

	cancel context.CancelFunc
	done   chan struct{}

	uas chan arrival
	uac chan arrival
}

// arrival is one message and where it came from.
type arrival struct {
	msg  model.Message
	peer string
}

// newSplitTransport starts sorting tr into the two halves. The halves are
// read through Serving() and Upstream(); the splitter itself is ended by
// Close, which every path that ends a node must call.
func newSplitTransport(ctx context.Context, tr port.SIPTransport) *splitTransport {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s := &splitTransport{
		tr:     tr,
		cancel: cancel,
		done:   make(chan struct{}),
		uas:    make(chan arrival, splitQueue),
		uac:    make(chan arrival, splitQueue),
	}
	go s.run(runCtx)
	return s
}

// run is the one reader. It ends when the socket or the context ends, and
// every message it reads goes to exactly one half.
func (s *splitTransport) run(ctx context.Context) {
	defer close(s.done)
	for {
		msg, peer, err := s.tr.Receive(ctx)
		if err != nil {
			return // the socket closed, or the node was stopped
		}
		a := arrival{msg: msg, peer: peer}
		queue := s.uac
		if msg.IsRequest() {
			queue = s.uas
		}
		select {
		case queue <- a:
		default:
			// The half that should have taken this is not reading:
			// a request nobody serves, or an answer to something
			// nobody sent. Dropping it is better than blocking the
			// other half behind it.
		}
	}
}

// Serving is the socket as the serving half sees it: it receives the
// requests and sends over the shared listener.
func (s *splitTransport) Serving() port.SIPTransport { return splitHalf{split: s, half: halfServing} }

// Upstream is the socket as the registering half sees it: it receives the
// answers and sends over the shared listener.
func (s *splitTransport) Upstream() port.SIPTransport { return splitHalf{split: s, half: halfUpstream} }

// Close ends the sorting and waits for the reader to stop. It does not close
// the socket itself — that belongs to the lifecycle that bound it.
//
// The two queues are closed once the reader has stopped, so a half that is
// still waiting is released instead of waiting forever for a message that
// can no longer arrive.
func (s *splitTransport) Close() error {
	s.cancel()
	<-s.done
	close(s.uas)
	close(s.uac)
	return nil
}

// splitHalf is one half's view of the shared socket.
type splitHalf struct {
	split *splitTransport
	half  half
}

// Send goes straight out over the shared socket: a node has one address, and
// both halves speak from it.
func (h splitHalf) Send(ctx context.Context, msg model.Message, dst string) error {
	return h.split.tr.Send(ctx, msg, dst)
}

// Receive waits for a message addressed to this half — a request for the
// serving half, an answer for the registering one.
func (h splitHalf) Receive(ctx context.Context) (model.Message, string, error) {
	queue := h.split.uac
	if h.half == halfServing {
		queue = h.split.uas
	}
	select {
	case <-ctx.Done():
		return model.Message{}, "", ctx.Err()
	case a, ok := <-queue:
		if !ok {
			return model.Message{}, "", context.Canceled
		}
		return a.msg, a.peer, nil
	}
}

// Close does not close the shared socket: the half did not bind it, and the
// other half may still be using it. Ending the splitter is Close on the
// splitTransport itself.
func (h splitHalf) Close() error { return nil }

// splitters keeps the splitter of every node that has one, so a node that is
// stopped — or faulted — takes its reader with it and nothing is left
// reading a released socket.
type splitters struct {
	mu      sync.Mutex
	byNode  map[string]*splitTransport
	factory func(port.SIPTransport) *splitTransport
}

func newSplitters() *splitters {
	return &splitters{byNode: make(map[string]*splitTransport)}
}

// forNode returns the node's splitter, making one from tr the first time it
// is asked. A second call for the same node returns the same splitter: two
// halves, one socket, one reader.
func (s *splitters) forNode(id model.NodeID, tr port.SIPTransport) *splitTransport {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sp, ok := s.byNode[id.String()]; ok {
		return sp
	}
	factory := s.factory
	if factory == nil {
		factory = func(socket port.SIPTransport) *splitTransport {
			return newSplitTransport(context.Background(), socket)
		}
	}
	sp := factory(tr)
	s.byNode[id.String()] = sp
	return sp
}

// lookup returns the node's splitter without making one: a caller that only
// wants to know whether the node is sorting its socket must not create a
// reader as a side effect.
func (s *splitters) lookup(id model.NodeID) (*splitTransport, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.byNode[id.String()]
	return sp, ok
}

// end closes and forgets the node's splitter. It is safe to call for a node
// that never had one.
func (s *splitters) end(id model.NodeID) {
	s.mu.Lock()
	sp, ok := s.byNode[id.String()]
	delete(s.byNode, id.String())
	s.mu.Unlock()
	if !ok {
		return
	}
	_ = sp.Close()
}

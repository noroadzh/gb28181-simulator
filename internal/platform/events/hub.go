// Package events provides a small in-process publish/subscribe bus for
// domain events that are not log records.
//
// Why this is not logging.Hub: the logging hub fans out *slog.Record bytes to
// the dashboard's log stream, where the consumer decodes them as structured
// log lines. A node status change is a domain fact, not a diagnostic: it has a
// stable wire shape ({type, id, status, at}) that the WebUI binds to directly
// and that must not change when someone rewords a log message. Squeezing it
// through the logging hub would make the UI's event schema depend on log
// formatting, and would make "log level = off" swallow state transitions.
//
// The Hub keeps logging.Hub's delivery contract — non-blocking producer,
// bounded per-subscriber buffer, drop-on-slow-consumer — because a stalled
// browser tab must never wedge the SIP state machine that publishes to it.
package events

import (
	"sync"
	"time"
)

// Event is one published domain fact. Type discriminates the payload; the
// three scalar fields are present on every event so a subscriber can route
// and order without inspecting a type-specific body.
type Event struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	// Status is the node's new state, empty for events that carry no status.
	Status string `json:"status,omitempty"`
	// At is when the change was observed, in RFC 3339 with nanoseconds.
	At time.Time `json:"at"`
}

// Event types published by the runtime.
const (
	// TypeNodeStatusChanged accompanies every node status transition.
	TypeNodeStatusChanged = "node_status_changed"
	// TypeNodeCreated accompanies a successful create of a node
	// definition, whether through the HTTP API or the boot path. ID
	// carries the new node id; Status is the post-create status, which
	// is always "idle".
	TypeNodeCreated = "node_created"
	// TypeNodeUpdated accompanies a successful update of a node
	// definition. ID is the updated node id; Status is the post-update
	// status.
	TypeNodeUpdated = "node_updated"
	// TypeNodeDeleted accompanies a successful delete of a node
	// definition. ID is the deleted node id; Status is the
	// pre-delete status (always idle/offline/fault).
	TypeNodeDeleted = "node_deleted"
)

// Subscriber is a bounded channel that consumes events emitted by the Hub.
// The subscriber must never close its channel — the Hub owns its lifecycle.
type Subscriber struct {
	id      uint64
	channel chan Event
}

// Chan returns the receive end of the subscription.
func (s *Subscriber) Chan() <-chan Event { return s.channel }

// ID returns the stable identifier of the subscription.
func (s *Subscriber) ID() uint64 { return s.id }

// Hub is a non-blocking fan-out for domain events. Every publish is offered
// to all live subscribers; a subscriber whose buffer is full drops the event
// rather than stalling the publisher.
type Hub struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]*Subscriber
	bufferSize  int
	closed      bool
}

// NewHub builds a Hub with the given per-subscriber buffer size. The size
// bounds how far a slow subscriber may lag before it starts losing events;
// Publish never blocks regardless of this value.
func NewHub(bufferSize int) *Hub {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &Hub{
		subscribers: make(map[uint64]*Subscriber),
		bufferSize:  bufferSize,
	}
}

// Subscribe registers a new subscriber and returns its handle. Subscribing to
// a closed Hub returns a subscriber with an already-closed channel, so a
// handler can select on it and exit without a second error path.
func (h *Hub) Subscribe() *Subscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		ch := make(chan Event)
		close(ch)
		return &Subscriber{id: 0, channel: ch}
	}
	h.nextID++
	sub := &Subscriber{
		id:      h.nextID,
		channel: make(chan Event, h.bufferSize),
	}
	h.subscribers[sub.id] = sub
	return sub
}

// Unsubscribe drops a subscription and closes its channel. It is safe to call
// twice for the same subscriber.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	if sub == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subscribers[sub.id]; !ok {
		return
	}
	delete(h.subscribers, sub.id)
	close(sub.channel)
}

// Publish offers ev to every live subscriber. It never blocks: a subscriber
// that is not draining fast enough loses the event rather than holding up the
// caller, which in this codebase is a SIP state machine.
func (h *Hub) Publish(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.subscribers {
		select {
		case sub.channel <- ev:
		default:
			// Slow consumer: drop rather than block the producer.
		}
	}
}

// Close shuts the Hub down and closes every subscriber channel. Publish after
// Close is a no-op.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, sub := range h.subscribers {
		delete(h.subscribers, id)
		close(sub.channel)
	}
}

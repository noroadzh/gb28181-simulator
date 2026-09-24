package logging

import (
	"sync"
)

// Subscriber is a bounded channel that consumes record bytes emitted by the
// Hub. Subscribers are responsible for decoding the JSON-encoded record and
// must never close the channel — the Hub owns its lifecycle.
type Subscriber struct {
	id      uint64
	channel chan []byte
}

// Chan returns the receive end of the subscription.
func (s *Subscriber) Chan() <-chan []byte { return s.channel }

// ID returns the stable identifier of the subscription.
func (s *Subscriber) ID() uint64 { return s.id }

// Hub is a non-blocking fan-out for log records. Records are encoded once and
// forwarded to all live subscribers; slow consumers are skipped instead of
// blocking the producer.
type Hub struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]*Subscriber
	bufferSize  int
	closed      bool
}

// NewHub builds a Hub with the given per-subscriber buffer size. The size
// controls how many records a slow subscriber can lag behind before messages
// are dropped on the floor (Hub does not block producers).
func NewHub(bufferSize int) *Hub {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &Hub{
		subscribers: make(map[uint64]*Subscriber),
		bufferSize:  bufferSize,
	}
}

// Subscribe registers a new subscriber and returns its handle.
func (h *Hub) Subscribe() *Subscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		// After shutdown, hand out a doomed subscriber that can be safely
		// drained; new producers should not publish anyway because Init
		// already replaced the global handler.
		ch := make(chan []byte)
		close(ch)
		return &Subscriber{id: 0, channel: ch}
	}
	h.nextID++
	sub := &Subscriber{
		id:      h.nextID,
		channel: make(chan []byte, h.bufferSize),
	}
	h.subscribers[sub.id] = sub
	return sub
}

// Unsubscribe removes a subscriber and closes its channel.
func (h *Hub) Unsubscribe(s *Subscriber) {
	if s == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subscribers[s.id]; ok {
		delete(h.subscribers, s.id)
		// Drain remaining buffered records to allow GC, then close.
		close(s.channel)
	}
}

// Publish fan-outs encoded record bytes to every subscriber. Subscribers with
// full buffers are skipped to keep the hub non-blocking; we never drop
// records from the file sink at this layer.
func (h *Hub) Publish(payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, s := range h.subscribers {
		select {
		case s.channel <- payload:
		default:
			// Slow consumer; drop and continue.
		}
	}
}

// Count returns the number of live subscribers (used by tests).
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// Close shuts the hub. After Close, new subscriptions return drained channels
// and Publish becomes a no-op.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for _, s := range h.subscribers {
		close(s.channel)
	}
	h.subscribers = make(map[uint64]*Subscriber)
	h.closed = true
}

package events

import (
	"sync"
	"testing"
	"time"
)

func TestHubPublishFanout(t *testing.T) {
	h := NewHub(16)
	defer h.Close()

	a := h.Subscribe()
	b := h.Subscribe()
	ev := Event{Type: TypeNodeStatusChanged, ID: "n1", Status: "online", At: time.Now()}

	h.Publish(ev)

	for name, sub := range map[string]*Subscriber{"a": a, "b": b} {
		got := <-sub.Chan()
		if got != ev {
			t.Fatalf("%s: got %+v, want %+v", name, got, ev)
		}
	}
}

func TestHubDropOnSlowConsumer(t *testing.T) {
	h := NewHub(1)
	defer h.Close()

	sub := h.Subscribe()
	// Fill the buffer, then keep publishing: the producer must never block
	// and the subscriber must lose the overflow rather than the call hang.
	h.Publish(Event{Type: "x", ID: "1"})
	h.Publish(Event{Type: "x", ID: "2"})
	done := make(chan struct{})
	go func() {
		h.Publish(Event{Type: "x", ID: "3"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}
	if got := <-sub.Chan(); got.ID != "1" {
		t.Fatalf("first buffered event = %q, want 1", got.ID)
	}
}

func TestHubUnsubscribeClosesChannel(t *testing.T) {
	h := NewHub(4)
	sub := h.Subscribe()
	h.Unsubscribe(sub)
	if _, open := <-sub.Chan(); open {
		t.Fatal("channel still open after Unsubscribe")
	}
	h.Unsubscribe(sub) // must not panic
}

func TestHubCloseIdempotent(t *testing.T) {
	h := NewHub(4)
	sub := h.Subscribe()
	h.Close()
	h.Close()
	if _, open := <-sub.Chan(); open {
		t.Fatal("channel still open after Close")
	}
	// Post-close Subscribe hands out a closed channel, and Publish must not
	// panic on the (now empty) subscriber set.
	doomed := h.Subscribe()
	if _, open := <-doomed.Chan(); open {
		t.Fatal("post-close subscriber should read a closed channel")
	}
	h.Publish(Event{Type: "x", ID: "1"})
}

func TestHubConcurrentPublishSubscribe(t *testing.T) {
	h := NewHub(64)
	defer h.Close()

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			sub := h.Subscribe()
			go func() {
				for range sub.Chan() {
				} // drain
			}()
			for range 100 {
				h.Publish(Event{Type: "x", ID: "n"})
			}
		})
	}
	wg.Wait()
}

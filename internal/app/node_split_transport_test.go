package app

import (
	"context"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// splitSource is a socket whose messages are queued up front, so a test can
// say which half each one is meant for.
func splitSource(t *testing.T, in []incoming) *scriptedTransport {
	t.Helper()
	return &scriptedTransport{in: in}
}

func request(t *testing.T, method string) model.Message {
	t.Helper()
	return requestWithCallID(t, method, "split-call")
}

func requestWithCallID(t *testing.T, method, callID string) model.Message {
	t.Helper()
	msg, err := model.NewRequest(method, "sip:34020000002160000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000011310000001@3402000000>;tag=split1"),
		model.NewHeader("To", "<sip:34020000002160000001@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 "+method),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// The serving half must not be able to take what the registering half is
// waiting for: a request goes one way, an answer the other.
func TestSplitTransportSortsByDirection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	src := splitSource(t, []incoming{
		{msg: request(t, "REGISTER"), peer: testServer},
		{msg: response(t, 200, callIDHeader("split-call")), peer: testServer},
	})
	split := newSplitTransport(ctx, src)
	defer func() { _ = split.Close() }()

	// Whichever order the halves read in, each gets its own message.
	upstream := split.Upstream()
	serving := split.Serving()

	gotReq, peer, err := serving.Receive(ctx)
	if err != nil {
		t.Fatalf("serving Receive: %v", err)
	}
	if gotReq.Method() != "REGISTER" {
		t.Errorf("serving half got %q, want the REGISTER", gotReq.Method())
	}
	if peer != testServer {
		t.Errorf("peer = %q, want %q", peer, testServer)
	}

	gotResp, _, err := upstream.Receive(ctx)
	if err != nil {
		t.Fatalf("upstream Receive: %v", err)
	}
	if gotResp.StatusCode() != 200 {
		t.Errorf("upstream half got status %d, want the answer", gotResp.StatusCode())
	}
}

// Both halves send over the same socket: a node has one address, and the
// peer must not see two.
func TestSplitTransportSharesOneSocket(t *testing.T) {
	t.Parallel()
	src := &scriptedTransport{}
	split := newSplitTransport(context.Background(), src)
	defer func() { _ = split.Close() }()

	if err := split.Serving().Send(context.Background(), request(t, "MESSAGE"), testServer); err != nil {
		t.Fatalf("serving Send: %v", err)
	}
	if err := split.Upstream().Send(context.Background(), request(t, "REGISTER"), testServer); err != nil {
		t.Fatalf("upstream Send: %v", err)
	}
	sent := src.messages()
	if len(sent) != 2 {
		t.Fatalf("the socket carried %d messages, want 2", len(sent))
	}
	if sent[0].msg.Method() != "MESSAGE" || sent[1].msg.Method() != "REGISTER" {
		t.Errorf("sent = %q, %q", sent[0].msg.Method(), sent[1].msg.Method())
	}
}

// Ending the splitter ends its reader: no goroutine is left holding a
// listener the node has already given back.
func TestSplitTransportCloseEndsTheReader(t *testing.T) {
	t.Parallel()
	src := &scriptedTransport{}
	split := newSplitTransport(context.Background(), src)
	serving := split.Serving()

	done := make(chan error, 1)
	go func() {
		_, _, err := serving.Receive(context.Background())
		done <- err
	}()

	if err := split.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("Receive returned without an error after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Receive is still waiting after Close: the reader outlived the splitter")
	}
}

// A half that is not reading must not jam the other one: messages nobody
// wants are dropped rather than queued in front of the ones somebody does.
func TestSplitTransportDropsWhatNobodyReads(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	in := make([]incoming, 0, splitQueue+8)
	for i := 0; i < splitQueue+8; i++ {
		in = append(in, incoming{msg: request(t, "MESSAGE"), peer: testServer})
	}
	in = append(in, incoming{msg: response(t, 200, callIDHeader("split-call")), peer: testServer})
	split := newSplitTransport(ctx, splitSource(t, in))
	defer func() { _ = split.Close() }()

	// Nobody reads the serving half, but the answer still gets through.
	got, _, err := split.Upstream().Receive(ctx)
	if err != nil {
		t.Fatalf("upstream Receive: %v", err)
	}
	if got.StatusCode() != 200 {
		t.Errorf("status = %d, want the answer", got.StatusCode())
	}
}

// The service keeps one splitter per node and takes it away when the node is
// stopped, so a second start does not leave two readers on one socket.
func TestSplittersOnePerNode(t *testing.T) {
	t.Parallel()
	s := newSplitters()
	id := mustParse(t, testPlatformSmall)
	tr := &scriptedTransport{}

	first := s.forNode(id, tr)
	if first != s.forNode(id, tr) {
		t.Error("a second call made a second splitter for one node")
	}
	if _, ok := s.lookup(mustParse(t, testPlatformSmallFailing)); ok {
		t.Error("lookup reports a splitter for a node that has none")
	}
	if got, ok := s.lookup(id); !ok || got != first {
		t.Error("lookup did not return the node's splitter")
	}
	s.end(id)
	if _, ok := s.lookup(id); ok {
		t.Error("the splitter outlived the node it belonged to")
	}
	// Ending a node that never had one is not an error.
	s.end(mustParse(t, testPlatformSmallFailing))
}

// The halves are ordinary transports as far as the rest of the app is
// concerned: what matters is that the splitter owns the socket, not them.
func TestSplitHalfCloseLeavesTheSocket(t *testing.T) {
	t.Parallel()
	src := &scriptedTransport{}
	split := newSplitTransport(context.Background(), src)
	defer func() { _ = split.Close() }()

	var serving port.SIPTransport = split.Serving()
	if err := serving.Close(); err != nil {
		t.Errorf("closing a half = %v, want it to leave the socket alone", err)
	}
	if err := split.Upstream().Send(context.Background(), request(t, "REGISTER"), testServer); err != nil {
		t.Errorf("the socket is unusable after a half was closed: %v", err)
	}
}

// Responses whose Call-ID matches a registered transaction handler are
// delivered to that handler rather than the default direction-based half.
func TestSplitTransportTransactionHandlerOverridesDefaultRouting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callID := "txn-123"
	// Start with an empty source so the handler is registered before any
	// message is processed.
	src := &scriptedTransport{}
	split := newSplitTransport(ctx, src)
	defer func() { _ = split.Close() }()

	// Register a handler before any message arrives.
	got := make(chan model.Message, 1)
	split.RegisterHandler(callID, TransactionHandler{
		Half: halfUpstream,
		OnMessage: func(msg model.Message, peer string) {
			got <- msg
		},
	})

	// Inject the response after the handler is registered.
	src.mu.Lock()
	src.in = append(src.in, incoming{msg: response(t, 200, callIDHeader(callID)), peer: testServer})
	src.mu.Unlock()

	select {
	case msg := <-got:
		if msg.StatusCode() != 200 {
			t.Errorf("status = %d, want 200", msg.StatusCode())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not receive the matched response")
	}
}

// Unregistered responses fall back to direction-based routing.
func TestSplitTransportTransactionHandlerFallbackToDefaultRouting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	src := splitSource(t, []incoming{
		{msg: response(t, 200, callIDHeader("no-handler")), peer: testServer},
	})
	split := newSplitTransport(ctx, src)
	defer func() { _ = split.Close() }()

	got, _, err := split.Upstream().Receive(ctx)
	if err != nil {
		t.Fatalf("upstream Receive: %v", err)
	}
	if got.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", got.StatusCode())
	}
}

// INVITE requests automatically register a default handler so that later
// responses for the same Call-ID reach the serving half.
func TestSplitTransportInviteAutoRegistersHandler(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callID := "invite-auto"
	src := splitSource(t, []incoming{
		{msg: requestWithCallID(t, "INVITE", callID), peer: testServer},
		{msg: response(t, 200, callIDHeader(callID)), peer: testServer},
	})
	split := newSplitTransport(ctx, src)
	defer func() { _ = split.Close() }()

	// The serving half must receive the INVITE request.
	serving := split.Serving()
	msg, _, err := serving.Receive(ctx)
	if err != nil {
		t.Fatalf("serving Receive (INVITE): %v", err)
	}
	if msg.Method() != "INVITE" {
		t.Errorf("method = %q, want INVITE", msg.Method())
	}

	// The auto-registered handler routes the response to the serving half.
	msg, _, err = serving.Receive(ctx)
	if err != nil {
		t.Fatalf("serving Receive (response): %v", err)
	}
	if msg.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", msg.StatusCode())
	}
}

// Unregistering a handler removes the override; subsequent responses fall
// back to direction-based routing.
func TestSplitTransportUnregisterHandlerFallsBack(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callID := "unreg-txn"
	src := splitSource(t, []incoming{
		{msg: response(t, 200, callIDHeader(callID)), peer: testServer},
	})
	split := newSplitTransport(ctx, src)
	defer func() { _ = split.Close() }()

	split.RegisterHandler(callID, TransactionHandler{
		Half: halfUpstream,
		OnMessage: func(msg model.Message, peer string) {
			// Intentionally empty: we unregister before the response arrives.
		},
	})
	split.UnregisterHandler(callID)

	got, _, err := split.Upstream().Receive(ctx)
	if err != nil {
		t.Fatalf("upstream Receive: %v", err)
	}
	if got.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", got.StatusCode())
	}
}

// SUBSCRIBE requests also auto-register a transaction handler.
func TestSplitTransportSubscribeAutoRegistersHandler(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callID := "sub-auto"
	src := splitSource(t, []incoming{
		{msg: requestWithCallID(t, "SUBSCRIBE", callID), peer: testServer},
		{msg: response(t, 200, callIDHeader(callID)), peer: testServer},
	})
	split := newSplitTransport(ctx, src)
	defer func() { _ = split.Close() }()

	serving := split.Serving()
	sub, _, err := serving.Receive(ctx)
	if err != nil {
		t.Fatalf("serving Receive: %v", err)
	}
	if sub.Method() != "SUBSCRIBE" {
		t.Errorf("method = %q, want SUBSCRIBE", sub.Method())
	}

	resp, _, err := serving.Receive(ctx)
	if err != nil {
		t.Fatalf("serving did not receive the response via auto-registered handler: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode())
	}
}

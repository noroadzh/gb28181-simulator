package port

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// fakeTransport is a hand-rolled stub that records the messages sent and
// returns the messages supplied via the constructor on Receive. It
// implements SIPTransport so it doubles as a compile-time check.
type fakeTransport struct {
	sent    []model.Message
	receive []model.Message
	rcvIdx  int
	closed  bool
}

func (f *fakeTransport) Send(_ context.Context, msg model.Message) error {
	if f.closed {
		return errors.New("closed")
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeTransport) Receive(ctx context.Context) (model.Message, error) {
	if f.rcvIdx >= len(f.receive) {
		<-ctx.Done()
		return model.Message{}, ctx.Err()
	}
	m := f.receive[f.rcvIdx]
	f.rcvIdx++
	return m, nil
}

func (f *fakeTransport) Close() error { f.closed = true; return nil }

// Compile-time check that fakeTransport satisfies SIPTransport.
var _ SIPTransport = (*fakeTransport)(nil)

// TestSIPTransport_Contract verifies the contract through a hand-rolled
// stub. A custom fake is preferred over an auto-generated mock because it
// doubles as documentation of how the port is expected to be used.
func TestSIPTransport_Contract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	ft := &fakeTransport{
		receive: []model.Message{
			mustResp(t, 200, "OK"),
		},
	}
	req, err := model.NewRequest("REGISTER", "sip:x@y", nil, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := ft.Send(ctx, req); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := ft.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.StatusCode() != 200 {
		t.Errorf("Receive() status = %d, want 200", got.StatusCode())
	}
	if err := ft.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if !ft.closed {
		t.Error("Close did not set closed=true")
	}
}

// mustResp builds a response Message for tests; errors are fatal.
func mustResp(t *testing.T, code int, text string) model.Message {
	t.Helper()
	m, err := model.NewResponse(code, text, nil, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	return m
}
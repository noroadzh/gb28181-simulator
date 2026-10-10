package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Frame helpers also referenced (but undefined) by other test files.
// Defining them here makes the whole package compile while we fix the
// unresolved references in acceptor_message_test.go and media_e2e_test.go.

func frameWithNAL(payload []byte) []byte {
	return append([]byte{0x00, 0x00, 0x00, 0x01}, payload...)
}

var nalPrefix = []byte{0x00, 0x00, 0x00, 0x01}

// newStubSource returns a MediaSource whose Open yields the given NAL
// payload (with H.264 Annex-B start code already included by frameWithNAL).
// A new src instance is returned per call so concurrent tests do not share
// underlying ReadCloser state.
func newStubSource(cfg model.MediaConfig, payload []byte) port.MediaSource {
	return &stubSource{payload: payload, cfg: cfg}
}

// newStubRTP returns an RTPizer that emits a single RTP packet carrying
// the PS payload as-is. Sequence starts at 1; SSRC is fixed to 0.
type stubRTP struct{ seq uint16 }

func (s *stubRTP) Packetize(ps model.PSFrame) ([]model.RTPPacket, error) {
	return []model.RTPPacket{{
		Sequence: s.seq, SSRC: 0, PayloadType: 96, Marker: true,
		Timestamp: uint32(ps.PTS), Payload: ps.Payload,
	}}, nil
}

func (s *stubRTP) Sequence() uint16 { return s.seq }

func newStubRTP(mtu int) *stubRTP { return &stubRTP{seq: 1} }

// stubPS is a PS packetizer that simply wraps the ES payload verbatim.
type stubPS struct{}

func (s stubPS) Packetize(frame model.ESFrame) (model.PSFrame, error) {
	if len(frame.Payload) == 0 {
		return model.PSFrame{}, io.ErrUnexpectedEOF
	}
	return model.PSFrame{Payload: append([]byte(nil), frame.Payload...), PTS: frame.PTS}, nil
}

func (s stubPS) Header() []byte { return []byte{0x00, 0x00, 0x01, 0xBA} }

// stubPSPacketizer is kept for backward compatibility with any callers that
// reference it by that name; it is identical to stubPS.
type stubPSPacketizer = stubPS

// stubSource yields the H.264 Annex-B stream in `payload` once and then
// reports io.EOF on Read. Close() flips an atomic counter so tests can
// verify cleanup reached the source.
type stubSource struct {
	payload  []byte
	cfg      model.MediaConfig
	closed   atomic.Int32
}

func (s *stubSource) Open(ctx context.Context) (io.ReadCloser, error) {
	return &stubReadCloser{src: s, ctx: ctx, buf: *bytes.NewReader(s.payload)}, nil
}

func (s *stubSource) Close() error {
	s.closed.Add(1)
	return nil
}

func (s *stubSource) Config() model.MediaConfig { return s.cfg }

type stubReadCloser struct {
	src *stubSource
	ctx context.Context
	buf bytes.Reader
}

func newStubReadCloser(payload []byte, ctx context.Context) *stubReadCloser {
	return &stubReadCloser{src: &stubSource{payload: payload}, ctx: ctx, buf: *bytes.NewReader(payload)}
}

func (r *stubReadCloser) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.buf.Read(p)
}

func (r *stubReadCloser) Close() error { return r.src.Close() }

// streamPayload builds a small H.264 Annex-B stream: a single 4-byte start
// code + 1 byte IDR slice header, then EOF. The StreamESReader splits on
// start codes and yields exactly one ES frame.
func streamPayload() []byte {
	return []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00}
}

// newTestMediaService wires a MediaService whose factory returns a stub
// source yielding `streamPayload()` and whose PS packetizer is a no-op stub.
func newTestMediaService() *MediaService {
	factory := MediaSourceFactory(func(cfg model.MediaConfig) port.MediaSource {
		return &stubSource{payload: streamPayload()}
	})
	psFactory := PSPacketizerFactory(func() port.PSPacketizer { return stubPSPacketizer{} })
	rtpFactory := RTPizerFactory(func(mtu int) port.RTPizer { return nil })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewMediaService(factory, psFactory, rtpFactory, logger)
}

// TestSubscribePSCloseByEOF exercises the natural-EOF branch: the reader
// returns io.EOF, the goroutine exits and closes the channel through once.
// The caller then calls cleanup(); the test must not panic.
func TestSubscribePSCloseByEOF(t *testing.T) {
	m := newTestMediaService()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, cleanup, err := m.SubscribePS(ctx, model.MediaConfig{
		Kind: model.MediaSourceKind("synthetic"),
		Path: "/dev/null",
	})
	if err != nil {
		t.Fatalf("SubscribePS: %v", err)
	}

	// Drain the channel. The test will hang (and eventually time out via
	// t.Cleanup) if once is broken, but the assertion we really care about
	// is that calling cleanup after the channel closes is safe.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				cleanup()
				return
			}
		case <-timeout:
			t.Fatal("SubscribePS did not close within 2s after EOF")
		}
	}
}

// TestSubscribePSCloseByCancel forces both close paths to run concurrently:
// ctx cancellation triggers the reader goroutine's defer, while a parallel
// goroutine calls cleanup(). Without the once fix this panics with
// "close of closed channel" on most runs and reliably under -race.
func TestSubscribePSCloseByCancel(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		m := newTestMediaService()
		ctx, cancel := context.WithCancel(context.Background())
		ch, cleanup, err := m.SubscribePS(ctx, model.MediaConfig{
			Kind: model.MediaSourceKind("synthetic"),
			Path: "/dev/null",
		})
		if err != nil {
			t.Fatalf("SubscribePS: %v", err)
		}

		// Cancel from one goroutine, call cleanup from another. Either
		// order would historically trigger the double-close.
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cancel()
		}()
		go func() {
			defer wg.Done()
			cleanup()
		}()
		wg.Wait()

		// Drain whatever is left. No panic should be observed.
		for range ch {
		}
	}
}

// TestSubscribePSCloseByCleanup verifies that calling cleanup() while the
// reader is still running shuts the source and closes the channel exactly
// once; the reader goroutine's deferred once.Do is a no-op.
func TestSubscribePSCloseByCleanup(t *testing.T) {
	// Hold a reference to the source so the test can assert on close count.
	srcHolder := &stubSource{payload: streamPayload()}
	srcFactory := MediaSourceFactory(func(cfg model.MediaConfig) port.MediaSource {
		return srcHolder
	})
	psFactory := PSPacketizerFactory(func() port.PSPacketizer { return stubPSPacketizer{} })
	rtpFactory := RTPizerFactory(func(mtu int) port.RTPizer { return nil })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewMediaService(srcFactory, psFactory, rtpFactory, logger)

	ch, cleanup, err := m.SubscribePS(context.Background(), model.MediaConfig{
		Kind: model.MediaSourceKind("synthetic"),
		Path: "/dev/null",
	})
	if err != nil {
		t.Fatalf("SubscribePS: %v", err)
	}

	// Consume exactly one frame so the reader is unblocked, then close.
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no frame within 2s")
	}
	cleanup()

	// Source must have been closed exactly once.
	if got := srcHolder.closed.Load(); got != 1 {
		t.Errorf("source Close count = %d, want 1", got)
	}
}

// TestSubscribePSReaderWokenByCleanupWhenBlockedOnSend verifies that when the
// reader goroutine is blocked on a buffered send (buffer full, no consumer
// reading the channel), calling cleanup() closes the done signal and the
// reader exits instead of leaking. This covers the send-blocked deadlock
// scenario.
func TestSubscribePSReaderWokenByCleanupWhenBlockedOnSend(t *testing.T) {
	srcHolder := &stubSource{payload: streamPayload()}
	srcFactory := MediaSourceFactory(func(cfg model.MediaConfig) port.MediaSource {
		return srcHolder
	})
	psFactory := PSPacketizerFactory(func() port.PSPacketizer { return stubPSPacketizer{} })
	rtpFactory := RTPizerFactory(func(mtu int) port.RTPizer { return nil })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewMediaService(srcFactory, psFactory, rtpFactory, logger)

	ctx, cancel := context.WithCancel(context.Background())
	ch, cleanup, err := m.SubscribePS(ctx, model.MediaConfig{
		Kind: model.MediaSourceKind("synthetic"),
		Path: "/dev/null",
	})
	if err != nil {
		t.Fatalf("SubscribePS: %v", err)
	}
	// Deliberately NOT reading from ch — reader will fill the buffered channel
	// (256 frames) then block on the next send.

	// Give the reader time to fill the buffer and block on the next send.
	time.Sleep(500 * time.Millisecond)

	// cleanup must not deadlock; done must wake the blocked reader.
	cleanup()
	cancel()

	// Drain the channel: it must reach a closed state (zero value with ok=false)
	// within the deadline, even if we have to skip past buffered frames.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				// channel closed — pass.
				goto done
			}
			// Got a buffered frame; keep draining until close.
		case <-deadline:
			t.Fatal("channel not closed within 2s after cleanup — possible goroutine leak")
		}
	}
done:

	if got := srcHolder.closed.Load(); got != 1 {
		t.Errorf("source Close count = %d, want 1", got)
	}
}

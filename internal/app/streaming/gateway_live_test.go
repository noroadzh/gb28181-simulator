package streaming

// Task 3.2: the FLV gateway prefers an active live pull, falls back to the
// local media source, and honours ?source=.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

type fakeLiveSource struct {
	frames   []model.PSFrame
	absent   bool
	called   int
	cleanups int
}

func (f *fakeLiveSource) LivePS(_ context.Context, _ model.NodeID, _ string) (<-chan model.PSFrame, func(), bool) {
	if f.absent {
		return nil, nil, false
	}
	f.called++
	ch := make(chan model.PSFrame, len(f.frames))
	for _, fr := range f.frames {
		ch <- fr
	}
	close(ch)
	return ch, func() { f.cleanups++ }, true
}

type countingMedia struct {
	inner fakePSMediaSupplier
	calls int
}

func (c *countingMedia) SubscribePS(ctx context.Context, nodeID model.NodeID, channelID string) (<-chan model.PSFrame, error) {
	c.calls++
	return c.inner.SubscribePS(ctx, nodeID, channelID)
}

func onePSFrame(t *testing.T) model.PSFrame {
	t.Helper()
	ps, err := media.NewPSPacketizer(nil).Packetize(model.NewESFrame(
		append([]byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42}, bytes.Repeat([]byte{0xAA}, 8)...), 90000))
	if err != nil {
		t.Fatalf("Packetize: %v", err)
	}
	return ps
}

func liveTestNode(t *testing.T) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID("34020000011310000001")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	return id
}

// drainHeader reads until the channel closes, returning the chunks seen.
func drainChunks(t *testing.T, out <-chan []byte) [][]byte {
	t.Helper()
	var chunks [][]byte
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out draining the FLV stream")
		}
	}
}

func TestGatewayPrefersLiveSource(t *testing.T) {
	live := &fakeLiveSource{frames: []model.PSFrame{onePSFrame(t)}}
	mediaSvc := &countingMedia{inner: fakePSMediaSupplier{frames: []model.PSFrame{onePSFrame(t)}}}
	g := NewGateway(mediaSvc, fakeDeviceRegistry{}, nil).WithLiveSource(live)

	out, err := g.Subscribe(context.Background(), liveTestNode(t), "ch-1")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	chunks := drainChunks(t, out)
	if live.called != 1 {
		t.Errorf("live source used %d times, want 1", live.called)
	}
	if mediaSvc.calls != 0 {
		t.Errorf("local source used %d times, want 0", mediaSvc.calls)
	}
	if len(chunks) == 0 || !bytes.Equal(chunks[0], FLVHeader) {
		t.Fatalf("first chunk is not the FLV header: % x", chunks)
	}
	if live.cleanups != 1 {
		t.Errorf("live subscription cleanup ran %d times, want 1", live.cleanups)
	}
}

func TestGatewayFallsBackToLocalWithoutLiveSession(t *testing.T) {
	live := &fakeLiveSource{absent: true}
	mediaSvc := &countingMedia{inner: fakePSMediaSupplier{frames: []model.PSFrame{onePSFrame(t)}}}
	g := NewGateway(mediaSvc, fakeDeviceRegistry{}, nil).WithLiveSource(live)

	out, err := g.Subscribe(context.Background(), liveTestNode(t), "ch-1")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	drainChunks(t, out)
	if mediaSvc.calls != 1 {
		t.Errorf("local source used %d times, want 1", mediaSvc.calls)
	}
}

func TestGatewaySourceLiveFailsWithoutSession(t *testing.T) {
	g := NewGateway(&fakePSMediaSupplier{}, fakeDeviceRegistry{}, nil).
		WithLiveSource(&fakeLiveSource{absent: true})

	_, err := g.SubscribeSource(context.Background(), liveTestNode(t), "ch-1", "live")
	var unsupported *ErrStreamingUnsupported
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want ErrStreamingUnsupported", err)
	}
}

func TestGatewaySourceLocalIgnoresLive(t *testing.T) {
	live := &fakeLiveSource{frames: []model.PSFrame{onePSFrame(t)}}
	mediaSvc := &countingMedia{inner: fakePSMediaSupplier{frames: []model.PSFrame{onePSFrame(t)}}}
	g := NewGateway(mediaSvc, fakeDeviceRegistry{}, nil).WithLiveSource(live)

	out, err := g.SubscribeSource(context.Background(), liveTestNode(t), "ch-1", "local")
	if err != nil {
		t.Fatalf("SubscribeSource(local): %v", err)
	}
	drainChunks(t, out)
	if live.called != 0 {
		t.Errorf("local was requested but the live source was used %d times", live.called)
	}
	if mediaSvc.calls != 1 {
		t.Errorf("local source used %d times, want 1", mediaSvc.calls)
	}
}

func TestGatewayRejectsUnknownSource(t *testing.T) {
	g := NewGateway(&fakePSMediaSupplier{}, fakeDeviceRegistry{}, nil)
	if _, err := g.SubscribeSource(context.Background(), liveTestNode(t), "ch-1", "satellite"); err == nil {
		t.Fatal("an unknown source was accepted")
	}
}

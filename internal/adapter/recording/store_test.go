package recording

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

func testNode(t *testing.T) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID("34020000002000000001")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	return id
}

func psFrame(marker byte) model.PSFrame {
	return model.PSFrame{Payload: []byte{0x00, 0x00, 0x01, 0xBA, marker, marker, marker}}
}

func TestAppendCreatesSegmentAndLists(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = s.Close() }()
	node := testNode(t)

	if err := s.Append(context.Background(), node, "ch-1", psFrame(0x11)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	recs := s.List(context.Background(), node, "ch-1", time.Time{}, time.Time{})
	if len(recs) != 1 {
		t.Fatalf("List = %d recordings, want 1", len(recs))
	}
	if recs[0].ChannelID != "ch-1" || recs[0].Size == 0 || recs[0].Path == "" {
		t.Errorf("recording = %+v", recs[0])
	}
	if other := s.List(context.Background(), node, "ch-2", time.Time{}, time.Time{}); len(other) != 0 {
		t.Errorf("another channel sees %d recordings", len(other))
	}
}

func TestAppendRollsSegments(t *testing.T) {
	s, err := New(t.TempDir(), 40*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = s.Close() }()
	node := testNode(t)

	for i := 0; i < 2; i++ {
		if err := s.Append(context.Background(), node, "ch-1", psFrame(byte(0x20+i))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	recs := s.List(context.Background(), node, "ch-1", time.Time{}, time.Time{})
	if len(recs) != 2 {
		t.Fatalf("List = %d recordings, want 2 after the segment duration", len(recs))
	}
	if recs[0].Start.After(recs[1].Start) {
		t.Error("recordings are not ordered by start time")
	}
}

func TestListFiltersWindow(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = s.Close() }()
	node := testNode(t)
	if err := s.Append(context.Background(), node, "ch-1", psFrame(0x11)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	all := s.List(context.Background(), node, "ch-1", time.Time{}, time.Time{})
	if len(all) != 1 {
		t.Fatalf("List = %d", len(all))
	}
	rec := all[0]

	if got := s.List(context.Background(), node, "ch-1", rec.Start.Add(-time.Minute), rec.Start.Add(-time.Second)); len(got) != 0 {
		t.Errorf("a window before the recording returned %d", len(got))
	}
	if got := s.List(context.Background(), node, "ch-1", rec.Start, rec.End); len(got) != 1 {
		t.Errorf("a window covering the recording returned %d", len(got))
	}
}

func TestScanRebuildsIndex(t *testing.T) {
	dir := t.TempDir()
	node := testNode(t)
	first, err := New(dir, time.Minute)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := first.Append(context.Background(), node, "ch-1", psFrame(0x11)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := New(dir, time.Minute)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = second.Close() }()
	recs := second.List(context.Background(), node, "ch-1", time.Time{}, time.Time{})
	if len(recs) != 1 {
		t.Fatalf("after reopen List = %d, want 1", len(recs))
	}
	if recs[0].Size == 0 {
		t.Error("the scanned recording has no size")
	}
}

func TestStreamReplaysFrames(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	node := testNode(t)
	frames := []model.PSFrame{psFrame(0x11), psFrame(0x22)}
	for _, fr := range frames {
		if err := s.Append(context.Background(), node, "ch-1", fr); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ch, cleanup, err := s.Stream(context.Background(), node, "ch-1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer cleanup()
	var got [][]byte
	for fr := range ch {
		got = append(got, fr.Payload)
	}
	if len(got) != len(frames) {
		t.Fatalf("streamed %d frames, want %d", len(got), len(frames))
	}
	for i := range frames {
		if string(got[i]) != string(frames[i].Payload) {
			t.Errorf("frame %d = % X, want % X", i, got[i], frames[i].Payload)
		}
	}
}

func TestStreamConcatenatesSegmentsInOrder(t *testing.T) {
	s, err := New(t.TempDir(), 40*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	node := testNode(t)
	if err := s.Append(context.Background(), node, "ch-1", psFrame(0x11)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := s.Append(context.Background(), node, "ch-1", psFrame(0x22)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ch, cleanup, err := s.Stream(context.Background(), node, "ch-1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer cleanup()
	var order []byte
	for fr := range ch {
		order = append(order, fr.Payload[4])
	}
	if len(order) != 2 || order[0] != 0x11 || order[1] != 0x22 {
		t.Fatalf("frames in order % X, want [11 22]", order)
	}
}

func TestStreamUnknownChannelIsNotFound(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = s.Close() }()
	_, _, err = s.Stream(context.Background(), testNode(t), "nope", time.Time{}, time.Time{})
	if !errors.Is(err, port.ErrRecordingNotFound) {
		t.Fatalf("err = %v, want ErrRecordingNotFound", err)
	}
}

func TestStreamCancelStops(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	node := testNode(t)
	for i := 0; i < 50; i++ {
		if err := s.Append(context.Background(), node, "ch-1", psFrame(byte(i))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, cleanup, err := s.Stream(ctx, node, "ch-1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	<-ch // one frame is enough to know the reader is running
	cancel()
	cleanup()
	deadline := time.Now().Add(time.Second)
	for range ch {
		if time.Now().After(deadline) {
			t.Fatal("the stream kept producing after cancellation")
		}
	}
}

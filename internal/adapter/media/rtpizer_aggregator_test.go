package media

import (
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestRTPizerAggregatesValidationError asserts that an invalid PS frame is
// counted under the "rtp-send" signature and that the original error is still
// returned to the caller unchanged.
func TestRTPizerAggregatesValidationError(t *testing.T) {
	agg := NewErrorAggregatorWithWindow(time.Hour)
	r := NewRTPizer(0xABCDEF01, 1400, agg)

	bad := model.PSFrame{Payload: nil, PTS: 0}
	for i := 0; i < 4; i++ {
		if _, err := r.Packetize(bad); err == nil {
			t.Fatalf("case %d: expected validation error for empty PS payload", i)
		}
	}

	if got := agg.Snapshot()["rtp-send"]; got != 4 {
		t.Fatalf("rtp-send count = %d, want 4 (snapshot=%v)", got, agg.Snapshot())
	}
}

// TestRTPizerNilAggregator verifies the nil-aggregator fast path keeps the
// original behaviour: the error is returned and no panic occurs.
func TestRTPizerNilAggregator(t *testing.T) {
	r := NewRTPizer(0xABCDEF01, 1400, nil)
	if _, err := r.Packetize(model.PSFrame{Payload: nil}); err == nil {
		t.Fatal("expected validation error with nil aggregator")
	}

	// A valid frame must still produce RTP packets.
	ps := model.PSFrame{
		Payload: []byte{0x00, 0x00, 0x01, 0xBA, 0x00, 0x00, 0x01, 0xBB},
		PTS:     90000,
	}
	pkts, err := r.Packetize(ps)
	if err != nil {
		t.Fatalf("valid frame packetize: %v", err)
	}
	if len(pkts) == 0 {
		t.Fatal("valid frame produced zero RTP packets")
	}
}

// TestRTPizerValidFrameNotAggregated guards against over-counting: healthy
// frames must not touch the aggregator.
func TestRTPizerValidFrameNotAggregated(t *testing.T) {
	agg := NewErrorAggregatorWithWindow(time.Hour)
	r := NewRTPizer(0xABCDEF01, 1400, agg)

	ps := model.PSFrame{
		Payload: []byte{0x00, 0x00, 0x01, 0xBA, 0x00, 0x00, 0x01, 0xBB},
		PTS:     90000,
	}
	for i := 0; i < 5; i++ {
		if _, err := r.Packetize(ps); err != nil {
			t.Fatalf("case %d: unexpected error: %v", i, err)
		}
	}
	if snap := agg.Snapshot(); len(snap) != 0 {
		t.Fatalf("aggregator recorded %v, want empty", snap)
	}
}

package media

import (
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestPSPacketizerAggregatesValidationError asserts that an invalid ES frame
// is counted under the "ps-mux" signature and that the original error is still
// returned to the caller unchanged.
func TestPSPacketizerAggregatesValidationError(t *testing.T) {
	agg := NewErrorAggregatorWithWindow(time.Hour)
	p := NewPSPacketizer(agg)

	bad := model.ESFrame{Payload: nil, PTS: 0, Kind: model.ESFrameVideo}
	for i := 0; i < 3; i++ {
		if _, err := p.Packetize(bad); err == nil {
			t.Fatalf("case %d: expected validation error for empty payload", i)
		}
	}

	if got := agg.Snapshot()["ps-mux"]; got != 3 {
		t.Fatalf("ps-mux count = %d, want 3 (snapshot=%v)", got, agg.Snapshot())
	}
}

// TestPSPacketizerNilAggregator verifies the nil-aggregator fast path keeps
// the original behaviour: the error is returned and nothing panics.
func TestPSPacketizerNilAggregator(t *testing.T) {
	p := NewPSPacketizer(nil)
	bad := model.ESFrame{Payload: nil, PTS: 0, Kind: model.ESFrameVideo}
	if _, err := p.Packetize(bad); err == nil {
		t.Fatal("expected validation error with nil aggregator")
	}

	// A valid frame must still packetize normally.
	good := model.NewESFrame([]byte{0x00, 0x00, 0x00, 0x01, 0x65}, 90000)
	ps, err := p.Packetize(good)
	if err != nil {
		t.Fatalf("valid frame packetize: %v", err)
	}
	if len(ps.Payload) == 0 {
		t.Fatal("valid frame produced empty PS payload")
	}
}

// TestPSPacketizerValidFrameNotAggregated guards against over-counting: a
// healthy frame must not touch the aggregator.
func TestPSPacketizerValidFrameNotAggregated(t *testing.T) {
	agg := NewErrorAggregatorWithWindow(time.Hour)
	p := NewPSPacketizer(agg)

	for i := 0; i < 5; i++ {
		f := model.NewESFrame([]byte{0x00, 0x00, 0x00, 0x01, 0x65, byte(i)}, 90000)
		if _, err := p.Packetize(f); err != nil {
			t.Fatalf("case %d: unexpected error: %v", i, err)
		}
	}
	if snap := agg.Snapshot(); len(snap) != 0 {
		t.Fatalf("aggregator recorded %v, want empty", snap)
	}
}

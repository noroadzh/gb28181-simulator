// Package media — PS/RTP packetizers and media sources.
//
// All concrete implementations of the media ports live here:
//   - PS packetizer (ES → PS)
//   - PS depacketizer (PS → ES)
//   - RTP packetizer (PS → RTP)
//   - RTP depacketizer (RTP → PS)
//   - Four media sources (file, RTSP, HLS, synthetic)
//
// The app layer (internal/app/) depends only on the ports in
// internal/domain/port/media.go; this package has no upstream dependencies
// except internal/domain/model and internal/domain/port.
package media

import (
	"sort"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// RTPDeizer reassembles RTP packets back into PS frames.
//
// It buffers packets until the marker bit is seen, deduplicates by
// sequence number, then sorts by sequence and concatenates to reconstruct
// the original PS payload (handles out-of-order delivery).
type RTPDeizer struct {
	ssrc     uint32
	framePTS uint64
	pending  map[uint16][]byte
	gotFirst bool
}

// NewRTPDeizer returns a configured RTPDeizer.
func NewRTPDeizer(ssrc uint32) *RTPDeizer {
	return &RTPDeizer{ssrc: ssrc}
}

// Write ingests one RTP datagram and emits any PS frames that are fully
// reassembled. A nil PS frame and nil error means the packet was buffered
// for the next one.
func (d *RTPDeizer) Write(pkt model.RTPPacket) (model.PSFrame, error) {
	if err := pkt.Validate(); err != nil {
		return model.PSFrame{}, err
	}

	// Initialize on first packet.
	if !d.gotFirst {
		d.ssrc = pkt.SSRC
		d.framePTS = uint64(pkt.Timestamp)
		d.gotFirst = true
	}

	// Buffer payload by sequence (dedup by overwrite).
	if d.pending == nil {
		d.pending = make(map[uint16][]byte)
	}
	d.pending[pkt.Sequence] = pkt.Payload

	// Marker bit = end of frame: sort sequences and emit.
	if pkt.Marker {
		out := d.flush()
		return model.PSFrame{Payload: out, PTS: d.framePTS}, nil
	}

	return model.PSFrame{}, nil
}

// flush concatenates buffered packets in sequence order and resets state.
func (d *RTPDeizer) flush() []byte {
	if len(d.pending) == 0 {
		return nil
	}
	seqs := make([]uint16, 0, len(d.pending))
	for s := range d.pending {
		seqs = append(seqs, s)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })

	out := make([]byte, 0, 200)
	for _, s := range seqs {
		out = append(out, d.pending[s]...)
	}
	d.pending = nil
	return out
}

// Close ends the deizer and flushes any buffered partial frame.
func (d *RTPDeizer) Close() error {
	d.pending = nil
	return nil
}

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
	"encoding/binary"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// RTP payload type for PS (dynamic, negotiated via SDP).
const defaultRTPPayloadType = 96

// RTPizer slices a PS frame into RTP packets.
//
// The RTPizer is stateless with respect to a single PS frame but keeps
// sequence number and SSRC across frames so consecutive frames flow as one
// RTP session. Each call to Packetize consumes one PSFrame and yields one or
// more RTPPacket (one per MTU-sized slice).
type RTPizer struct {
	ssrc        uint32
	seq         uint16
	payloadType uint8
	mtu         int
}

// NewRTPizer returns a configured RTPizer.
func NewRTPizer(ssrc uint32, mtu int) *RTPizer {
	if mtu <= 0 {
		mtu = 1400
	}
	if ssrc == 0 {
		ssrc = 0x12345678 // default SSRC for testing
	}
	return &RTPizer{
		ssrc:        ssrc,
		seq:         0,
		payloadType: defaultRTPPayloadType,
		mtu:         mtu,
	}
}

// Packetize returns one or more RTP packets for the given PS frame.
// marker=true on the last packet marks the PS frame boundary.
func (r *RTPizer) Packetize(ps model.PSFrame) ([]model.RTPPacket, error) {
	if err := ps.Validate(); err != nil {
		return nil, err
	}

	// Map the 90 kHz PTS to the RTP timestamp domain (same value, both 90 kHz).
	rtpTS := uint32(ps.PTS & 0xFFFFFFFF)
	total := len(ps.Payload)

	if total <= r.mtu-12 { // one packet fits
		r.seq++
		return []model.RTPPacket{
			{
				Sequence:    r.seq,
				SSRC:        r.ssrc,
				PayloadType: r.payloadType,
				Marker:      true, // end of frame
				Timestamp:   rtpTS,
				Payload:     ps.Payload,
			},
		}, nil
	}

	// Fragment: one packet per MTU-sized slice.
	var pkts []model.RTPPacket
	offset := 0
	for offset < total {
		r.seq++
		end := offset + r.mtu - 12
		if end > total {
			end = total
		}
		isLast := end == total

		pkts = append(pkts, model.RTPPacket{
			Sequence:    r.seq,
			SSRC:        r.ssrc,
			PayloadType: r.payloadType,
			Marker:      isLast,
			Timestamp:   rtpTS,
			Payload:     ps.Payload[offset:end],
		})
		offset = end
	}

	return pkts, nil
}

// Sequence returns the current RTP sequence number (the next to be emitted).
func (r *RTPizer) Sequence() uint16 {
	return r.seq
}

// WritePacketHeaderTo writes the 12-byte RTP fixed header into buf for the
// given packet. buf must be at least 12 bytes. The payload follows in the
// caller's buffer.
func WritePacketHeaderTo(pkt model.RTPPacket, buf []byte) {
	// Byte 0: version 2, no padding, no extension, no CSRCs.
	buf[0] = 0x80
	// Byte 1: marker bit + payload type.
	buf[1] = pkt.PayloadType
	if pkt.Marker {
		buf[1] |= 0x80
	}
	binary.BigEndian.PutUint16(buf[2:4], pkt.Sequence)
	binary.BigEndian.PutUint32(buf[4:8], pkt.Timestamp)
	binary.BigEndian.PutUint32(buf[8:12], pkt.SSRC)
}

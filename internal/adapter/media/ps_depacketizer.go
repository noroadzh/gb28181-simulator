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
	"bytes"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// PSDepacketizer consumes a byte stream containing PS frames and emits ES
// frames. It is stateful: an instance is bound to one source and its PS
// stream. Callers feed raw bytes; the depacketizer emits frames as they are
// cut.
//
// Every PS frame written by the packetizer follows the layout:
//
//	00 00 01 BA <12-byte pack header>
//	00 00 01 E0 <2-byte PES length> <9-byte PES header> <ES payload>   (video)
//	00 00 01 C0 <2-byte PES length> <9-byte PES header> <ES payload>   (audio)
type PSDepacketizer struct {
	buf []byte
}

// NewPSDepacketizer returns a depacketizer ready to consume PS bytes.
func NewPSDepacketizer() *PSDepacketizer {
	return &PSDepacketizer{}
}

// Write ingests raw PS bytes and yields any ES frames that are fully cut.
// The caller should keep calling Write until the stream ends, then Close.
// A nil []esFrame and nil error means the packet was buffered for the
// next one.
func (d *PSDepacketizer) Write(raw []byte) ([]model.ESFrame, error) {
	d.buf = append(d.buf, raw...)
	return d.cutFrames()
}

// cutFrames scans the buffer for complete PS frames and emits ES frames.
func (d *PSDepacketizer) cutFrames() ([]model.ESFrame, error) {
	var frames []model.ESFrame

	for {
		// Find the next PS pack start code (00 00 01 BA).
		idx := bytes.Index(d.buf, []byte{0x00, 0x00, 0x01, 0xBA})
		if idx < 0 {
			break
		}
		d.buf = d.buf[idx:]

		// Find the next PES stream start code (00 00 01 E0 or 00 00 01 C0) after the pack header.
		pesIdx := bytes.Index(d.buf[12:], []byte{0x00, 0x00, 0x01})
		if pesIdx < 0 {
			break
		}
		pesIdx += 12
		if pesIdx+3 >= len(d.buf) {
			break
		}
		streamID := d.buf[pesIdx+3]
		if streamID != 0xE0 && streamID != 0xC0 {
			// Not a recognized stream; skip past this start code and keep scanning.
			d.buf = d.buf[pesIdx+4:]
			continue
		}

		// We need at least 6 more bytes for the PES length field.
		if len(d.buf) < pesIdx+6 {
			break
		}

		// Read PES_packet_length (big-endian, 2 bytes after stream_id).
		pesLen := int(d.buf[pesIdx+4])<<8 | int(d.buf[pesIdx+5])

		// Reject short packets that cannot contain the minimum 9-byte PES header.
		if pesLen < 9 {
			// Skip this malformed frame and continue scanning.
			d.buf = d.buf[pesIdx+4:]
			continue
		}

		// We need the full PES packet: stream_id(1) + length(2) + pesLen bytes.
		if len(d.buf) < pesIdx+6+pesLen {
			break
		}

		// ES payload starts after the PES header.
		//   pesIdx + 0..2 : start code 00 00 01
		//   pesIdx + 3    : stream_id (E0 or C0)
		//   pesIdx + 4..5 : PES_packet_length (big-endian)
		//   pesIdx + 6..14: 9-byte PES header (flags + PTS)
		//   pesIdx + 15   : first ES byte
		// pesLen = 9 (PES header) + esLen  →  esLen = pesLen - 9
		esStart := pesIdx + 15
		esLen := pesLen - 9

		// Guard against malformed packets.
		if esStart < 0 || esStart >= len(d.buf) || esLen <= 0 || esStart+esLen > len(d.buf) {
			// Skip this frame and continue scanning.
			d.buf = d.buf[pesIdx+4:]
			continue
		}

		// Extract the PTS (in 90 kHz domain, 5 bytes starting at pesIdx+10).
		pts := decodePTS(d.buf[pesIdx+10 : pesIdx+15])

		// Copy the ES payload so the caller gets an independent slice.
		esPayload := make([]byte, esLen)
		copy(esPayload, d.buf[esStart:esStart+esLen])

		kind := model.ESFrameVideo
		if streamID == 0xC0 {
			kind = model.ESFrameAudio
		}
		frames = append(frames, model.ESFrameWithPTSAndKind(esPayload, pts, kind))

		// Remove the consumed frame from the buffer.
		frameEnd := pesIdx + 6 + pesLen
		if frameEnd < len(d.buf) {
			d.buf = d.buf[frameEnd:]
		} else {
			d.buf = d.buf[:0]
			break
		}
	}

	return frames, nil
}

// Close ends the depacketizer and discards any buffered bytes.
// After Close, further Write calls return nil frames.
func (d *PSDepacketizer) Close() error {
	d.buf = nil
	return nil
}

// findStartCode scans b for the start code value (00 00 01 xx) and returns
// the index of the first byte of the start code, or -1 if not found.
func findStartCode(b []byte, code uint32) int {
	_ = b[len(b)-1] // bounds check hint
	for i := 0; i <= len(b)-4; i++ {
		if uint32(b[i])<<24|uint32(b[i+1])<<16|uint32(b[i+2])<<8|uint32(b[i+3]) == code {
			return i
		}
	}
	return -1
}

// decodePTS decodes a 5-byte MPEG-2 PTS into a uint64 (90 kHz domain).
//
// Format (40 bits):
//
//	byte 0: '0010' + 3 bits + '1'             -> bits 33..32 of PTS are the 3 bits, top 4 bits are the marker
//	byte 1: 8 bits                            -> PTS[31..24]
//	byte 2: (PTS[21..15] << 1) | '1'         -> 7+1 = 8 bits
//	byte 3: 8 bits                            -> PTS[15..8]
//	byte 4: (PTS[6..0] << 1) | '1'            -> 7+1 = 8 bits
//
// We discard the top 4 bits ('0010' marker) and the trailing '1' bits.
func decodePTS(b []byte) uint64 {
	if len(b) != 5 {
		return 0
	}
	return (uint64(b[0]&0x0E) << 29) |
		(uint64(b[1]) << 22) |
		(uint64(b[2]&0xFE) << 14) |
		(uint64(b[3]) << 7) |
		(uint64(b[4]) >> 1)
}

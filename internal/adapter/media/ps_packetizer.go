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

// PS constants — MPEG-2 Program Stream start codes.
const (
	psPackStartCode  = 0x000001BA
	psVideoStartCode = 0x000001E0 // video elementary stream
)

// defaultMuxRate is the nominal stream bit-rate in bits per second.
// 9_000_000 = 9 Mbps, a safe default that fits most SD/HD streams.
// The MPEG-2 program_mux_rate field is measured in 50-byte units,
// so we convert bits/s → bytes/s → 50-byte units: muxRate/8/50.
const defaultMuxRate = 9000000

// PSPacketizer turns ES frames into program-stream packets.
//
// Every ES frame becomes one PS frame. The PS frame always starts with
//   00 00 01 BA <pack header>
//   00 00 01 E0 <PES header with PTS>
//   <ES payload>
type PSPacketizer struct {
	muxRate uint32
}

// NewPSPacketizer returns a PSPacketizer ready to emit PS frames.
func NewPSPacketizer() *PSPacketizer {
	return &PSPacketizer{muxRate: defaultMuxRate}
}

// Packetize returns one PSFrame wrapping the ES frame. The ES payload is
// taken by reference; the returned PSFrame Payload includes the PS header
// bytes so the caller does not need to prepend anything.
func (p *PSPacketizer) Packetize(frame model.ESFrame) (model.PSFrame, error) {
	if err := frame.Validate(); err != nil {
		return model.PSFrame{}, err
	}

	// Pack header (12 bytes total, preceded by 00 00 01 BA).
	// MPEG-2 PS pack header (simplified, SCR=0):
	//   Byte 0: '01' (2 bits) + SCR[32..30] (3 bits) + marker (1 bit) + SCR[29..28] (2 bits) -> 0x44
	//   Byte 1: SCR[27..20]
	//   Byte 2: (SCR[19..15] << 1) | marker
	//   Byte 3: SCR[14..7]
	//   Byte 4: (SCR[6..0] << 1) | marker
	//   Bytes 5-6: program_mux_rate (22 bits) in 50-byte units
	//   Bytes 7-11: reserved (zeros)
	muxVal := p.muxRate / 8 / 50 // bits/s -> bytes/s -> 50-byte units
	packHeader := make([]byte, 12)
	packHeader[0] = 0x44 // '01' + SCR=0 + marker + 0
	binary.BigEndian.PutUint32(packHeader[1:5], 0x00010001) // SCR=0 with mandatory marker bits
	binary.BigEndian.PutUint16(packHeader[5:7], uint16(muxVal))

	// PES header for video (variable length, minimum 9 bytes after stream_id).
	// Layout:
	//   2 bytes: PES_packet_length (everything after these 2 bytes)
	//   1 byte:  '10' (6 bits) + 6 reserved '111111'
	//   1 byte:  PTS_DTS_flags '10' (5 bits) + other flags (3 bits)
	//   1 byte:  other flags (PTS/DTS/ES_rate/DSM_trick/CRC/ext = 0)
	//   1 byte:  PES_header_data_length (5)
	//   5 bytes: PTS (MPEG-2 format)
	esLen := len(frame.Payload)
	pesHeaderLen := 9 // fixed PES header size (length field excluded)
	pesTotal := 2 + pesHeaderLen // length field + PES header only
	pesHeader := make([]byte, pesTotal)
	binary.BigEndian.PutUint16(pesHeader[0:2], uint16(pesHeaderLen+esLen))
	pesHeader[2] = 0x80 // '10' marker
	pesHeader[3] = 0x80 // PTS only
	pesHeader[4] = 0x00
	pesHeader[5] = 0x05 // 5 bytes of PTS follow

	// Encode PTS in 90 kHz domain into 5-byte MPEG-2 format:
	//   byte 0: '0010' + PTS[32..30] + '1'
	//   byte 1: PTS[29..22]
	//   byte 2: (PTS[21..15] << 1) | '1'
	//   byte 3: PTS[14..7]
	//   byte 4: (PTS[6..0] << 1) | '1'
	pts := frame.PTS
	pesHeader[6] = 0x21 | byte((pts>>29)&0x0E)
	pesHeader[7] = byte((pts >> 22) & 0xFF)
	pesHeader[8] = ((byte((pts >> 15) & 0x7F)) << 1) | 0x01
	pesHeader[9] = byte((pts >> 7) & 0xFF)
	pesHeader[10] = (byte(pts&0x7F) << 1) | 0x01

	// Assemble full PS frame.
	ps := make([]byte, 0, 4+12+4+pesTotal+esLen)
	ps = append(ps, 0x00, 0x00, 0x01, 0xBA) // pack start code
	ps = append(ps, packHeader...)
	ps = append(ps, 0x00, 0x00, 0x01, 0xE0) // video stream
	ps = append(ps, pesHeader...)
	ps = append(ps, frame.Payload...)

	return model.PSFrame{Payload: ps, PTS: pts}, nil
}

// Header returns the MPEG-2 PS system header prefix (00 00 01 BB + body).
// Callers prepend it before the first PS frame if the transport requires it.
func (p *PSPacketizer) Header() []byte {
	// Minimal system header: 14 bytes after 00 00 01 BB.
	sh := make([]byte, 18)
	sh[0] = 0x00
	sh[1] = 0x00
	sh[2] = 0x01
	sh[3] = 0xBB // system header start code
	binary.BigEndian.PutUint16(sh[4:6], 14) // header length = 14
	// Rate bound = 1 Mbps, 1 video stream, 1 audio stream
	sh[6] = 0x80
	sh[7] = 0x01
	sh[8] = 0x00
	sh[9] = 0x01 // 1 video stream bound
	sh[10] = 0xE0
	sh[11] = 0x01 // 1 audio stream bound
	sh[12] = 0xC0
	return sh
}

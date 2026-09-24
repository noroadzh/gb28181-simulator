// Package port — MediaSource, PSPacketizer, RTPizer.
//
// This file defines the ports through which the media pipeline talks to the
// outside world: a source that produces ES frames, packetizers that turn ES
// into PS and PS into RTP, and the symmetric de-packetizers on the way in.
// app layer code depends only on these interfaces and on
// internal/domain/model; the concrete adapters live in internal/adapter/media.
package port

import (
	"bytes"
	"context"
	"io"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// MediaSource is the entry point of the outbound pipeline: a single method
// that, given configuration, produces an ordered ES-frame stream.
//
// The caller MUST close the returned ReadCloser when it no longer wants
// frames. A closed or errored Read yields io.EOF; a failure to open returns
// the error and an empty ReadCloser, so the caller never has to guess what
// kind of failure it got.
type MediaSource interface {
	// Open connects to the configured origin (file path, RTSP URL, HLS
	// playlist) and returns a reader that yields ES frames. The context is
	// for cancellation only — sources MUST NOT block on Open after the
	// context is done, and MUST release their resources if it is.
	Open(ctx context.Context) (io.ReadCloser, error)

	// Close releases the underlying resource. It is safe to call Close
	// multiple times; calls after the first return nil.
	Close() error

	// Config returns the configuration the source was opened with. It is
	// immutable after Open and safe for concurrent reads.
	Config() model.MediaConfig
}

// ESReader returns ES frames from an opened source. Reads block until a
// frame is available, the source is closed, or the context is done.
type ESReader interface {
	// Read returns the next ESFrame. A nil frame and a non-nil error mean
	// the stream is dead; a non-nil frame and nil error is a normal unit
	// of data. 0-length frames are legal timestamps-only markers (e.g. a
	// PTS jump); they are never elided by the source.
	Read(ctx context.Context) (model.ESFrame, error)
}

// ESWriteCloser extends ESReader with a write side for the reassembler path
// (RTPDeizer → PSDepacketizer → file or callback).
type ESWriteCloser interface {
	ESReader
	// Write stores an ES frame produced by the depacketizer. Writes block
	// until the frame is accepted or the context is done; a closed
	// destination returns io.EOF.
	Write(ctx context.Context, frame model.ESFrame) error
}

// PSPacketizer turns ES frames into PS packets. Every call to Packetize
// consumes one ES frame and yields one PS frame (possibly split across
// multiple RTP packets later). The PTS from the ES frame is propagated so
// the RTP layer can stamp its timestamp field without recomputing.
type PSPacketizer interface {
	// Packetize consumes one ES frame and returns one PS frame. A nil PS
	// frame with a non-nil error means the frame could not be packaged
	// (malformed NALU boundary, etc.) and should be skipped or reported.
	Packetize(frame model.ESFrame) (model.PSFrame, error)

	// Header returns the system header prefix (the bytes before the ES
	// payload begins) so callers that build PS streams incrementally can
	// align with the first frame. The header is the same for every frame
	// of a given source and only changes on Reset.
	Header() []byte
}

// PSDepacketizer consumes a byte stream containing PS frames and emits ES
// frames. It is stateful: an instance is bound to one source and its PS
// stream. Callers feed raw bytes; the depacketizer emits frames as they are
// cut.
type PSDepacketizer interface {
	// Write ingests raw PS bytes and yields any ES frames that are fully
	// cut. It is legal for Write to emit zero frames; callers should keep
	// feeding bytes until the source ends.
	Write(p []byte) ([]model.ESFrame, error)

	// Close ends the depacketizer and flushes any buffered partial frame.
	// After Close, further Write calls return an error.
	Close() error
}

// RTPizer slices a PS frame into RTP packets suitable for sending over
// UDP. It is stateless with respect to a single PS frame, but it keeps the
// sequence number and SSRC across frames so consecutive frames flow as one
// RTP session.
type RTPizer interface {
	// Packetize consumes one PS frame and returns one or more RTP
	// datagrams. It MAY buffer the frame if the MTU would produce
	// unusably tiny packets; callers should not assume one PS frame maps
	// to exactly one RTP packet.
	Packetize(ps model.PSFrame) ([]model.RTPPacket, error)

	// Sequence returns the next sequence number to be emitted, so callers
	// that manage their own RTP session state can detect loss or reorder
	// without inspecting the packet bytes.
	Sequence() uint16
}

// RTPDeizer reassembles an RTP byte stream into PS frames. It groups
// packets by SSRC and payload type, orders them by sequence, discards
// duplicates, and emits a PS frame when the reassembler says one is
// complete.
type RTPDeizer interface {
	// Write ingests one RTP datagram and emits any PS frames that are
	// fully reassembled. A nil PS frame and nil error means the packet
	// was buffered for the next one.
	Write(pkt model.RTPPacket) (model.PSFrame, error)

	// Close ends the deizer and flushes any buffered partial frame. After
	// Close, further Write calls return an error.
	Close() error
}

// StreamESReader reads an elementary-stream byte stream (typically H.264/H.265
// with 4-byte start codes) and yields one ESFrame per NAL unit.
type StreamESReader struct {
	rc     io.ReadCloser
	config model.MediaConfig
	buf    []byte
	eof    bool
	pts    uint64
}

// NewStreamESReader builds a reader that splits an elementary-stream byte
// stream into ESFrame values.
func NewStreamESReader(rc io.ReadCloser, config model.MediaConfig) *StreamESReader {
	return &StreamESReader{rc: rc, config: config}
}

// Read returns the next ESFrame from the stream.
func (r *StreamESReader) Read(ctx context.Context) (model.ESFrame, error) {
	if r.eof {
		return model.ESFrame{}, io.EOF
	}

	const startCode = "\x00\x00\x00\x01"
	for {
		if len(r.buf) > 0 && !bytes.HasPrefix(r.buf, []byte(startCode)) {
			r.buf = r.buf[1:]
			continue
		}
		if len(r.buf) < 4 {
			chunk := make([]byte, 4096)
			n, err := r.rc.Read(chunk)
			if n > 0 {
				r.buf = append(r.buf, chunk[:n]...)
			}
			if err != nil {
				r.eof = true
				if len(r.buf) > 0 {
					frame := model.ESFrameWithPTS(r.buf, r.pts)
					r.buf = nil
					return frame, nil
				}
				if err == io.EOF {
					return model.ESFrame{}, io.EOF
				}
				return model.ESFrame{}, err
			}
			continue
		}

		next := bytes.Index(r.buf[4:], []byte(startCode))
		if next >= 0 {
			nal := r.buf[:4+next]
			r.buf = r.buf[4+next:]
			frame := model.ESFrameWithPTS(nal, r.pts)
			if r.config.FPS > 0 {
				r.pts += r.config.Clock / uint64(r.config.FPS)
			}
			return frame, nil
		}

		chunk := make([]byte, 4096)
		n, err := r.rc.Read(chunk)
		if n > 0 {
			r.buf = append(r.buf, chunk[:n]...)
		}
		if err != nil {
			r.eof = true
			if len(r.buf) > 0 {
				frame := model.ESFrameWithPTS(r.buf, r.pts)
				r.buf = nil
				return frame, nil
			}
			if err == io.EOF {
				return model.ESFrame{}, io.EOF
			}
			return model.ESFrame{}, err
		}
	}
}

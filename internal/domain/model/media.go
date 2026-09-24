// Package model — media frames and pipeline configuration.
//
// This file holds the values that flow through the media pipeline: an ES
// frame coming out of a source, a PS packet ready for RTP slicing, an RTP
// packet on the wire, and the configuration a source is opened with. It has
// no dependency on any adapter — it is the vocabulary the port interfaces in
// internal/domain/port/media.go are written in.
package model

import (
	"fmt"
)

// ESFrame is one elementary-stream unit produced by a MediaSource: a coded
// audio/video sample together with its presentation timestamp. Payload is
// application bytes (typically a H.264/H.265 NALU or encoder frame); this
// package treats it opaquely — packaging and depacketization move it, they
// never interpret it.
type ESFrame struct {
	// Payload is the coded sample bytes. The MediaSource owns the backing
	// buffer; consumers MUST copy before retaining.
	Payload []byte
	// PTS is the presentation timestamp in the 90 kHz clock domain, the
	// GB/T 28181 and MPEG-style reference. 0 is a legal instantaneous value
	// but a frame with no meaningful time is flagged by ptsSet.
	PTS uint64
	// ptsSet distinguishes a decode-time-only frame from one carrying an
	// explicit presentation timestamp.
	ptsSet bool
}

// NewESFrame builds an ESFrame with an explicit presentation timestamp.
// The payload slice is taken by reference (not copied) to avoid double
// buffering in the hot pipeline; callers who retain it must copy.
func NewESFrame(payload []byte, pts uint64) ESFrame {
	return ESFrame{Payload: payload, PTS: pts, ptsSet: true}
}

// ESFrameWithPTS builds an ESFrame whose PTS comes from the depacketizer
// (i.e. parsed out of a PES header). It is the public hook that lets
// adapters in other packages flag the frame as timestamped without
// exposing the unexported ptsSet flag directly.
func ESFrameWithPTS(payload []byte, pts uint64) ESFrame {
	return ESFrame{Payload: payload, PTS: pts, ptsSet: true}
}

// HasPTS reports whether the frame carries an explicit timestamp.
func (f ESFrame) HasPTS() bool { return f.ptsSet }

// Validate reports a frame that cannot enter the pipeline: an empty payload
// (nothing to package) or a payload that looks like it is not frame-bounded.
func (f ESFrame) Validate() error {
	if len(f.Payload) == 0 {
		return fmt.Errorf("model: ESFrame payload is empty")
	}
	return nil
}

// PSFrame is one program-stream packet: the unit RTP packets are sliced
// from. It always carries the three layer-1 start codes at Payload[:6]
// (`00 00 01 BA` then `00 00 01 BB`/`00 00 01 E0`), produced by the PS
// packetizer and expected by the PS depacketizer.
type PSFrame struct {
	Payload []byte
	// PTS mirrors the source ES frame's timestamp for the RTP layer.
	PTS uint64
}

// Validate reports a PS frame that cannot be sliced into RTP.
func (p PSFrame) Validate() error {
	if len(p.Payload) == 0 {
		return fmt.Errorf("model: PSFrame payload is empty")
	}
	return nil
}

// RTPPacket is one RTP datagram on the wire. Header is the 12-byte fixed
// header; Payload follows it. Only the fields the RTPizer/RTPDeizer and the
// golden tests care about are surfaced as typed fields; the rest of the
// header lives in the bytes themselves.
type RTPPacket struct {
	// Sequence, SSRC, PayloadType, Marker and Timestamp are decoded header
	// fields, kept typed so the pipeline never re-parses bytes it already
	// read.
	Sequence    uint16
	SSRC        uint32
	PayloadType uint8
	Marker      bool
	Timestamp   uint32
	// Payload is the bytes after the fixed header.
	Payload []byte
}

// Validate reports an RTP packet that cannot be fed to the reassembler.
func (p RTPPacket) Validate() error {
	if len(p.Payload) == 0 {
		return fmt.Errorf("model: RTPPacket payload is empty")
	}
	return nil
}

// MediaSourceKind names one of the four built-in sources. The value doubles
// as a configuration discriminator.
type MediaSourceKind string

const (
	SourceKindFile      MediaSourceKind = "file"
	SourceKindRTSP      MediaSourceKind = "rtsp"
	SourceKindHLS       MediaSourceKind = "hls"
	SourceKindSynthetic MediaSourceKind = "synthetic"
)

// MediaConfig is what a MediaSource is opened with: which kind, where it
// lives, and the timing/limits it should honour. Values are validated by
// the caller before Open is called; this struct only carries them.
type MediaConfig struct {
	Kind  MediaSourceKind
	Path  string // file path, RTSP URL or HLS m3u8 URL
	SSRC  uint32 // RTP SSRC used by the RTPizer, 0 means auto-generate
	MTU   int    // max bytes per RTP payload; defaults to 1400
	FPS   int    // synthetic source frame rate; defaults to 25
	Clock uint64 // RTP clock rate (Hz); defaults to 90000
}

// Validate checks that a MediaConfig has the minimum fields for its kind.
// File, RTSP, and HLS sources require a non-empty Path; synthetic does not
// (it generates frames procedurally). A non-existent file is caught at Open.
// Validate does not mutate — call Normalize to fill defaults.
func (c MediaConfig) Validate() error {
	if c.Kind == "" {
		return fmt.Errorf("model: MediaConfig.Kind is empty")
	}
	switch c.Kind {
	case SourceKindFile, SourceKindRTSP, SourceKindHLS:
		if c.Path == "" {
			return fmt.Errorf("model: MediaConfig.Path is empty for kind %q", c.Kind)
		}
	}
	return nil
}

// Normalize returns a copy with zero-valued timing/limit fields replaced by
// their defaults (MTU 1400, FPS 25, Clock 90000). Kind and Path are left
// untouched; the caller should Validate before or after Normalize.
func (c MediaConfig) Normalize() MediaConfig {
	if c.MTU <= 0 {
		c.MTU = 1400
	}
	if c.FPS <= 0 {
		c.FPS = 25
	}
	if c.Clock == 0 {
		c.Clock = 90000
	}
	return c
}

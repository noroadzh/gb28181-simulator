// Package sdp provides GB/T 28181-2016 §K-aware SDP parsing and serialisation.
//
// Background: the upstream pion/sdp library implements RFC 4566 correctly but
// silently drops the §K.2 extension lines `y=` (SSRC) and `f=` (media option)
// which are mandatory for GB/T 28181 PS media negotiation. Without these
// fields, Change 8 (PS demux) cannot demultiplex streams and Change 9 (audio)
// cannot pick the correct codec — both degrade to "black screen / no audio"
// the first time a real device is connected.
//
// This package wraps pion/sdp with a line-level shim:
//
//  1. Before unmarshal: extracts every line starting with `y=` or `f=`
//     together with its owning media-block index, so pion never sees them.
//  2. After unmarshal: re-attaches the SSRC/MediaOption to the appropriate
//     media block in the returned *Session.
//  3. After marshal: re-injects the §K lines per §K.2 ordering
//     (`m=`/`a=` block → `y=` → `f=` → next `m=`).
//
// The public API stays plain:
//
//	Parse(text string) (*Session, error)
//	Marshal(s *Session) (string, error)
package sdp

import (
	"errors"

	pionsdp "github.com/pion/sdp"
)

// SSRC is the 32-bit decimal stream identifier per GB/T 28181-2016 §K.2.
// Rendered as a string so that callers do not need to convert back and
// forth across API boundaries.
type SSRC = string

// MediaOption names the role of a media block. Only the values declared in
// §K.2 are recognised: "v" (video), "a" (audio), "av" (audio+video), "m"
// (metadata). The empty string means "no §K extension was declared".
type MediaOption = string

// Session is the GB/T 28181 §K view of an SDP body. It embeds
// pion/sdp.SessionDescription so all RFC 4566 fields remain accessible.
//
// Convention: SSRC at session level is reserved for future use and is
// currently always empty. The §K.2 spec places the `y=` line inside each
// media block.
type Session struct {
	*pionsdp.SessionDescription

	// SSRC at the session level. Currently always empty.
	SSRC SSRC

	// Media is the ordered list of media blocks. The order matches the
	// textual order of `m=` lines in the body.
	Media []*MediaBlock
}

// MediaBlock augments pion/sdp.MediaDescription with §K.2 extension fields.
type MediaBlock struct {
	*pionsdp.MediaDescription

	// SSRC is the 32-bit decimal stream identifier for this media block.
	// Empty when the §K extension was not declared.
	SSRC SSRC

	// MediaOption is one of "v" / "a" / "av" / "m" / "".
	MediaOption MediaOption
}

// ErrEmptyBody is returned by Parse when the input is empty or whitespace-only.
// ErrMalformedBody is returned for unrecoverable format problems surfaced by
// pion's parser.
var (
	ErrEmptyBody    = errors.New("sdp: empty body")
	ErrMalformedBody = errors.New("sdp: malformed body")
)
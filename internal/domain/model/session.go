// Package model — Session.
package model

import (
	"fmt"
	"strings"
)

// MediaStream describes one m= block in an SDP session: a media type
// (e.g. "video", "audio"), the transport port, the protocol ("RTP/AVP")
// and the format list ("96 97 98").
type MediaStream struct {
	MediaType string
	Port      int
	Protocol  string
	Formats   []string
}

// Validate reports any structural problem with the MediaStream. Empty
// fields are all errors so a malformed stream never silently slips into
// a Session.
func (m MediaStream) Validate() error {
	if m.MediaType == "" {
		return fmt.Errorf("model: MediaStream.MediaType is empty")
	}
	if m.Port < 0 || m.Port > 65535 {
		return fmt.Errorf("model: MediaStream.Port %d out of range", m.Port)
	}
	if m.Protocol == "" {
		return fmt.Errorf("model: MediaStream.Protocol is empty")
	}
	if len(m.Formats) == 0 {
		return fmt.Errorf("model: MediaStream.Formats is empty")
	}
	return nil
}

// Session is an immutable SDP session description (RFC 4566 + GB/T 28181
// §K.2 extensions: y= and f= lines are kept as opaque extensions and
// re-emitted on Marshal). Constructors panic on missing mandatory fields.
type Session struct {
	origin      string
	sessionName string
	connection  string
	streams     []MediaStream
	extensions  []string // verbatim y= / f= lines, in order
}

// NewSession builds a Session. streams are copied defensively; extensions
// are copied and validated to contain no CR/LF (which would corrupt the
// wire format).
func NewSession(origin, sessionName, connection string, streams []MediaStream, extensions []string) Session {
	if origin == "" {
		panic("model: Session.Origin is empty")
	}
	if sessionName == "" {
		panic("model: Session.SessionName is empty")
	}
	if connection == "" {
		panic("model: Session.ConnectionAddress is empty")
	}
	if len(streams) == 0 {
		panic("model: Session has no MediaStreams")
	}
	for i, s := range streams {
		if err := s.Validate(); err != nil {
			panic(fmt.Sprintf("model: stream[%d]: %v", i, err))
		}
	}
	ec := append([]string(nil), extensions...)
	for i, e := range ec {
		if strings.ContainsAny(e, "\r\n") {
			panic(fmt.Sprintf("model: extensions[%d] contains CR/LF", i))
		}
	}
	sc := append([]MediaStream(nil), streams...)
	return Session{
		origin:      origin,
		sessionName: sessionName,
		connection:  connection,
		streams:     sc,
		extensions:  ec,
	}
}

// Origin returns the o= line verbatim.
func (s Session) Origin() string { return s.origin }

// SessionName returns the s= line verbatim.
func (s Session) SessionName() string { return s.sessionName }

// ConnectionAddress returns the c= line verbatim.
func (s Session) ConnectionAddress() string { return s.connection }

// Streams returns a defensive copy of the media streams slice.
func (s Session) Streams() []MediaStream {
	cp := append([]MediaStream(nil), s.streams...)
	return cp
}

// Extensions returns the GB/T 28181 §K.2 extension lines (y=, f=) in their
// original order.
func (s Session) Extensions() []string {
	cp := append([]string(nil), s.extensions...)
	return cp
}

// String returns a one-line debug summary.
func (s Session) String() string {
	return fmt.Sprintf("SDP<%s streams=%d exts=%d>",
		s.sessionName, len(s.streams), len(s.extensions))
}

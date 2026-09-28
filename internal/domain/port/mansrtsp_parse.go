// Package mansrtsp parses the MANSRTSP bodies carried inside SIP INFO
// requests (GB/T 28181-2022 Annex G). The protocol reuses RTSP syntax:
// a request line ("PLAY RTSP/1.0"), CSeq and Scale headers, and an
// "npt=" Range for seeking. The parser turns the text form into the
// structured Command the acceptor maps onto PlaybackPort calls.
package port

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Command is the MANSRTSP request line verb.
type Command string

const (
	// CommandPlay resumes or starts playback, optionally seeking and/or
	// changing the rate.
	CommandPlay Command = "PLAY"
	// CommandPause suspends playback without tearing the session down.
	CommandPause Command = "PAUSE"
)

// Body is one parsed MANSRTSP request.
type Body struct {
	// Command is PLAY or PAUSE; anything else fails to parse.
	Command Command
	// CSeq is the RTSP-level sequence echoed for correlation. Zero when
	// the body carried no CSeq header.
	CSeq int
	// Scale is the requested playback rate: 1.0 normal (the default when
	// the header is absent), 0 paused, negative reverse.
	Scale float64
	// SeekFrom is the position playback should jump to, in seconds, when
	// the body carried a "Range: npt=<start>-" header. Zero means no seek.
	// SeekTo carries the optional end of the range (negative when absent).
	SeekFrom float64
	SeekTo   float64
	// HasSeek reports whether a Range header was present at all — a seek
	// to 0 and no seek are different requests.
	HasSeek bool
}

// Parse turns a MANSRTSP body into a Body. It returns an error for empty
// bodies, unknown request lines, and Scale values that are not plain
// finite numbers. Line endings may be CRLF or LF; a trailing blank line
// is optional.
func Parse(body string) (Body, error) {
	text := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// Trim trailing empty lines so a body that ends with a blank line
	// (the RTSP message terminator) parses the same as one without.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return Body{}, fmt.Errorf("mansrtsp: empty body")
	}

	fields := strings.Fields(strings.TrimSpace(lines[0]))
	if len(fields) == 0 {
		return Body{}, fmt.Errorf("mansrtsp: empty request line")
	}
	out := Body{Command: Command(strings.ToUpper(fields[0])), Scale: 1.0}
	switch out.Command {
	case CommandPlay, CommandPause:
	default:
		return Body{}, fmt.Errorf("mansrtsp: unsupported command %q", fields[0])
	}

	for _, line := range lines[1:] {
		name, value, ok := splitHeader(line)
		if !ok {
			continue
		}
		switch name {
		case "CSEQ":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return Body{}, fmt.Errorf("mansrtsp: bad CSeq %q: %w", value, err)
			}
			out.CSeq = n
		case "SCALE":
			s, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || math.IsNaN(s) || math.IsInf(s, 0) {
				return Body{}, fmt.Errorf("mansrtsp: bad Scale %q", value)
			}
			out.Scale = s
		case "RANGE":
			from, to, err := parseNPT(value)
			if err != nil {
				return Body{}, fmt.Errorf("mansrtsp: bad Range %q: %w", value, err)
			}
			out.HasSeek = true
			out.SeekFrom = from
			out.SeekTo = to
		}
	}
	return out, nil
}

// splitHeader splits "Name: value" on the first colon. A line without a
// colon (or an empty name) is not a header; the caller skips it.
func splitHeader(line string) (string, string, bool) {
	i := strings.Index(line, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.ToUpper(strings.TrimSpace(line[:i])), line[i+1:], true
}

// parseNPT reads "npt=<start>-<end>" where both ends are seconds (a bare
// float), "now", or omitted ("npt=10-" or "npt=-"). The returned end is
// negative when the range has no end.
func parseNPT(value string) (float64, float64, error) {
	v := strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(v), "npt=") {
		return 0, 0, fmt.Errorf("missing npt= prefix")
	}
	span := v[len("npt="):]
	dash := strings.Index(span, "-")
	if dash < 0 {
		return 0, 0, fmt.Errorf("missing range separator")
	}
	from, err := parseNPTEndpoint(strings.TrimSpace(span[:dash]), true)
	if err != nil {
		return 0, 0, err
	}
	to := -1.0
	if tail := strings.TrimSpace(span[dash+1:]); tail != "" {
		to, err = parseNPTEndpoint(tail, false)
		if err != nil {
			return 0, 0, err
		}
	}
	return from, to, nil
}

// parseNPTEndpoint reads one end of an npt range: a bare number of
// seconds or "now" (only valid at the start).
func parseNPTEndpoint(s string, allowNow bool) (float64, error) {
	if strings.EqualFold(s, "now") {
		if !allowNow {
			return 0, fmt.Errorf("now is only valid as the start")
		}
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("bad npt value %q", s)
	}
	return f, nil
}

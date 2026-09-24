package sdp

import (
	"bufio"
	"errors"
	"strings"

	pionsdp "github.com/pion/sdp"
)

// Parse reads an SDP body and returns a GB/T 28181 §K-aware Session.
//
// Lines starting with `y=` or `f=` (and not preceded by `a=`) are stripped
// before being handed to pion/sdp, then attached to the matching media
// block in the returned Session. RFC 4566 lines flow through unchanged.
//
// The function returns ErrEmptyBody for whitespace-only input and
// ErrMalformedBody for any error surfaced by pion's Unmarshal (wrapped
// so callers can errors.Is against it).
func Parse(text string) (*Session, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyBody
	}

	// Step 1: line-level extract of §K.2 extensions.
	klines, remainder := extractKLines(text)

	// Step 2: pion parses the remainder.
	pd := &pionsdp.SessionDescription{}
	if err := pd.Unmarshal(remainder); err != nil {
		// pion returns github.com/pkg/errors errors; wrap as our sentinel
		// so callers can errors.Is regardless of pion's internals.
		return nil, errors.Join(ErrMalformedBody, err)
	}

	// Step 3: build our Session with one MediaBlock per pion media
	// description, then fold klines back in by blockIdx.
	sess := &Session{
		SessionDescription: pd,
		Media:              make([]*MediaBlock, 0, len(pd.MediaDescriptions)),
	}
	for _, md := range pd.MediaDescriptions {
		sess.Media = append(sess.Media, &MediaBlock{
			MediaDescription: md,
		})
	}
	for _, k := range klines {
		if k.blockIdx < 0 || k.blockIdx >= len(sess.Media) {
			// Orphan — silently drop per §K.2.
			continue
		}
		mb := sess.Media[k.blockIdx]
		switch k.kind {
		case "y":
			mb.SSRC = k.value
		case "f":
			mb.MediaOption = k.value
		}
	}
	return sess, nil
}

// kLine captures one §K extension row before it is hidden from pion.
type kLine struct {
	// blockIdx is the 0-based index of the media block this line
	// belongs to. blockIdx == -1 means "before any m= line" — pion
	// would not place §K.2 lines there, and we drop them.
	blockIdx int

	// rawLine is the verbatim line, without trailing CRLF.
	rawLine string

	// kind is "y" or "f".
	kind string

	// value is what follows the "=" sign.
	value string
}

// extractKLines scans the body line by line and splits out every line whose
// first character is "y" or "f" (and second character is "="), returning
// the §K lines in textual order with their owning media-block index plus
// the pion-friendly remainder of the body.
//
// Rules:
//   - y= and f= lines may appear inside a media block only (after the
//     matching m= line and before the next m= line). Lines before the
//     first m= are tagged blockIdx=-1 and dropped.
//   - "a=y:..." and "a=f:..." are RFC 4566 attributes and MUST be left
//     intact for pion — we only match bare "y=" / "f=" at line start.
//   - Empty `y=` or `f=` (no value after "=") is recorded as a kLine with
//     empty value. Marshalling omits empty y= per spec scenario
//     "未设 SSRC 时不输出空 y= 行".
//   - CRLF and LF are tolerated transparently — bufio strips the trailing
//     \n, we additionally strip a trailing \r on each line.
func extractKLines(body string) ([]kLine, string) {
	var ks []kLine
	var rem strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	blockIdx := -1
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimRight(line, "\r")

		switch {
		case strings.HasPrefix(line, "m="):
			blockIdx++
			rem.WriteString(line)
			rem.WriteByte('\n')
		case isBareKLine(line, "y"):
			ks = append(ks, kLine{
				blockIdx: blockIdx,
				rawLine:  line,
				kind:     "y",
				value:    strings.TrimPrefix(line, "y="),
			})
		case isBareKLine(line, "f"):
			ks = append(ks, kLine{
				blockIdx: blockIdx,
				rawLine:  line,
				kind:     "f",
				value:    strings.TrimPrefix(line, "f="),
			})
		default:
			rem.WriteString(line)
			rem.WriteByte('\n')
		}
	}
	return ks, rem.String()
}

// isBareKLine reports whether line is a top-level "y=..." or "f=..."
// (not nested inside an "a=" attribute).
func isBareKLine(line, kind string) bool {
	prefix := kind + "="
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	// Reject "a=y=..." or "a=f=..." — those are RFC 4566 attributes.
	if strings.HasPrefix(line, "a=") {
		return false
	}
	return true
}
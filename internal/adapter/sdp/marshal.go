package sdp

import (
	"strings"
)

// Marshal renders a Session back to a wire-format SDP body. Per
// GB/T 28181-2016 §K.2 every `m=` block MUST have `y=` then `f=` appended
// after the `m=` and its `a=` attribute lines and before the next `m=` line.
//
// Empty SSRC is omitted (spec scenario "未设 SSRC 时不输出空 y= 行") while
// empty MediaOption is omitted too.
//
// Implementation note: pion/sdp renders all media blocks back-to-back with
// no blank line between them, so a "\n\n" split does NOT isolate blocks.
// We therefore scan line by line and insert the per-block §K pair at the
// point just before the next `m=` line (or at end-of-body).
func Marshal(s *Session) (string, error) {
	if s == nil || s.SessionDescription == nil {
		return "", ErrEmptyBody
	}

	// Step 1: have pion render RFC 4566 core (CRLF-terminated).
	raw := s.SessionDescription.Marshal()

	if len(s.Media) == 0 {
		// No media blocks: nothing to inject.
		return raw, nil
	}

	// Step 2: build per-block §K line pairs in declaration order.
	blockLines := make([]string, len(s.Media))
	for i, mb := range s.Media {
		var klines []string
		if mb.SSRC != "" {
			klines = append(klines, "y="+mb.SSRC)
		}
		if mb.MediaOption != "" {
			klines = append(klines, "f="+mb.MediaOption)
		}
		blockLines[i] = strings.Join(klines, "\r\n")
	}

	// Step 3: walk lines; record the index of every "m=" line. A §K pair
	// for block N goes immediately before the (N+1)-th "m=" line, or at
	// EOF if N is the last block. We insert right after the last non-blank
	// line of the block, which is equivalent to "just before the next m=
	// line" because pion does not emit blank lines between media blocks.
	lines := strings.Split(raw, "\r\n")
	// Find insertion points: index i in `lines` where we flush blockLines[k].
	mIndexes := make([]int, 0, len(s.Media))
	for i, ln := range lines {
		if strings.HasPrefix(ln, "m=") {
			mIndexes = append(mIndexes, i)
		}
	}

	var out strings.Builder
	nextBlock := 0
	for i, ln := range lines {
		// Before emitting the (nextBlock+1)-th "m=" line, flush the §K
		// pair of block `nextBlock` (which ended just before this point).
		if nextBlock < len(blockLines) &&
			nextBlock < len(mIndexes) && i == mIndexes[nextBlock] {
			// This is the first m= line — no block has ended yet, so
			// there is nothing to flush before it (block index 0 has
			// not started). Skip.
		}
		out.WriteString(ln)
		if i < len(lines)-1 {
			out.WriteString("\r\n")
		}

		// After emitting the last line of block `nextBlock`, flush its
		// §K pair if the pair is non-empty.
		// Block N ends right before block N+1's m= line, or at EOF.
		blockEndsHere := false
		if nextBlock < len(blockLines) {
			if nextBlock+1 < len(mIndexes) {
				if i+1 == mIndexes[nextBlock+1] {
					blockEndsHere = true
				}
			} else if i == len(lines)-1 {
				blockEndsHere = true
			}
		}
		if blockEndsHere {
			if pair := blockLines[nextBlock]; pair != "" {
				out.WriteString(pair)
				out.WriteString("\r\n")
			}
			nextBlock++
		}
	}
	// Any remaining blocks (shouldn't happen) get appended at EOF.
	for ; nextBlock < len(blockLines); nextBlock++ {
		if pair := blockLines[nextBlock]; pair != "" {
			out.WriteString(pair)
			out.WriteString("\r\n")
		}
	}
	return out.String(), nil
}

// MustMarshal calls Marshal and panics on error, intended for test fixtures
// where the caller already knows the input is valid.
func MustMarshal(s *Session) string {
	out, err := Marshal(s)
	if err != nil {
		panic(err)
	}
	return out
}

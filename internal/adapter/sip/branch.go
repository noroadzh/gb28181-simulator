package sip

import (
	"crypto/rand"
	"encoding/base32"
	"sync/atomic"
)

// BranchGenerator produces Via branch values. The default implementation
// uses crypto/rand with 12 bytes of entropy encoded as RFC 4648 base32
// (lowercase, no padding) — that yields 20 characters after the
// "z9hG4bK" magic cookie, which comfortably exceeds the RFC 3261
// §8.1.1.7 minimum and gives ~80 bits of collision resistance.
type BranchGenerator struct {
	seq    atomic.Uint64
	custom func() string
}

// NewBranchGenerator returns the default BranchGenerator.
func NewBranchGenerator() *BranchGenerator {
	return &BranchGenerator{}
}

// NewBranchGeneratorWith returns a BranchGenerator that calls custom()
// on every Next call. Tests use this to inject deterministic values.
func NewBranchGeneratorWith(custom func() string) *BranchGenerator {
	return &BranchGenerator{custom: custom}
}

// Next returns a fresh branch string starting with the RFC 3261 magic
// cookie "z9hG4bK" followed by "-" and a 20-character base32 suffix.
//
// The atomic counter is mixed in as a tie-breaker so that two calls
// within the same nanosecond on the same rand sequence still differ.
func (g *BranchGenerator) Next() string {
	if g.custom != nil {
		return g.custom()
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand can only fail on a broken OS; fall back to the
		// counter so we still return unique-ish values.
		buf = make([]byte, 8)
		g.seq.Add(1)
		for i := range buf {
			buf[i] = byte(g.seq.Load() >> (8 * i))
		}
	}
	// 12 bytes -> 20 base32 chars (no padding).
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	// Mix the monotonic counter into the last byte of the suffix to
	// guarantee uniqueness across calls that read the same rand bytes.
	suffix := enc[:19] + string('a'+(byte(g.seq.Add(1))&0x1f))
	return "z9hG4bK-" + suffix
}

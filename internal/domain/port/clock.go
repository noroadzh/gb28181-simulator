// Package port — Clock.
package port

import "time"

// Clock abstracts time.Now so time-sensitive use cases (nonce TTL, retry
// deadlines, audit timestamps) can be unit-tested deterministically.
// Production code uses platform/clock.Real(); tests use platform/clock.Fake().
type Clock interface {
	// Now returns the current wall-clock time.
	Now() time.Time
}
// Package clock provides an injectable time source for use-case code that
// must remain deterministic in tests (nonce TTL, audit timestamps, retry
// backoff). Production wiring calls Real() to get the system clock;
// tests construct a Fake() via NewFake and call Set to advance time.
package clock

import (
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Real returns a Clock backed by time.Now. Always non-nil.
func Real() port.Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Fake is a manually-advanced Clock. Use NewFake to obtain one and Set or
// Advance to mutate time. The zero value is not usable; always go through
// NewFake so internal mutex is initialised.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake clock pinned to the supplied initial time.
func NewFake(initial time.Time) *Fake {
	return &Fake{now: initial}
}

// Now returns the currently-set time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set pins the clock to t. Useful for "now is exactly 2026-09-23T00:00:00Z".
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// Advance moves the clock forward by d. Negative d is allowed (rewinds).
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Compile-time check that Fake satisfies the domain port.
var _ port.Clock = (*Fake)(nil)
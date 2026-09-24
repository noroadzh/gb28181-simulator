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

// Ticker is an injectable periodic signal. Use-case code that must wake up
// on a schedule — a device keepalive, a registration renewal — depends on
// this instead of time.NewTicker, so a test can fire the schedule by hand
// and a shutdown can stop it without waiting for the next tick.
type Ticker interface {
	// C is the channel a tick is delivered on. It behaves like
	// time.Ticker.C: at most one pending tick, never closed.
	C() <-chan time.Time
	// Stop releases the ticker's resources. It is idempotent and does not
	// close C.
	Stop()
}

// TickerFactory creates a Ticker for d. Injected so tests can substitute a
// scripted ticker without touching the use case's logic.
type TickerFactory func(d time.Duration) Ticker

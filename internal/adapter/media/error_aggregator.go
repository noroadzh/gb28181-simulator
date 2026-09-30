// Package media — error aggregation for hot media loops.
//
// The media pipeline moves thousands of RTP packets per second. Logging every
// recoverable per-packet error there would flood the file sink and the Hub
// subscribers (the Web UI would be unusable), so repeated failures of the same
// kind are counted instead and reported once per aggregation window.
package media

import (
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// DefaultWindow is how long an error signature accumulates before the
// aggregator emits a summary line. It matches the contract in
// openspec/changes/enhance-logging-coverage: 60 seconds.
const DefaultWindow = 60 * time.Second

// aggregatorLog is the component tag used for every aggregated record. It
// matches the "internal/adapter/media" module path so `log.modules` overrides
// apply to these lines too.
var aggregatorLog = logging.L().With("component", "internal/adapter/media")

// ErrorAggregator counts repeated errors by signature and reports one summary
// record per signature per window.
//
// A signature is the caller's own key (typically a stable string such as
// "rtp-send" or "ps-mux"). Errors sharing a key are considered the same kind
// of failure for aggregation purposes.
type ErrorAggregator struct {
	mu     sync.Mutex
	window time.Duration
	// entries maps signature → current accumulation window.
	entries map[string]*aggEntry
	// now is injectable for tests; defaults to time.Now.
	now func() time.Time
	// closed stops new windows from being reported after Flush.
	closed bool
}

type aggEntry struct {
	count     int
	firstSeen time.Time
	lastSeen  time.Time
	// sample is the most recent error text, reported in the summary so the
	// operator can see what actually failed without drowning in repeats.
	sample string
}

// NewErrorAggregator returns an aggregator using DefaultWindow.
func NewErrorAggregator() *ErrorAggregator {
	return NewErrorAggregatorWithWindow(DefaultWindow)
}

// NewErrorAggregatorWithWindow returns an aggregator with a custom window. A
// non-positive window is clamped to DefaultWindow.
func NewErrorAggregatorWithWindow(window time.Duration) *ErrorAggregator {
	if window <= 0 {
		window = DefaultWindow
	}
	return &ErrorAggregator{
		window:  window,
		entries: make(map[string]*aggEntry),
		now:     time.Now,
	}
}

// Record counts one occurrence of err under signature. It never logs: the
// whole point is to keep the hot loop quiet.
//
// Safe for concurrent use.
func (a *ErrorAggregator) Record(signature string, err error) {
	if a == nil || signature == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	e, ok := a.entries[signature]
	if !ok {
		a.entries[signature] = &aggEntry{
			count:     1,
			firstSeen: now,
			lastSeen:  now,
			sample:    errText(err),
		}
		return
	}
	// Window elapsed: report the finished window and start a fresh one so a
	// persistent failure produces one line per window rather than forever.
	if now.Sub(e.firstSeen) >= a.window {
		a.reportLocked(signature, e)
		a.entries[signature] = &aggEntry{
			count:     1,
			firstSeen: now,
			lastSeen:  now,
			sample:    errText(err),
		}
		return
	}
	e.count++
	e.lastSeen = now
	if err != nil {
		e.sample = errText(err)
	}
}

// Sweep drains any window that has elapsed and emits its summary. Callers
// running a background ticker (e.g. once a minute) use this to close out
// windows for errors that stopped arriving, so a burst is still reported even
// though no new error triggered the rollover.
//
// It is safe to call Sweep concurrently with Record.
func (a *ErrorAggregator) Sweep() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	now := a.now()
	for sig, e := range a.entries {
		if now.Sub(e.firstSeen) >= a.window {
			a.reportLocked(sig, e)
			delete(a.entries, sig)
		}
	}
}

// Snapshot returns a copy of the current per-signature counts. It is intended
// for tests that need to assert which signatures have been recorded without
// driving a sweep. Safe for concurrent use.
func (a *ErrorAggregator) Snapshot() map[string]int {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]int, len(a.entries))
	for sig, e := range a.entries {
		out[sig] = e.count
	}
	return out
}

// Flush reports every active window immediately and stops accepting new
// records. Call at shutdown so the operator does not lose the last burst.
func (a *ErrorAggregator) Flush() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	for sig, e := range a.entries {
		if e.count == 0 {
			continue
		}
		a.reportLocked(sig, e)
	}
	a.entries = map[string]*aggEntry{}
	a.closed = true
}

// reportLocked emits a single warn-level summary record. Caller holds a.mu.
func (a *ErrorAggregator) reportLocked(signature string, e *aggEntry) {
	aggregatorLog.Warn(
		"media error aggregated",
		"signature", signature,
		"count", e.count,
		"first_seen", e.firstSeen,
		"last_seen", e.lastSeen,
		"sample", e.sample,
	)
}

// errText returns a stable string representation of err, never the empty
// string — callers expect a non-empty "sample" in the summary.
func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
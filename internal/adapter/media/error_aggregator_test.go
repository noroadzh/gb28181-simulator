package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

func TestErrorAggregator_WindowRollsOver(t *testing.T) {
	var buf bytes.Buffer
	h := logging.NewMultiHandler(slog.LevelDebug, nil, false, nil, &nopCloserBuf{&buf})
	logger := slog.New(h).With("component", "internal/adapter/media")
	// Swap the package global to a logger backed by our buffer.
	oldLog := aggregatorLog
	aggregatorLog = logger
	defer func() { aggregatorLog = oldLog }()

	clock := time.Unix(0, 0)
	a := NewErrorAggregatorWithWindow(50 * time.Millisecond)
	a.now = func() time.Time { return clock }

	a.Record("rtp-send", errors.New("first"))
	a.Record("rtp-send", errors.New("second"))
	clock = clock.Add(60 * time.Millisecond) // window elapsed
	a.Record("rtp-send", errors.New("third")) // triggers rollover + fresh count

	out := buf.String()
	if !strings.Contains(out, "media error aggregated") {
		t.Fatalf("expected aggregated record, got %s", out)
	}
	if !strings.Contains(out, `"count":2`) {
		t.Fatalf("expected count=2 in rolled-over window, got %s", out)
	}
	// The third error opens a new window — no second aggregate yet.
	if strings.Count(out, "media error aggregated") != 1 {
		t.Fatalf("expected exactly one aggregated record so far, got %s", out)
	}
}

func TestErrorAggregator_SweepFlushesStaleWindow(t *testing.T) {
	var buf bytes.Buffer
	h := logging.NewMultiHandler(slog.LevelDebug, nil, false, nil, &nopCloserBuf{&buf})
	logger := slog.New(h).With("component", "internal/adapter/media")
	oldLog := aggregatorLog
	aggregatorLog = logger
	defer func() { aggregatorLog = oldLog }()

	clock := time.Unix(0, 0)
	a := NewErrorAggregatorWithWindow(50 * time.Millisecond)
	a.now = func() time.Time { return clock }
	a.Record("ps-mux", errors.New("oops"))
	// No further records; advancing the clock past the window without
	// touching Record must not lose the burst.
	clock = clock.Add(70 * time.Millisecond)
	a.Sweep()
	if !strings.Contains(buf.String(), "media error aggregated") {
		t.Fatalf("expected Sweep to flush the stale window, got %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"signature":"ps-mux"`) {
		t.Fatalf("expected ps-mux signature in summary, got %s", buf.String())
	}
}

func TestErrorAggregator_FlushReportsActiveAndStopsAccepting(t *testing.T) {
	var buf bytes.Buffer
	h := logging.NewMultiHandler(slog.LevelDebug, nil, false, nil, &nopCloserBuf{&buf})
	logger := slog.New(h).With("component", "internal/adapter/media")
	oldLog := aggregatorLog
	aggregatorLog = logger
	defer func() { aggregatorLog = oldLog }()

	clock := time.Unix(0, 0)
	a := NewErrorAggregatorWithWindow(time.Hour)
	a.now = func() time.Time { return clock }
	a.Record("rtp-send", errors.New("a"))
	a.Record("rtp-send", errors.New("b"))
	a.Record("ps-mux", errors.New("c"))

	a.Flush()
	out := buf.String()
	if !strings.Contains(out, "media error aggregated") {
		t.Fatalf("expected flush to emit summary, got %s", out)
	}
	if strings.Count(out, "media error aggregated") != 2 {
		t.Fatalf("expected two distinct signatures summarised, got %s", out)
	}

	buf.Reset()
	a.Record("rtp-send", errors.New("after-flush"))
	if buf.Len() != 0 {
		t.Fatalf("Flush must stop further reporting, but got %s", out)
	}
}

// nopCloserBuf adapts bytes.Buffer to io.WriteCloser for tests.
type nopCloserBuf struct{ *bytes.Buffer }

func (n *nopCloserBuf) Close() error { return nil }

// decodeJSONLines keeps the test self-contained; mirrors decodeRecord elsewhere.
func decodeJSONLines(b []byte) []map[string]any {
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// Ensure _context stays referenced for go vet consistency.
var _ = context.Background
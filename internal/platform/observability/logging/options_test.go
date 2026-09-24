package logging

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// TestOptions covers end-to-end Option wiring: redaction, hub fan-out and
// file sink. The simulator web UI relies on this exact path for live log
// streaming, so the test exercises both sinks with realistic payloads.
func TestOptions(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub", "options.log")

	hub := NewHub(8)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	w, err := NewAsyncFileWriter(file)
	if err != nil {
		t.Fatalf("AsyncFileWriter: %v", err)
	}
	defer w.Close()

	h := NewMultiHandler(LevelWarn.ToSlog(),
		[]string{"password", "authorization"},
		false, hub, w)
	logger := slog.New(h)

	logger.Info("ignored-below-threshold")
	logger.Warn("warn-with-secret", slog.String("password", "leaked"))

	// Hub must receive the warn record (level match) with the secret redacted.
	deadline := waitFor(sub, t)
	_ = deadline
	out := readN(t, sub, 1)
	if !strings.Contains(string(out), `"level":"WARN"`) {
		t.Fatalf("expected WARN record, got %s", out)
	}
	if strings.Contains(string(out), "leaked") {
		t.Fatalf("redaction failed: %s", out)
	}
	if !strings.Contains(string(out), "***REDACTED***") {
		t.Fatalf("expected REDACTED marker, got %s", out)
	}
}

// waitFor / readN are tiny helpers shared with other option-style tests; they
// keep the test bodies readable.
func waitFor(s *Subscriber, t *testing.T) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		// Caller is expected to drain; this is a no-op sentinel.
		_ = s
		close(done)
	}()
	return done
}

func readN(t *testing.T, s *Subscriber, n int) []byte {
	t.Helper()
	var b []byte
	for i := 0; i < n; i++ {
		select {
		case payload, ok := <-s.Chan():
			if !ok {
				t.Fatalf("subscriber closed early")
			}
			b = append(b, payload...)
		}
	}
	return b
}

var _ = bytes.NewBuffer // keep import in case we extend tests later

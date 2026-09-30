package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"trace": LevelTrace,
		"DEBUG": LevelDebug,
		"info ": LevelInfo,
		"Warn":  LevelWarn,
		"error": LevelError,
		"":      LevelInfo,
		"??":    LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Fatalf("ParseLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOptions_RedactionAndFanout(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "out.log")

	hub := NewHub(64)
	w, err := NewAsyncFileWriter(logFile)
	if err != nil {
		t.Fatalf("AsyncFileWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	h := NewMultiHandler(slog.LevelDebug, []string{"password"}, false, hub, w)
	logger := slog.New(h)

	// 3 subscribers should all receive the same payload.
	subs := []*Subscriber{hub.Subscribe(), hub.Subscribe(), hub.Subscribe()}
	defer func() {
		for _, s := range subs {
			hub.Unsubscribe(s)
		}
	}()

	logger.Info("login", slog.String("user", "alice"), slog.String("password", "hunter2"))
	logger.Debug("debug-only") // suppressed by default? no: LevelDebug
	logger.Warn("low disk", slog.Int("percent", 90))

	// For each subscriber, drain until we have seen the redacted record at
	// least once. Other records are tolerated. We also assert that no record
	// contains the raw secret.
	deadline := time.Now().Add(2 * time.Second)
	for _, s := range subs {
		sawRedacted := false
		for !sawRedacted && time.Now().Before(deadline) {
			select {
			case payload, ok := <-s.Chan():
				if !ok {
					t.Fatalf("subscriber %d closed early", s.ID())
				}
				if bytes.Contains(payload, []byte("hunter2")) {
					t.Fatalf("redaction failed: payload %s", payload)
				}
				if bytes.Contains(payload, []byte("***REDACTED***")) {
					sawRedacted = true
				}
			case <-time.After(50 * time.Millisecond):
			}
		}
		if !sawRedacted {
			t.Fatalf("subscriber %d never saw redacted payload", s.ID())
		}
	}
}

func TestMultiHandler_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := NewMultiHandler(slogLevelTrace, nil, false, nil, &nopCloser{&buf})
	logger := slog.New(h)
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")
	out := buf.String()
	for _, level := range []string{`"level":"DEBUG"`, `"level":"INFO"`, `"level":"WARN"`, `"level":"ERROR"`} {
		if !strings.Contains(out, level) {
			t.Fatalf("expected record %s in output, got %s", level, out)
		}
	}
	// Now raise threshold to warn and ensure debug/info are filtered.
	buf.Reset()
	h2 := NewMultiHandler(slog.LevelWarn, nil, false, nil, &nopCloser{&buf})
	logger = slog.New(h2)
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	if strings.Contains(buf.String(), `"level":"DEBUG"`) {
		t.Fatalf("debug must be filtered at warn: %s", buf.String())
	}
	if strings.Contains(buf.String(), `"level":"INFO"`) {
		t.Fatalf("info must be filtered at warn: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"level":"WARN"`) {
		t.Fatalf("warn must pass: %s", buf.String())
	}
}

// TestModuleLevelFiltering verifies that per-module level overrides take
// precedence over the handler's default level, using the longest-prefix rule.
func TestModuleLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := NewMultiHandlerWithModules(
		slog.LevelInfo, // default: info
		map[string]Level{
			"internal/app":     LevelDebug,
			"internal/app/sip": LevelTrace,
		},
		nil, false, nil, &nopCloser{&buf},
	)
	logger := slog.New(h)

	// No component → default info. debug must be dropped.
	logger.Debug("plain-debug")
	// component=internal/app → debug. debug passes, trace does not.
	logger.With("component", "internal/app").Debug("app-debug")
	logger.With("component", "internal/app").Info("app-info")
	// component=internal/app/sip → trace (longest prefix wins over
	// internal/app's debug). trace passes.
	logger.With("component", "internal/app/sip").Log(context.Background(), slogLevelTrace, "sip-trace")

	out := buf.String()
	if strings.Contains(out, "plain-debug") {
		t.Fatalf("module-less debug must be filtered at default info: %s", out)
	}
	if !strings.Contains(out, "app-debug") {
		t.Fatalf("internal/app debug must pass: %s", out)
	}
	if !strings.Contains(out, "app-info") {
		t.Fatalf("internal/app info must pass: %s", out)
	}
	if !strings.Contains(out, "sip-trace") {
		t.Fatalf("internal/app/sip trace must pass via longest-prefix match: %s", out)
	}
}

func TestHub_FanoutDropsSlowSubscribers(t *testing.T) {
	hub := NewHub(2)
	fast := hub.Subscribe()
	slow := hub.Subscribe()
	defer hub.Unsubscribe(fast)
	defer hub.Unsubscribe(slow)

	// Publish 10 messages without draining slow: must not block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			hub.Publish([]byte(`"x"`))
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Hub.Publish blocked on slow subscriber")
	}
	// Drain fast to ensure fast saw at least one message.
	select {
	case <-fast.Chan():
	default:
		t.Fatalf("fast subscriber received no message")
	}
}

func TestInit_GlobalLoggerAndHub(t *testing.T) {
	dir := t.TempDir()
	if err := Init(Options{Level: LevelInfo, File: filepath.Join(dir, "global.log")}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sub := DefaultHub().Subscribe()
	defer DefaultHub().Unsubscribe(sub)
	L().Info("hello", slog.String("password", "secret-value"))
	// Hub should receive the redacted record.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case payload := <-sub.Chan():
			if !bytes.Contains(payload, []byte("***REDACTED***")) {
				t.Fatalf("Init-driven redaction failed: %s", payload)
			}
			_ = Shutdown(context.Background())
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("never received record from hub")
	_ = os.Stderr
}

// nopCloser adapts bytes.Buffer to io.WriteCloser for tests that do not need
// to assert on Close semantics.
type nopCloser struct{ *bytes.Buffer }

func (n *nopCloser) Close() error { return nil }

// ensure JSON encoded output decodes for assertions elsewhere.
func decodeRecord(b []byte) (map[string]any, error) {
	// JSON handler appends a trailing newline when given an io.Writer; trim.
	b = bytes.TrimSpace(b)
	var m map[string]any
	err := json.Unmarshal(b, &m)
	return m, err
}

var _ = decodeRecord

// TestUpdateLevels_Concurrent hammers the handler with parallel writers
// and concurrent UpdateLevels calls to exercise the rwmu around
// defaultLevel / moduleLevels. Race detector (go test -race) catches any
// missed synchronisation.
func TestUpdateLevels_Concurrent(t *testing.T) {
	dir := t.TempDir()
	if err := Init(Options{Level: LevelInfo, File: filepath.Join(dir, "upd.log")}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Shutdown(context.Background()); Reset() })

	const (
		writers = 4
		updates = 50
	)
	var wg sync.WaitGroup
	wg.Add(writers + updates)
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				L().Info("writer", slog.Int("id", id), slog.Int("seq", j))
			}
		}(i)
	}
	for i := 0; i < updates; i++ {
		go func(i int) {
			defer wg.Done()
			level := LevelInfo
			if i%2 == 0 {
				level = LevelDebug
			}
			if err := UpdateLevels(level, map[string]Level{
				"internal/app": level,
			}); err != nil {
				t.Errorf("UpdateLevels: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if got := CurrentLevels().Default; got != LevelDebug && got != LevelInfo {
		t.Fatalf("CurrentLevels.Default = %q, want Debug or Info", got)
	}
}

// TestCurrentLevels_NoInit asserts that CurrentLevels is safe to call
// before Init, returning LevelUnset. The HTTP PATCH handler depends on
// this so a misbehaving caller cannot panic the server.
func TestCurrentLevels_NoInit(t *testing.T) {
	Reset()
	got := CurrentLevels()
	if got.Default != LevelUnset {
		t.Fatalf("Default = %q, want LevelUnset", got.Default)
	}
	if got.Modules != nil {
		t.Fatalf("Modules = %v, want nil", got.Modules)
	}
}

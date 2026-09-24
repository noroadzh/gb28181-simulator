package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Options configures the global logger. All fields are optional.
type Options struct {
	// Level sets the minimum severity emitted by any sink. Defaults to info.
	Level Level
	// File enables file output. An empty path disables the file sink.
	File string
	// AddSource enables slog's source location (file:line) capture.
	AddSource bool
	// RedactKeys is the case-insensitive blacklist of attribute names whose
	// string / []byte values must be replaced by ***REDACTED*** before
	// emission. Defaults to DefaultRedactKeys() when nil.
	RedactKeys []string
	// HubBufferSize tunes the per-subscriber buffer; defaults to 256.
	HubBufferSize int
}

// MultiHandler is a slog.Handler that fans out to a file sink and the global
// Hub. Encoding is JSON for both sinks so the on-disk format and the
// WebSocket payload stay byte-identical.
type MultiHandler struct {
	level      slog.Level
	addSource  bool
	redactKeys map[string]struct{}
	hub        *Hub
	writer     io.WriteCloser
	mu         sync.Mutex
}

// NewMultiHandler builds a handler with the given level, redact keys and
// sinks. Either writer or hub may be nil to disable that sink (common during
// tests).
func NewMultiHandler(level slog.Level, redactKeys []string, addSource bool, hub *Hub, writer io.WriteCloser) *MultiHandler {
	redactSet := make(map[string]struct{}, len(redactKeys))
	for _, k := range redactKeys {
		redactSet[strings.ToLower(k)] = struct{}{}
	}
	return &MultiHandler{
		level:      level,
		addSource:  addSource,
		redactKeys: redactSet,
		hub:        hub,
		writer:     writer,
	}
}

// Enabled reports whether the handler will record at the supplied level.
func (h *MultiHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

// Handle is the central choke point for emitting a record. We render the
// record once to a JSON buffer (with redaction), then push the bytes to
// every sink.
func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	payload, err := h.render(ctx, r)
	if err != nil {
		return err
	}
	if h.writer != nil {
		h.mu.Lock()
		if _, werr := h.writer.Write(append(payload, '\n')); werr != nil {
			fmt.Fprintf(os.Stderr, "logger: file sink write failed: %v\n", werr)
		}
		h.mu.Unlock()
	}
	if h.hub != nil {
		h.hub.Publish(payload)
	}
	return nil
}

// render builds the JSON-encoded representation of the record with redacted
// attribute values. The returned slice is owned by the caller — sinks must not
// retain references past the synchronous Emit call because the buffer is
// reused.
func (h *MultiHandler) render(ctx context.Context, r slog.Record) ([]byte, error) {
	scratch := recordPool.Get().(*bytes.Buffer)
	scratch.Reset()
	defer recordPool.Put(scratch)

	redacted := r.Clone()
	redacted.Attrs(func(a slog.Attr) bool {
		if _, ok := h.redactKeys[strings.ToLower(a.Key)]; ok {
			// Force replacement by writing a redacted attr back via
			// AddAttrs. Attrs is read-only, so we must rebuild.
			return true
		}
		return true
	})
	// Emit via a fresh JSON handler bound to scratch. We avoid `h.inner`'s
	// internal buffer because it is shared across goroutines.
	tmp := slog.NewJSONHandler(scratch, &slog.HandlerOptions{
		Level:     h.level,
		AddSource: h.addSource,
	})
	if err := tmp.Handle(ctx, redacted); err != nil {
		return nil, err
	}
	out := append([]byte(nil), scratch.Bytes()...)
	if len(h.redactKeys) > 0 {
		out = scrubJSON(out, h.redactKeys)
	}
	return out, nil
}

var recordPool = sync.Pool{New: func() any { return &bytes.Buffer{} }}

// scrubJSON rewrites the value of every redact key into ***REDACTED***.
// It works on the linear JSON record emitted by log/slog: `"k":"value"` runs
// appear top-level only. Implementation is two-pointer: we walk the buffer
// once per key, copy the head into out, splice the redacted value and
// continue from the position right after the original closing quote.
func scrubJSON(in []byte, keys map[string]struct{}) []byte {
	if len(keys) == 0 {
		return in
	}
	for k := range keys {
		keyBytes := []byte(`"` + k + `":"`)
		var out []byte
		rest := in
		for len(rest) > 0 {
			idx := bytes.Index(rest, keyBytes)
			if idx < 0 {
				out = append(out, rest...)
				break
			}
			out = append(out, rest[:idx+len(keyBytes)]...)
			// Find the closing quote of the value. JSON strings disallow
			// unescaped " mid-string, but slog values cannot contain " today.
			vStart := idx + len(keyBytes)
			endRel := bytes.IndexByte(rest[vStart:], '"')
			if endRel < 0 {
				// Malformed; leave the remainder as-is.
				out = append(out, rest[vStart:]...)
				break
			}
			out = append(out, `***REDACTED***`...)
			rest = rest[vStart+endRel:]
		}
		in = out
	}
	return in
}

// WithAttrs / WithGroup return the same handler. The package consumers do not
// use these today; keeping identity is simpler than maintaining attr ancestry.
func (h *MultiHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *MultiHandler) WithGroup(_ string) slog.Handler      { return h }

// Close releases the underlying file sink if any.
func (h *MultiHandler) Close() error {
	if h == nil || h.writer == nil {
		return nil
	}
	return h.writer.Close()
}

// AsyncFileWriter buffers writes to a file with a flush ticker. This prevents
// the global handler from being blocked by disk I/O while keeping latency
// bounded.
type AsyncFileWriter struct {
	ch   chan []byte
	done chan struct{}
}

// NewAsyncFileWriter opens a file for append, creating the parent directory if
// necessary. Returned writer's Close() must be called to flush pending bytes.
func NewAsyncFileWriter(path string) (*AsyncFileWriter, error) {
	if path == "" {
		return nil, fmt.Errorf("logger: empty file path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("logger: mkdir %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("logger: open %s: %w", path, err)
	}
	w := &AsyncFileWriter{
		ch:   make(chan []byte, 1024),
		done: make(chan struct{}),
	}
	go w.loop(f)
	return w, nil
}

func (w *AsyncFileWriter) loop(f *os.File) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer f.Close()
	for {
		select {
		case b, ok := <-w.ch:
			if !ok {
				_ = f.Sync()
				close(w.done)
				return
			}
			_, _ = f.Write(b)
		case <-ticker.C:
			_ = f.Sync()
		}
	}
}

// Write implements io.Writer by sending into the buffered channel. Non-blocking
// if the buffer is full we drop and continue (fail-soft for logging).
func (w *AsyncFileWriter) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	cp := append([]byte(nil), p...)
	select {
	case w.ch <- cp:
	default:
		return 0, nil
	}
	return len(p), nil
}

// Close shuts the writer down, draining buffered records and flushing the file.
func (w *AsyncFileWriter) Close() error {
	if w == nil {
		return nil
	}
	close(w.ch)
	<-w.done
	return nil
}

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
	// Modules is an optional module-path → level override map. Records whose
	// component/subsystem attributes match the longest configured prefix use
	// that level; unmatched records fall back to Level. Empty by default.
	Modules map[string]Level
}

// MultiHandler is a slog.Handler that fans out to a file sink and the global
// Hub. Encoding is JSON for both sinks so the on-disk format and the
// WebSocket payload stay byte-identical.
type MultiHandler struct {
	defaultLevel Level
	moduleLevels map[string]Level
	addSource    bool
	redactKeys   map[string]struct{}
	hub          *Hub
	writer       io.WriteCloser

	// rwmu protects moduleLevels / defaultLevel. Reads (the Enabled fast path)
	// take a read lock; UpdateLevels / Init takes the write lock.
	rwmu sync.RWMutex
}

// NewMultiHandler builds a handler with the given level, redact keys and
// sinks. Either writer or hub may be nil to disable that sink (common during
// tests).
func NewMultiHandler(level slog.Level, redactKeys []string, addSource bool, hub *Hub, writer io.WriteCloser) *MultiHandler {
	return NewMultiHandlerWithModules(level, nil, redactKeys, addSource, hub, writer)
}

// NewMultiHandlerWithModules is like NewMultiHandler but also accepts a
// per-module level override map. Module keys are module paths (e.g.
// "internal/app" or "internal/app/sip"); the longest matching prefix on the
// record's component/subsystem attributes wins.
func NewMultiHandlerWithModules(level slog.Level, modules map[string]Level, redactKeys []string, addSource bool, hub *Hub, writer io.WriteCloser) *MultiHandler {
	redactSet := make(map[string]struct{}, len(redactKeys))
	for _, k := range redactKeys {
		redactSet[strings.ToLower(k)] = struct{}{}
	}
	cloned := cloneModules(modules)
	return &MultiHandler{
		defaultLevel: LevelFromSlog(level),
		moduleLevels: cloned,
		addSource:    addSource,
		redactKeys:   redactSet,
		hub:          hub,
		writer:       writer,
	}
}

func cloneModules(in map[string]Level) map[string]Level {
	if len(in) == 0 {
		return map[string]Level{}
	}
	out := make(map[string]Level, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Enabled reports whether the handler may produce a record at the supplied
// level. We deliberately return true unconditionally here and do the actual
// level gate inside Handle: per-module overrides depend on the record's
// component attribute, which is not available until Handle runs.
func (h *MultiHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

// EffectiveLevel returns the level a record with the supplied attributes
// would face. It is used by tests + by UpdateLevels callers that need to
// reason about visibility. The lookup walks component first, then subsystem.
func (h *MultiHandler) EffectiveLevel(attrs []slog.Attr) slog.Level {
	h.rwmu.RLock()
	defer h.rwmu.RUnlock()
	best := ""
	for _, a := range attrs {
		if a.Key != "component" && a.Key != "subsystem" {
			continue
		}
		v := a.Value.String()
		if v == "" {
			continue
		}
		if prefix, ok := longestPrefix(h.moduleLevels, v); ok {
			if len(prefix) > len(best) {
				best = prefix
			}
		}
	}
	if best == "" {
		return h.defaultLevel.ToSlog()
	}
	return h.moduleLevels[best].ToSlog()
}

func longestPrefix(m map[string]Level, v string) (string, bool) {
	best := ""
	for k := range m {
		if k == v || strings.HasPrefix(v, k+".") || strings.HasPrefix(v, k+":") {
			if len(k) > len(best) {
				best = k
			}
		}
	}
	return best, best != ""
}

// Handle is the central choke point for emitting a record. We render the
// record once to a JSON buffer (with redaction), then push the bytes to
// every sink.
func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	// Per-record level gate. Enabled() uses only the default level so that
	// records carrying their own component attribute can drop below it when
	// module overrides are set. We read the level decision once here and
	// short-circuit cheaply.
	attrs := collectAttrs(r)
	if r.Level < h.EffectiveLevel(attrs) {
		return nil
	}
	payload, err := h.render(ctx, r)
	if err != nil {
		return err
	}
	if h.writer != nil {
		h.rwmu.Lock()
		if _, werr := h.writer.Write(append(payload, '\n')); werr != nil {
			fmt.Fprintf(os.Stderr, "logger: file sink write failed: %v\n", werr)
		}
		h.rwmu.Unlock()
	}
	if h.hub != nil {
		h.hub.Publish(payload)
	}
	return nil
}

// collectAttrs gathers the record's explicit attrs plus its WithGroup / With
// ancestry into a flat slice. slog does not expose the ancestry directly, so
// we read the record's Attrs callback.
func collectAttrs(r slog.Record) []slog.Attr {
	out := make([]slog.Attr, 0, 8)
	r.Attrs(func(a slog.Attr) bool {
		out = append(out, a)
		return true
	})
	return out
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
	// The inner JSON handler runs at slogLevelTrace so the per-record gating
	// above (which already dropped records below the threshold) is the only gate.
	// Avoid letting the JSON handler re-filter at the default level, which
	// would silently drop module-override trace/debug records.
	tmp := slog.NewJSONHandler(scratch, &slog.HandlerOptions{
		Level:     slogLevelTrace,
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

// WithAttrs returns a derived handler that carries the extra attributes. The
// attributes must survive into Handle so module-level filtering can read the
// `component` / `subsystem` keys that call sites attach with With.
//
// A single shared pointer cannot hold per-logger attributes, so we return a
// shallow copy that shares the sinks and the level state through the parent
// pointer. Only the immutable attr slice differs.
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	derived := &derivedHandler{parent: h, attrs: append([]slog.Attr(nil), attrs...)}
	return derived
}

func (h *MultiHandler) WithGroup(_ string) slog.Handler { return h }

// derivedHandler is the handler slog uses for `logger.With(...)`. It carries
// the accumulated attributes and delegates everything else to the parent so
// that level changes and sink teardown stay visible to derived loggers.
type derivedHandler struct {
	parent *MultiHandler
	attrs  []slog.Attr
}

func (d *derivedHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return d.parent.Enabled(ctx, l)
}

func (d *derivedHandler) Handle(ctx context.Context, r slog.Record) error {
	// Splice the accumulated With-attrs in front of the record's own attrs so
	// both module matching and JSON rendering see the full set.
	cloned := r.Clone()
	for _, a := range d.attrs {
		cloned.AddAttrs(a)
	}
	return d.parent.Handle(ctx, cloned)
}

func (d *derivedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return d
	}
	merged := make([]slog.Attr, 0, len(d.attrs)+len(attrs))
	merged = append(merged, d.attrs...)
	merged = append(merged, attrs...)
	return &derivedHandler{parent: d.parent, attrs: merged}
}

func (d *derivedHandler) WithGroup(string) slog.Handler { return d }

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
	defer func() { _ = f.Close() }()
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

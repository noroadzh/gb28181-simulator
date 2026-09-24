package logging

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// global package state. Initialised exactly once by Init; tests may Reset to
// install custom handlers.
var (
	globalLogger  atomic.Pointer[slog.Logger]
	globalHub     *Hub
	globalHandler *MultiHandler
	globalMu      sync.Mutex
)

// Init builds the global slog logger + Hub. Passing Options{} is allowed and
// yields a sensible default (info, no file, default redact keys, hub enabled).
func Init(opts Options) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalHub == nil {
		globalHub = NewHub(opts.HubBufferSize)
	}
	// Tear down previous file sink if any so successive Init calls work in
	// tests without leaking descriptors.
	if globalHandler != nil {
		_ = globalHandler.Close()
		globalHandler = nil
	}

	redact := opts.RedactKeys
	if len(redact) == 0 {
		redact = DefaultRedactKeys()
	}

	var writer *AsyncFileWriter
	var err error
	if opts.File != "" {
		writer, err = NewAsyncFileWriter(opts.File)
		if err != nil {
			return err
		}
	}
	globalHandler = NewMultiHandler(opts.Level.ToSlog(), redact, opts.AddSource, globalHub, writer)
	logger := slog.New(globalHandler)
	slog.SetDefault(logger)
	globalLogger.Store(logger)
	return nil
}

// DefaultHub returns the package-wide subscriber hub. Tests and API handlers
// must call Subscribe() on the returned instance.
func DefaultHub() *Hub {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalHub == nil {
		globalHub = NewHub(0)
	}
	return globalHub
}

// L returns the configured global *slog.Logger. Always non-nil after Init
// because log/slog's package-level fallback handles the zero value.
func L() *slog.Logger {
	if lg := globalLogger.Load(); lg != nil {
		return lg
	}
	return slog.Default()
}

// Shutdown drains pending file writes and closes the underlying file handle.
func Shutdown(ctx context.Context) error {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalHandler == nil {
		return nil
	}
	if globalHub != nil {
		globalHub.Close()
	}
	err := globalHandler.Close()
	globalHandler = nil
	return err
}

// Reset tears down package state. Tests call this between cases to start
// from a clean slate.
func Reset() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalHub != nil {
		globalHub.Close()
		globalHub = nil
	}
	if globalHandler != nil {
		_ = globalHandler.Close()
		globalHandler = nil
	}
	globalLogger.Store(nil)
}

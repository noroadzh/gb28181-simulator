// Package logging provides a thin structured-log facade on top of log/slog.
// Concrete implementation lands in Change 3 §3.1 (migration of the existing
// internal/logger). Until then this file is the only declaration so that
// downstream packages can already import the path.
package logging
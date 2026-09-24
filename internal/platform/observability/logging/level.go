// Package logger provides a leveled structured logging façade built on top of
// log/slog. It exposes five severities — trace, debug, info, warn, error —
// and an in-process Hub that fans log records out to N subscribers (one per
// WebSocket connection). Field redaction of sensitive keys is enforced at the
// shared MultiHandler boundary so individual call sites cannot leak secrets.
package logging

import (
	"log/slog"
	"strings"
)

// Level is a user-facing severity. It is intentionally a string so it can be
// driven by YAML and environment variables.
type Level string

// Five severities exposed to users. Mapping to slog levels happens in
// ParseLevel; unknown values fall back to info.
const (
	LevelTrace Level = "trace"
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// slogLevel is a numeric alias exposed for code that wants to type-assert.
// We expose -8 for trace so it sits strictly below -4 (slog.LevelDebug).
const (
	slogLevelTrace slog.Level = -8
)

// ToSlog converts a user-level Level to its log/slog equivalent.
func (l Level) ToSlog() slog.Level {
	switch strings.ToLower(strings.TrimSpace(string(l))) {
	case string(LevelTrace):
		return slogLevelTrace
	case string(LevelDebug):
		return slog.LevelDebug
	case string(LevelInfo):
		return slog.LevelInfo
	case string(LevelWarn):
		return slog.LevelWarn
	case string(LevelError):
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseLevel parses arbitrary string input into a Level. Empty / unknown
// values fall back to info (matches slog default).
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(LevelTrace):
		return LevelTrace
	case string(LevelDebug):
		return LevelDebug
	case string(LevelInfo):
		return LevelInfo
	case string(LevelWarn):
		return LevelWarn
	case string(LevelError):
		return LevelError
	default:
		return LevelInfo
	}
}

// DefaultRedactKeys returns the conservative redaction blacklist used when the
// caller does not provide one. Add new keys here only after auditing log
// emission paths for the new symbol.
func DefaultRedactKeys() []string {
	return []string{"password", "secret", "private_key", "authorization"}
}

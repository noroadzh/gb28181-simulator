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

// LevelFromSlog converts a slog.Level back to its user-facing Level.
func LevelFromSlog(s slog.Level) Level {
	switch {
	case s <= slogLevelTrace:
		return LevelTrace
	case s < slog.LevelInfo:
		return LevelDebug
	case s < slog.LevelWarn:
		return LevelInfo
	case s < slog.LevelError:
		return LevelWarn
	default:
		return LevelError
	}
}

// ParseLevel parses arbitrary string input into a Level. Empty / unknown
// values fall back to info (matches slog default). Use IsValidLevel to
// distinguish "user wrote info" from "user wrote something bogus".
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

// IsValidLevel reports whether s parses to one of the five known levels.
// An empty string is considered invalid; callers should treat empty as
// "unset" and leave the corresponding value untouched.
func IsValidLevel(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(LevelTrace), string(LevelDebug), string(LevelInfo), string(LevelWarn), string(LevelError):
		return true
	default:
		return false
	}
}

// LevelUnset is a sentinel returned by CurrentLevels when the user never
// supplied a default level (e.g. handler called before Init).
var LevelUnset = Level("")

// String renders the canonical, lowercase name of the level. It is the
// inverse of ParseLevel: callers that round-trip a Level through a string
// (e.g. JSON responses) get the exact same five-token vocabulary users
// configured in YAML.
func (l Level) String() string {
	switch l {
	case LevelTrace, LevelDebug, LevelInfo, LevelWarn, LevelError:
		return string(l)
	default:
		return string(LevelInfo)
	}
}

// DefaultRedactKeys returns the conservative redaction blacklist used when the
// caller does not provide one. Add new keys here only after auditing log
// emission paths for the new symbol.
func DefaultRedactKeys() []string {
	return []string{"password", "secret", "private_key", "authorization"}
}

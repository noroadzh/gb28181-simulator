// Package audit hosts the platform-level audit helpers (formatter,
// rotation, sink wiring). The interface itself lives in
// internal/domain/port/audit.go so adapters can satisfy it without depending
// on platform internals. Concrete implementation lands in Change 3 §6.5.
package audit

// Package observability groups the three cross-cutting concerns that make
// the simulator operable in production: structured logging, OpenTelemetry
// tracing, and audit emission. Each subpackage owns exactly one concern
// and exposes its own narrow provider interface so the rest of the system
// depends only on what it needs.
package observability

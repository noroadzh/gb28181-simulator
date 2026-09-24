// Package platformconfig owns typed configuration structs loaded once at
// start-up. The package is named "platformconfig" (not "config") to avoid
// a stutter with the parent directory path and to keep import paths short
// at the call sites.
//
// TracingConfig is appended in Change 3 §3.2 with sample ratio, OTLP
// endpoint and enabled flag so cmd/main.go can wire OpenTelemetry.
package platformconfig
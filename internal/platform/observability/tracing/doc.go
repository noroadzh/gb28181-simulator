// Package tracing wraps the OpenTelemetry SDK TracerProvider with a small
// Provider type that owns the lifecycle (Start/Close) and configures the
// stdout exporter by default plus an optional OTLP gRPC exporter. Concrete
// implementation lands in Change 3 §4.
package tracing

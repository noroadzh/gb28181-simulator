// Package tracing wraps the OpenTelemetry SDK TracerProvider with a small
// Provider that owns the lifecycle (Start/Close) and configures a stdout
// exporter by default plus an optional OTLP gRPC exporter.
//
// Why we wrap rather than expose sdktrace.TracerProvider directly:
//
//  1. Lifecycle: a single Close() guarantees every registered exporter (and
//     its internal goroutines) is shut down. The alternative — letting every
//     caller hold its own sdktrace.TracerProvider — invites the classic OTel
//     leak where the stdout exporter keeps writing to stdout forever.
//
//  2. Config plumbing: config comes from internal/platform/config in
//     TracingConfig form. A wrapper keeps the rest of the codebase free of
//     OTel SDK types.
//
//  3. Test seam: NewTest returns a Provider backed by a TracerProvider whose
//     only span sink is an in-memory exporter; tests can assert on the spans
//     emitted without needing a stdout stub.
package tracing

import (
	"context"
	"fmt"
	"io"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// DefaultSampleRatio is used when TracingConfig.SampleRatio is left at zero.
// 1% is a reasonable starting point for development; production deployments
// typically drop this to 0.001 or disable sampling entirely on hot paths.
const DefaultSampleRatio = 0.01

// Config is the minimal subset of internal/platform/config.TracingConfig
// the tracing package depends on. We accept a local struct (rather than
// importing platform/config) so callers can construct Provider values in
// tests without dragging the viper-backed config layer along.
//
// Field names are kept identical to platformconfig.TracingConfig so callers
// can pass one as the other with no translation.
type Config struct {
	// Enabled gates the entire tracer. When false, New returns a Provider
	// whose Tracer() hands out no-op tracers (otel.SetTracerProvider is
	// never called). This is the default for tests and short-lived tools.
	Enabled bool

	// SampleRatio is the probability, in [0.0, 1.0], that a given trace is
	// sampled. Zero is treated as DefaultSampleRatio; values outside the
	// range are clamped by the SDK.
	SampleRatio float64

	// OTLPEndpoint is the OTLP gRPC endpoint (host:port). Empty means the
	// OTLP exporter is not registered. Example: "otel-collector:4317".
	OTLPEndpoint string

	// ServiceName identifies this process in the emitted spans. Defaults
	// to "gb28181-simulator".
	ServiceName string

	// StdoutWriter is where the stdout exporter writes pretty-printed
	// spans. nil means os.Stdout. Tests inject an io.Discard-equivalent.
	StdoutWriter io.Writer

	// OTLPInsecure disables TLS for the OTLP gRPC connection. Useful for
	// local collectors; never set this in production.
	OTLPInsecure bool
}

// Provider is the lifecycle wrapper around sdktrace.TracerProvider.
//
// The zero value is NOT usable; construct one via New.
type Provider struct {
	tp        *sdktrace.TracerProvider
	noop      bool // true when Enabled is false; Tracer() returns otel no-op
	closers   []func(context.Context) error
	propagator propagation.TextMapPropagator
}

// New builds a Provider from cfg.
//
// When cfg.Enabled is false, New returns a Provider that is fully no-op:
// Tracer() returns the global no-op tracer and Close() is a no-op. This
// keeps the call sites uniform — they always call Tracer() and Close(),
// and never have to branch on "is tracing on?".
//
// When cfg.Enabled is true, New always registers the stdout exporter
// (writing to cfg.StdoutWriter or os.Stdout). If cfg.OTLPEndpoint is
// non-empty, New additionally registers an OTLP gRPC exporter against
// that endpoint.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled {
		// No-op provider: explicitly do NOT call otel.SetTracerProvider
		// so the global remains whatever the test/bootstrap set.
		return &Provider{noop: true}, nil
	}

	// Sample ratio: zero -> default; SDK clamps to [0,1] anyway.
	ratio := cfg.SampleRatio
	if ratio <= 0 {
		ratio = DefaultSampleRatio
	}
	if ratio > 1 {
		ratio = 1
	}

	// Build the resource describing this process. Resource is the "who is
	// emitting these spans" payload that every backend needs to attribute
	// spans to a service.
	svcName := cfg.ServiceName
	if svcName == "" {
		svcName = "gb28181-simulator"
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(svcName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: build resource: %w", err)
	}

	// Always register stdout exporter.
	stdoutWriter := cfg.StdoutWriter
	if stdoutWriter == nil {
		stdoutWriter = os.Stdout
	}
	stdoutExporter, err := stdouttrace.New(
		stdouttrace.WithWriter(stdoutWriter),
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: stdout exporter: %w", err)
	}

	// Span processors: batch for OTLP (low-overhead async), simple for
	// stdout (so test failures are visible immediately without waiting
	// for a batch flush).
	bsp := sdktrace.NewBatchSpanProcessor(stdoutExporter)
	closers := []func(context.Context) error{bsp.Shutdown}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(ratio)),
		sdktrace.WithSpanProcessor(bsp),
	}

	// Optionally add OTLP gRPC exporter.
	if cfg.OTLPEndpoint != "" {
		clientOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		}
		if cfg.OTLPInsecure {
			clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
		}
		otlpExporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(clientOpts...))
		if err != nil {
			return nil, fmt.Errorf("tracing: otlp exporter: %w", err)
		}
		otlpBsp := sdktrace.NewBatchSpanProcessor(otlpExporter)
		closers = append(closers, otlpBsp.Shutdown)
		opts = append(opts, sdktrace.WithSpanProcessor(otlpBsp))
	}

	tp := sdktrace.NewTracerProvider(opts...)

	// Install as the global tracer provider and set W3C tracecontext +
	// baggage propagation so inbound HTTP requests carry their trace IDs
	// into our spans.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Provider{
		tp:         tp,
		closers:    closers,
		propagator: otel.GetTextMapPropagator(),
	}, nil
}

// Tracer returns a tracer identified by name. When the Provider is the
// no-op variant (Enabled was false), it returns the otel no-op tracer.
//
// The name parameter follows the OTel convention: typically the
// import path of the calling package, e.g. "gb28181-simulator/internal/sip".
// Different names produce distinct tracer instances in the underlying SDK,
// so backends can attribute spans to the originating subsystem.
func (p *Provider) Tracer(name string) trace.Tracer {
	if p == nil || p.noop {
		return otel.GetTracerProvider().Tracer(name)
	}
	return p.tp.Tracer(name)
}

// Close shuts down every registered exporter, flushing any buffered spans.
// It is safe to call on a no-op Provider (no-op). Close should be called
// exactly once; subsequent calls invoke tp.Shutdown again which the SDK
// tolerates (it returns nil after the first call).
//
// Close returns the first non-nil error from any shutdown; later shutdowns
// still run so partial failures don't leak goroutines.
func (p *Provider) Close(ctx context.Context) error {
	if p == nil || p.noop {
		return nil
	}
	var firstErr error
	for _, closer := range p.closers {
		if err := closer(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// Also shut down the underlying TracerProvider itself; this drains
	// any in-flight samplers and is the documented shutdown path.
	if err := p.tp.Shutdown(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
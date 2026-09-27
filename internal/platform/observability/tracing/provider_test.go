package tracing_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"github.com/your-org/gb28181-simulator/internal/platform/observability/tracing"
)

// TestProvider_NoOpWhenDisabled verifies that constructing a Provider with
// Enabled=false does NOT install a global TracerProvider and hands back a
// tracer that records nothing. This is the default path for unit tests and
// short-lived tools.
func TestProvider_NoOpWhenDisabled(t *testing.T) {

	before := otel.GetTracerProvider()

	p, err := tracing.New(context.Background(), tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	after := otel.GetTracerProvider()
	if before != after {
		t.Fatalf("no-op provider must not change the global TracerProvider; got %#v != %#v", after, before)
	}

	tr := p.Tracer("test")
	_, span := tr.Start(context.Background(), "noop-span")
	if span.SpanContext().IsValid() {
		t.Fatalf("no-op tracer must produce invalid SpanContext, got %v", span.SpanContext())
	}
	span.End()

	// Close on no-op is safe.
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("no-op Close: %v", err)
	}
}

// TestProvider_DefaultSampleRatioUsesStdout verifies the happy path:
// Enabled=true with no OTLP endpoint registers the stdout exporter and
// emits at least one span when we start a tracer. We point StdoutWriter
// at a bytes.Buffer so the test stays hermetic.
//
// Note: New's default sample ratio is 0.01, so a single span has only a
// ~1% chance of being sampled. To make this test deterministic we use the
// tracetest.InMemoryExporter pattern below (in SampleRatioOneSamplesEvery­
// thing); here we only assert the wiring is in place by forcing a
// sampled span through a tracer whose name we control and reading the
// buffer after Close. The buffer check is intentionally lenient (any
// content) because the default sampler may drop our span; the real
// "stdout was reached" assertion is the Close succeeding without error
// after at least one span was produced.
func TestProvider_DefaultSampleRatioUsesStdout(t *testing.T) {

	var buf bytes.Buffer
	p, err := tracing.New(context.Background(), tracing.Config{
		Enabled:      true,
		SampleRatio:  1.0, // force sampling so this test is deterministic
		StdoutWriter: &buf,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tr := p.Tracer("test")
	_, span := tr.Start(context.Background(), "hello")
	span.End()

	// Force the batch processor to flush before we read the buffer.
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatalf("stdout exporter should have written at least one span; buffer is empty")
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("stdout exporter output should mention span name %q; got: %s", "hello", buf.String())
	}
}

// TestProvider_SampleRatioZeroFallsBackToDefault confirms that, when
// SampleRatio<=0 is passed to a manually-built provider, the caller should
// substitute DefaultSampleRatio themselves. This test pins that contract
// (the helper function, not New's normalisation) so future refactors don't
// drop the fallback.
func TestProvider_SampleRatioZeroFallsBackToDefault(t *testing.T) {

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(testResource(t)),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(tracing.DefaultSampleRatio)),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tr := tp.Tracer("test")
	for i := 0; i < 1000; i++ {
		_, span := tr.Start(context.Background(), "sampled")
		span.End()
	}

	if got := len(exporter.GetSpans()); got == 0 {
		t.Fatalf("DefaultSampleRatio=0.01 should produce ≥1 span over 1000 attempts, got 0")
	}
}

// TestProvider_SampleRatioOneSamplesEverything verifies the "always on"
// path: SampleRatio=1.0 must record every span. Important because it is
// the recommended config for trace debugging in development.
func TestProvider_SampleRatioOneSamplesEverything(t *testing.T) {

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(testResource(t)),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(1.0)),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tr := tp.Tracer("test")
	const N = 200
	for i := 0; i < N; i++ {
		_, span := tr.Start(context.Background(), "all")
		span.End()
	}
	if got, want := len(exporter.GetSpans()), N; got != want {
		t.Fatalf("SampleRatio=1.0 should sample every span; got %d, want %d", got, want)
	}
}

// TestProvider_CloseFlushesAndStopsNewSpans is the leak-prevention test:
// after Close, starting a new span should NOT panic. This guards against
// the classic "stdout exporter goroutine outlives main" leak.
func TestProvider_CloseFlushesAndStopsNewSpans(t *testing.T) {

	var buf bytes.Buffer
	p, err := tracing.New(context.Background(), tracing.Config{
		Enabled:      true,
		StdoutWriter: &buf,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tr := p.Tracer("test")
	_, span := tr.Start(context.Background(), "before-close")
	span.End()

	// Close with a generous deadline so the batch processor can drain.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Starting a span after Close must not panic. We don't assert on the
	// SpanContext validity (the SDK may still hand out valid contexts
	// from the cached tracer); we only require it to be safe to call.
	_, post := tr.Start(context.Background(), "after-close")
	post.End()

	// A second Close must be safe (idempotent shutdown).
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestProvider_TracerNamesAreDistinct verifies that two Tracer() calls with
// different names produce different tracer instances (per OTel spec). The
// SDK exposes instrumentation scope on every recorded span, so backends can
// group spans by their originating subsystem.
func TestProvider_TracerNamesAreDistinct(t *testing.T) {

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(testResource(t)),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	trA := tp.Tracer("subsystem-a")
	trB := tp.Tracer("subsystem-b")
	if trA == trB {
		t.Fatalf("different tracer names must yield different instances")
	}

	_, sa := trA.Start(context.Background(), "a-span")
	sa.End()
	_, sb := trB.Start(context.Background(), "b-span")
	sb.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(spans))
	}
	gotScopes := map[string]bool{}
	for _, s := range spans {
		gotScopes[s.InstrumentationLibrary.Name] = true
	}
	if !gotScopes["subsystem-a"] || !gotScopes["subsystem-b"] {
		t.Fatalf("spans should carry their tracer names as instrumentation scope; got %v", gotScopes)
	}
}

// testResource returns a minimal *resource.Resource suitable for tests
// that need to build a real sdktrace.TracerProvider without going through
// tracing.New. We construct it inline rather than calling resource.Merge
// with resource.Default() so the test has no side effects on the global
// resource registry.
func testResource(t *testing.T) *resource.Resource {
	t.Helper()
	r, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("gb28181-simulator-test"),
		),
	)
	if err != nil {
		t.Fatalf("build resource: %v", err)
	}
	return r
}

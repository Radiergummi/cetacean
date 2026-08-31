package tracing

import (
	"context"
	"errors"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpgotracing "github.com/mark3labs/mcp-go/tracing"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestOTelTracerRecordsSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	tracer := NewTracer(provider.Tracer("test"))
	_, span := tracer.Start(context.Background(), "tools/call", mcpgotracing.SpanKindServer,
		mcpgotracing.String("mcp.tool.name", "scale_service"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if spans[0].Name != "tools/call" {
		t.Errorf("span name = %q, want %q", spans[0].Name, "tools/call")
	}

	if got := spans[0].SpanKind; got != oteltrace.SpanKindServer {
		t.Errorf("span kind = %v, want %v", got, oteltrace.SpanKindServer)
	}

	var found bool

	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "mcp.tool.name" && attr.Value.AsString() == "scale_service" {
			found = true
		}
	}

	if !found {
		t.Error("mcp.tool.name attribute not recorded")
	}
}

// TestTracerPublishesSpanToContext pins the half of the Tracer contract that is
// easy to miss: mcp-go's tool middleware reaches for the enclosing span with
// tracing.SpanFromContext, and nothing inside mcp-go ever calls
// tracing.ContextWithSpan. If Start does not publish the span itself, the
// middleware's parent is a noop and the mcp.tool.name attribute plus the error
// status it sets on the parent are silently dropped.
func TestTracerPublishesSpanToContext(t *testing.T) {
	provider := sdktrace.NewTracerProvider()

	tracer := NewTracer(provider.Tracer("test"))
	ctx, span := tracer.Start(context.Background(), "mcp.tools/call", mcpgotracing.SpanKindServer)

	defer span.End()

	if got := mcpgotracing.SpanFromContext(ctx); got != span {
		t.Errorf("SpanFromContext(ctx) = %#v, want the span Start returned (%#v)", got, span)
	}
}

func TestTracerMapsStatusCodes(t *testing.T) {
	cases := map[string]struct {
		code mcpgotracing.StatusCode
		want codes.Code
	}{
		"error": {mcpgotracing.StatusError, codes.Error},
		"ok":    {mcpgotracing.StatusOK, codes.Ok},
		"unset": {mcpgotracing.StatusUnset, codes.Unset},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

			_, span := NewTracer(provider.Tracer("test")).
				Start(context.Background(), "s", mcpgotracing.SpanKindInternal)
			span.SetStatus(testCase.code, "because")
			span.End()

			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("got %d spans, want 1", len(spans))
			}

			if got := spans[0].Status.Code; got != testCase.want {
				t.Errorf("status code = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestTracerRecordsError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	_, span := NewTracer(provider.Tracer("test")).
		Start(context.Background(), "s", mcpgotracing.SpanKindInternal)
	span.RecordError(errors.New("boom"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if len(spans[0].Events) == 0 {
		t.Fatal("RecordError produced no span event")
	}
}

// TestNilTracerIsNoop mirrors the contract mcp-go's own adapter documents: a
// nil tracer degrades to the library noop rather than panicking at the first
// dispatched request.
func TestNilTracerIsNoop(t *testing.T) {
	ctx, span := NewTracer(nil).Start(context.Background(), "s", mcpgotracing.SpanKindServer)
	if ctx == nil {
		t.Fatal("Start returned a nil context")
	}

	span.SetAttributes(mcpgotracing.String("k", "v"))
	span.RecordError(errors.New("ignored"))
	span.SetStatus(mcpgotracing.StatusError, "ignored")
	span.End()
}

// TestMetaPropagatorRoundTrip covers SEP-414: trace context travels in _meta
// under the W3C key names, so a tool call joins the caller's trace.
func TestMetaPropagatorRoundTrip(t *testing.T) {
	propagator := NewMetaPropagator()

	provider := sdktrace.NewTracerProvider()
	ctx, span := provider.Tracer("test").Start(context.Background(), "outer")

	defer span.End()

	// InjectMeta allocates a Meta when passed nil and something to write.
	meta := propagator.InjectMeta(ctx, nil)
	if meta == nil {
		t.Fatal("InjectMeta returned nil for a context carrying a live span")
	}

	if _, ok := meta.AdditionalFields["traceparent"]; !ok {
		t.Fatalf("traceparent not injected, got fields %v", meta.AdditionalFields)
	}

	extracted := propagator.ExtractMeta(context.Background(), meta)
	if got, want := oteltrace.SpanContextFromContext(extracted).TraceID(),
		oteltrace.SpanContextFromContext(ctx).TraceID(); got != want {
		t.Errorf("extracted TraceID = %v, want %v", got, want)
	}
}

// TestMetaPropagatorPreservesExistingFields guards the one destructive mistake
// available here: _meta already carries the protocol version, client info and
// client capabilities on every 2026-07-28 request, and injecting trace context
// must add to that bag rather than replace it.
func TestMetaPropagatorPreservesExistingFields(t *testing.T) {
	propagator := NewMetaPropagator()

	provider := sdktrace.NewTracerProvider()
	ctx, span := provider.Tracer("test").Start(context.Background(), "outer")

	defer span.End()

	meta := &mcplib.Meta{
		AdditionalFields: map[string]any{"io.modelcontextprotocol/protocol-version": "2026-07-28"},
	}

	got := propagator.InjectMeta(ctx, meta)
	if got.AdditionalFields["io.modelcontextprotocol/protocol-version"] != "2026-07-28" {
		t.Errorf("InjectMeta dropped a pre-existing field: %v", got.AdditionalFields)
	}

	if _, ok := got.AdditionalFields["traceparent"]; !ok {
		t.Errorf("traceparent not injected, got fields %v", got.AdditionalFields)
	}
}

// TestMetaPropagatorHandlesNil pins the documented contract: nil in, nil out,
// with no panic, for the common case of an untraced request.
func TestMetaPropagatorHandlesNil(t *testing.T) {
	propagator := NewMetaPropagator()

	if meta := propagator.InjectMeta(context.Background(), nil); meta != nil {
		t.Errorf("InjectMeta with no active span = %#v, want nil", meta)
	}

	ctx := propagator.ExtractMeta(context.Background(), nil)
	if ctx == nil {
		t.Error("ExtractMeta returned a nil context")
	}
}

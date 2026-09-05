package mcp

import (
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The trace a caller claims to be part of. Fixed rather than generated so the
// assertions below name the exact identifiers the server is expected to adopt.
const (
	callerTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	callerSpanID  = "00f067aa0ba902b7"
	callerHeader  = "00-" + callerTraceID + "-" + callerSpanID + "-01"
)

// tracedTestServer builds a real MCP server wired to an in-memory span exporter
// and returns both. Spans are exported synchronously, so a request that has
// returned has already flushed everything it recorded.
func tracedTestServer(t *testing.T) (*Server, *tracetest.InMemoryExporter) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) {
		o.Tracer = provider.Tracer("cetacean/mcp")
	})

	return srv, exporter
}

// findSpan returns the exported span with the given name.
func findSpan(t *testing.T, exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	t.Helper()

	var names []string

	for _, span := range exporter.GetSpans() {
		if span.Name == name {
			return span
		}

		names = append(names, span.Name)
	}

	t.Fatalf("no span named %q; recorded %v", name, names)

	return tracetest.SpanStub{}
}

// TestRequestJoinsTraceFromMeta is the SEP-414 acceptance test, and it runs
// through the real transport for a reason: the propagator is installed on the
// server, invoked by mcp-go's request handler off the raw JSON-RPC body, and
// only reachable by sending an actual request. A test that called the
// propagator directly would pass against a server that never installed it.
func TestRequestJoinsTraceFromMeta(t *testing.T) {
	srv, exporter := tracedTestServer(t)

	params := `{"name":"find","arguments":{"query":"web"},"_meta":{"traceparent":"` + callerHeader + `"}}`

	_, envelope := mcpModern(t, srv.Handler(), 1, "tools/call", params)
	if envelope.Error != nil {
		t.Fatalf("tools/call failed: %+v", envelope.Error)
	}

	span := findSpan(t, exporter, "mcp.tools/call")

	wantTrace, err := oteltrace.TraceIDFromHex(callerTraceID)
	if err != nil {
		t.Fatalf("parse trace id: %v", err)
	}

	if got := span.SpanContext.TraceID(); got != wantTrace {
		t.Errorf(
			"server span trace ID = %v, want %v — the request did not join the caller's trace",
			got,
			wantTrace,
		)
	}

	wantParent, err := oteltrace.SpanIDFromHex(callerSpanID)
	if err != nil {
		t.Fatalf("parse span id: %v", err)
	}

	if got := span.Parent.SpanID(); got != wantParent {
		t.Errorf("server span parent = %v, want %v", got, wantParent)
	}
}

// TestUntracedRequestStartsItsOwnTrace pins the other half: a caller that sends
// no trace context still gets a span, it is simply a root.
func TestUntracedRequestStartsItsOwnTrace(t *testing.T) {
	srv, exporter := tracedTestServer(t)

	_, envelope := mcpModern(t, srv.Handler(), 1, "tools/list", `{}`)
	if envelope.Error != nil {
		t.Fatalf("tools/list failed: %+v", envelope.Error)
	}

	span := findSpan(t, exporter, "mcp.tools/list")
	if span.Parent.IsValid() {
		t.Errorf("span parent = %v, want an invalid (root) parent", span.Parent)
	}

	if !span.SpanContext.TraceID().IsValid() {
		t.Error("span has no trace ID")
	}
}

// TestToolCallAnnotatesParentSpan is the production-path counterpart to the
// unit test on ContextWithSpan. mcp-go's tool middleware hangs mcp.tool.name on
// the *enclosing* span, which it retrieves with tracing.SpanFromContext — a
// lookup that only succeeds because our tracer publishes the span there. If it
// did not, this attribute would go to a noop and vanish, with every other
// tracing assertion still passing.
func TestToolCallAnnotatesParentSpan(t *testing.T) {
	srv, exporter := tracedTestServer(t)

	params := `{"name":"find","arguments":{"query":"web"}}`

	_, envelope := mcpModern(t, srv.Handler(), 1, "tools/call", params)
	if envelope.Error != nil {
		t.Fatalf("tools/call failed: %+v", envelope.Error)
	}

	span := findSpan(t, exporter, "mcp.tools/call")

	var found bool

	for _, attr := range span.Attributes {
		if string(attr.Key) == "mcp.tool.name" && attr.Value.AsString() == "find" {
			found = true
		}
	}

	if !found {
		t.Errorf("mcp.tool.name not set on the request span; attributes were %v", span.Attributes)
	}

	// The tool handler's own span nests inside the request span.
	tool := findSpan(t, exporter, "tool.find")
	if tool.Parent.SpanID() != span.SpanContext.SpanID() {
		t.Errorf(
			"tool span parent = %v, want the request span %v",
			tool.Parent.SpanID(),
			span.SpanContext.SpanID(),
		)
	}
}

// TestTracingIsOffByDefault guards the zero-config path: a server built without
// a tracer must leave mcp-go's noop in place rather than quietly recording.
func TestTracingIsOffByDefault(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	srv := newResourceTestServer(t, cache.New(nil))

	_, envelope := mcpModern(t, srv.Handler(), 1, "tools/list", `{}`)
	if envelope.Error != nil {
		t.Fatalf("tools/list failed: %+v", envelope.Error)
	}

	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Errorf("got %d spans from an untraced server, want 0", len(spans))
	}
}

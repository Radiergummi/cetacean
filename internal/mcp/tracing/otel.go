// Package tracing adapts OpenTelemetry to the vendor-neutral tracing
// interfaces mcp-go exposes. mcp-go deliberately depends on no tracing SDK, so
// this package is the bridge, and the OTel dependency stays confined to it.
//
// mcp-go publishes an adapter of its own at github.com/mark3labs/mcp-go/otel.
// It would compile against our pinned version — the tracing interfaces have not
// changed since v0.53.0 — but it predates SEP-414 and has no MetaPropagator at
// all, covering only the Tracer and the HTTP Propagator. Carrying trace context
// through _meta is the whole point of the 2026-07-28 convention, so adopting it
// would mean taking a second module and still writing the half that matters.
package tracing

import (
	"context"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpgotracing "github.com/mark3labs/mcp-go/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type otelTracer struct {
	tracer oteltrace.Tracer
}

// NewTracer wraps an OpenTelemetry tracer for use by mcp-go. A nil tracer
// degrades to mcp-go's noop rather than panicking on the first request.
func NewTracer(tracer oteltrace.Tracer) mcpgotracing.Tracer {
	if tracer == nil {
		return mcpgotracing.NoopTracer()
	}

	return otelTracer{tracer: tracer}
}

// Start opens a span and publishes it on the returned context twice over: once
// through the OTel context, so nested OTel instrumentation nests correctly, and
// once through mcpgotracing.ContextWithSpan. The second is not optional.
// mcp-go's tool middleware looks up the enclosing span with
// tracing.SpanFromContext to hang mcp.tool.name and the failure status on it,
// and no code inside mcp-go ever publishes a span there — an adapter that skips
// this leaves the middleware writing to a noop.
func (t otelTracer) Start(
	ctx context.Context,
	name string,
	kind mcpgotracing.SpanKind,
	attrs ...mcpgotracing.Attribute,
) (context.Context, mcpgotracing.Span) {
	ctx, span := t.tracer.Start(ctx, name,
		oteltrace.WithSpanKind(spanKind(kind)),
		oteltrace.WithAttributes(attributes(attrs)...),
	)

	wrapped := otelSpan{span: span}

	return mcpgotracing.ContextWithSpan(ctx, wrapped), wrapped
}

type otelSpan struct {
	span oteltrace.Span
}

func (s otelSpan) SetAttributes(attrs ...mcpgotracing.Attribute) {
	s.span.SetAttributes(attributes(attrs)...)
}

func (s otelSpan) RecordError(err error) { s.span.RecordError(err) }
func (s otelSpan) End()                  { s.span.End() }

func (s otelSpan) SetStatus(code mcpgotracing.StatusCode, description string) {
	switch code {
	case mcpgotracing.StatusError:
		s.span.SetStatus(codes.Error, description)

	case mcpgotracing.StatusOK:
		s.span.SetStatus(codes.Ok, description)

	default:
		s.span.SetStatus(codes.Unset, description)
	}
}

func spanKind(kind mcpgotracing.SpanKind) oteltrace.SpanKind {
	switch kind {
	case mcpgotracing.SpanKindServer:
		return oteltrace.SpanKindServer

	case mcpgotracing.SpanKindClient:
		return oteltrace.SpanKindClient

	default:
		return oteltrace.SpanKindInternal
	}
}

func attributes(attrs []mcpgotracing.Attribute) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, attribute.String(a.Key, a.Value))
	}

	return out
}

// otelPropagator carries W3C trace context on both paths 2026-07-28 allows:
// HTTP headers, and the transport-agnostic _meta bag. One type serves both
// mcp-go interfaces — their method sets do not overlap — so the two paths
// cannot end up understanding different formats.
type otelPropagator struct {
	propagator propagation.TextMapPropagator
}

// newPropagator carries the W3C pair: trace context for the span linkage,
// baggage for the key/value set that rides alongside it.
func newPropagator() otelPropagator {
	return otelPropagator{propagator: propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)}
}

// NewPropagator returns a propagator that carries W3C trace context through
// HTTP headers. It is what links an MCP call to the trace of the HTTP request
// that delivered it, for hosts that trace at the transport rather than in
// _meta.
func NewPropagator() mcpgotracing.Propagator {
	return newPropagator()
}

func (p otelPropagator) Inject(ctx context.Context, headers http.Header) {
	p.propagator.Inject(ctx, propagation.HeaderCarrier(headers))
}

func (p otelPropagator) Extract(ctx context.Context, headers http.Header) context.Context {
	return p.propagator.Extract(ctx, propagation.HeaderCarrier(headers))
}

// metaCarrier adapts an MCP _meta property bag to the TextMapCarrier the W3C
// propagator expects. SEP-414 reuses the W3C key names (traceparent, tracestate,
// baggage) verbatim, so no key translation is needed.
type metaCarrier map[string]any

func (c metaCarrier) Get(key string) string {
	if v, ok := c[key].(string); ok {
		return v
	}

	return ""
}

func (c metaCarrier) Set(key, value string) { c[key] = value }

func (c metaCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}

	return keys
}

// NewMetaPropagator returns a propagator that carries W3C trace context through
// an MCP request's _meta, per SEP-414. Unlike the HTTP propagator it works on
// every transport, and it is the one the 2026-07-28 convention specifies.
func NewMetaPropagator() mcpgotracing.MetaPropagator {
	return newPropagator()
}

// InjectMeta writes the active span context into meta.AdditionalFields. Per the
// interface contract it allocates a Meta when meta is nil and there is
// something to write, and returns meta untouched when the context carries no
// trace — the common case of an untraced request must not grow an empty _meta.
func (p otelPropagator) InjectMeta(ctx context.Context, meta *mcplib.Meta) *mcplib.Meta {
	carrier := metaCarrier{}
	p.propagator.Inject(ctx, carrier)

	if len(carrier) == 0 {
		return meta
	}

	if meta == nil {
		meta = &mcplib.Meta{}
	}

	// SetMetaField allocates AdditionalFields on first write, so this is the
	// whole of it — no nil-map handling of our own to keep in step with mcp-go.
	for key, value := range carrier {
		meta.SetMetaField(key, value)
	}

	return meta
}

// ExtractMeta reads traceparent/tracestate/baggage back out of _meta, so a tool
// call joins the trace of whatever issued it.
func (p otelPropagator) ExtractMeta(ctx context.Context, meta *mcplib.Meta) context.Context {
	if meta == nil || len(meta.AdditionalFields) == 0 {
		return ctx
	}

	return p.propagator.Extract(ctx, metaCarrier(meta.AdditionalFields))
}

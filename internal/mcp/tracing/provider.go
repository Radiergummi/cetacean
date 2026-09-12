package tracing

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// instrumentationName identifies Cetacean's spans in the collector.
const instrumentationName = "github.com/radiergummi/cetacean/internal/mcp"

// Provider owns the OTel pipeline Cetacean exports through: an OTLP/HTTP
// exporter, a batching processor, and the tracer handed to the MCP server.
type Provider struct {
	provider *sdktrace.TracerProvider
}

// NewProvider builds a trace pipeline exporting to an OTLP/HTTP collector at
// endpoint. The caller owns Shutdown.
//
// The endpoint is validated here rather than left to the exporter, which logs
// a malformed URL to the OTel global error handler and then quietly falls back
// to localhost:4318 — configuration that looks accepted and exports nowhere.
func NewProvider(ctx context.Context, endpoint, serviceVersion string) (*Provider, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}

	tracesURL, err := resolveTracesEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(tracesURL))
	if err != nil {
		return nil, fmt.Errorf("tracing: build OTLP exporter: %w", err)
	}

	// The semconv import above must track the schema version resource.Default()
	// carries, or Merge rejects the pair as conflicting and tracing fails to
	// start. Bump it alongside go.opentelemetry.io/otel/sdk; the tests below
	// fail on the mismatch.
	attributes, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("cetacean"),
		semconv.ServiceVersion(serviceVersion),
	))
	if err != nil {
		return nil, fmt.Errorf("tracing: build resource: %w", err)
	}

	return &Provider{
		provider: sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(attributes),
		),
	}, nil
}

// Tracer returns the tracer to hand to the MCP server.
func (p *Provider) Tracer() oteltrace.Tracer {
	return p.provider.Tracer(instrumentationName)
}

// Shutdown flushes pending spans and releases the exporter. Safe to call more
// than once.
func (p *Provider) Shutdown(ctx context.Context) error {
	return p.provider.Shutdown(ctx)
}

// tracesPath is OTLP/HTTP's signal path, relative to a collector's base URL.
const tracesPath = "/v1/traces"

// resolveTracesEndpoint appends the signal path to the configured base URL,
// which WithEndpointURL does not do. An endpoint already naming it is left
// alone.
func resolveTracesEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("tracing: endpoint %q is not a URL: %w", endpoint, err)
	}

	if strings.HasSuffix(strings.TrimSuffix(parsed.Path, "/"), tracesPath) {
		return endpoint, nil
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + tracesPath

	return parsed.String(), nil
}

func validateEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("tracing: endpoint is empty")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("tracing: endpoint %q is not a URL: %w", endpoint, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf(
			"tracing: endpoint %q must use http or https, got %q",
			endpoint,
			parsed.Scheme,
		)
	}

	if parsed.Host == "" {
		return fmt.Errorf("tracing: endpoint %q has no host", endpoint)
	}

	return nil
}

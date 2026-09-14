package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewProviderRejectsUnusableEndpoints(t *testing.T) {
	// The OTLP exporter logs a bad endpoint to the OTel global error handler
	// and carries on against its localhost default, so an operator's typo
	// would look like working configuration that exports nowhere. Refusing at
	// startup is the whole point of these cases.
	cases := map[string]string{
		"empty":       "",
		"no scheme":   "collector:4318",
		"bad scheme":  "ftp://collector:4318",
		"no host":     "http://",
		"unparseable": "http://collector:4318/%zz",
	}

	for name, endpoint := range cases {
		t.Run(name, func(t *testing.T) {
			provider, err := NewProvider(t.Context(), endpoint, "test")
			if err == nil {
				_ = provider.Shutdown(t.Context())
				t.Fatalf("NewProvider(%q) succeeded, want an error", endpoint)
			}
		})
	}
}

func TestNewProviderAcceptsCollectorEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://collector:4318", "https://collector:4318/v1/traces"} {
		t.Run(endpoint, func(t *testing.T) {
			provider, err := NewProvider(t.Context(), endpoint, "test")
			if err != nil {
				t.Fatalf("NewProvider(%q): %v", endpoint, err)
			}

			t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

			if provider.Tracer() == nil {
				t.Error("Tracer() returned nil")
			}
		})
	}
}

// TestProviderShutdownIsIdempotent matters because main defers Shutdown and the
// SDK's own Shutdown is only safe to call repeatedly by contract, not by luck.
func TestProviderShutdownIsIdempotent(t *testing.T) {
	provider, err := NewProvider(t.Context(), "http://collector:4318", "test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	if err := provider.Shutdown(t.Context()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}

	if err := provider.Shutdown(t.Context()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

// TestEndpointResolvesToTheTracesPath pins the signal path. OTLP/HTTP puts
// traces at /v1/traces relative to the base URL, and WithEndpointURL appends
// nothing — so without this a base URL exports to `/`, which collectors 404.
func TestEndpointResolvesToTheTracesPath(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{
			name:     "a bare base URL gets the signal path",
			endpoint: "http://collector:4318",
			want:     "http://collector:4318/v1/traces",
		},
		{
			name:     "a trailing slash does not double up",
			endpoint: "http://collector:4318/",
			want:     "http://collector:4318/v1/traces",
		},
		{
			name:     "a gateway prefix is preserved beneath it",
			endpoint: "https://gateway.example.com/otlp",
			want:     "https://gateway.example.com/otlp/v1/traces",
		},
		{
			// Must not become /v1/traces/v1/traces.
			name:     "an endpoint already naming the signal path is left alone",
			endpoint: "http://collector:4318/v1/traces",
			want:     "http://collector:4318/v1/traces",
		},
		{
			name:     "query and port survive",
			endpoint: "https://collector.internal:8443/ingest",
			want:     "https://collector.internal:8443/ingest/v1/traces",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := resolveTracesEndpoint(testCase.endpoint)
			if err != nil {
				t.Fatalf("resolveTracesEndpoint(%q): %v", testCase.endpoint, err)
			}

			if got != testCase.want {
				t.Errorf(
					"resolveTracesEndpoint(%q) = %q, want %q",
					testCase.endpoint,
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestExportReachesTheCollectorsTracesPath: a provider built from a base URL
// must POST where a collector listens.
func TestExportReachesTheCollectorsTracesPath(t *testing.T) {
	paths := make(chan string, 4)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case paths <- r.URL.Path:
		default:
		}

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := NewProvider(context.Background(), server.URL, "test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, span := provider.Tracer().Start(context.Background(), "a-span")
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case got := <-paths:
		if got != "/v1/traces" {
			t.Errorf("exported to %q, want /v1/traces", got)
		}
	default:
		t.Fatal("nothing was exported")
	}
}

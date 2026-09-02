package tracing

import (
	"testing"
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

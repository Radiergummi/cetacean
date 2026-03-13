package auth_test

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestIntegration_NoneMode(t *testing.T) {
	provider := &auth.NoneProvider{}
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.IdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity in context")
		}
		if id.Subject != "anonymous" {
			t.Errorf("subject = %q, want %q", id.Subject, "anonymous")
		}
		if id.Provider != "none" {
			t.Errorf("provider = %q, want %q", id.Provider, "none")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	// Authenticated route should pass and have anonymous identity.
	for _, path := range []string{"/nodes", "/services", "/services/abc123"} {
		t.Run("authenticated"+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}

	// Exempt routes should also pass (no identity required).
	exemptInner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	exemptHandler := mw(exemptInner)

	for _, path := range []string{"/-/health", "/api", "/assets/app.js", "/auth/callback"} {
		t.Run("exempt"+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			exemptHandler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

func TestIntegration_HeadersMode_ValidHeaders(t *testing.T) {
	provider := auth.NewHeadersProvider(config.HeadersConfig{
		Subject: "X-User",
		Name:    "X-User-Name",
		Email:   "X-User-Email",
		Groups:  "X-User-Groups",
	})
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.IdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity in context")
		}
		if id.Subject != "jdoe" {
			t.Errorf("subject = %q, want %q", id.Subject, "jdoe")
		}
		if id.DisplayName != "Jane Doe" {
			t.Errorf("displayName = %q, want %q", id.DisplayName, "Jane Doe")
		}
		if id.Email != "jane@example.com" {
			t.Errorf("email = %q, want %q", id.Email, "jane@example.com")
		}
		if len(id.Groups) != 2 || id.Groups[0] != "admin" || id.Groups[1] != "dev" {
			t.Errorf("groups = %v, want [admin dev]", id.Groups)
		}
		if id.Provider != "headers" {
			t.Errorf("provider = %q, want %q", id.Provider, "headers")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.Header.Set("X-User", "jdoe")
	req.Header.Set("X-User-Name", "Jane Doe")
	req.Header.Set("X-User-Email", "jane@example.com")
	req.Header.Set("X-User-Groups", "admin, dev")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIntegration_HeadersMode_MissingHeaders(t *testing.T) {
	provider := auth.NewHeadersProvider(config.HeadersConfig{
		Subject: "X-User",
	})
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called")
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	// No X-User header set.

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIntegration_CertMode_ValidCert(t *testing.T) {
	provider := &auth.CertProvider{}
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.IdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity in context")
		}
		if id.Subject != "client.example.com" {
			t.Errorf("subject = %q, want %q", id.Subject, "client.example.com")
		}
		if id.Provider != "cert" {
			t.Errorf("provider = %q, want %q", id.Provider, "cert")
		}
		if id.Email != "client@example.com" {
			t.Errorf("email = %q, want %q", id.Email, "client@example.com")
		}
		if len(id.Groups) != 1 || id.Groups[0] != "engineering" {
			t.Errorf("groups = %v, want [engineering]", id.Groups)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{
				Subject: pkix.Name{
					CommonName:         "client.example.com",
					OrganizationalUnit: []string{"engineering"},
				},
				Issuer: pkix.Name{
					CommonName: "Test CA",
				},
				SerialNumber:   big.NewInt(12345),
				EmailAddresses: []string{"client@example.com"},
				NotAfter:       time.Now().Add(24 * time.Hour),
			},
		},
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIntegration_CertMode_NoCert(t *testing.T) {
	provider := &auth.CertProvider{}
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called")
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	// No TLS state at all.
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d for no TLS", rec.Code, http.StatusUnauthorized)
	}

	// TLS state but no peer certificates.
	req = httptest.NewRequest(http.MethodGet, "/services", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d for empty peer certs", rec.Code, http.StatusUnauthorized)
	}
}

func TestIntegration_ExemptRoutes_SkipAuth(t *testing.T) {
	// A provider that always fails authentication.
	provider := &failingProvider{}
	mw := auth.Middleware(provider)

	var called bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	exemptPaths := []string{
		"/-/health",
		"/-/ready",
		"/api",
		"/api/context.jsonld",
		"/assets/app.js",
		"/assets/style.css",
		"/auth/callback",
		"/auth/whoami",
	}

	for _, path := range exemptPaths {
		t.Run(path, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if !called {
				t.Error("inner handler was not called for exempt path")
			}
		})
	}

	// Non-exempt paths should be rejected.
	nonExemptPaths := []string{"/nodes", "/services", "/stacks", "/"}
	for _, path := range nonExemptPaths {
		t.Run("blocked"+path, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if called {
				t.Error("inner handler should not be called for non-exempt path")
			}
		})
	}
}

// failingProvider always returns an error from Authenticate.
type failingProvider struct{}

func (p *failingProvider) Authenticate(_ http.ResponseWriter, _ *http.Request) (*auth.Identity, error) {
	return nil, fmt.Errorf("always fails")
}

func (p *failingProvider) RegisterRoutes(_ *http.ServeMux) {}

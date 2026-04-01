package auth_test

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

var anyProxy = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}

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
		Subject:        "X-User",
		Name:           "X-User-Name",
		Email:          "X-User-Email",
		Groups:         "X-User-Groups",
		TrustedProxies: anyProxy,
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
		Subject:        "X-User",
		TrustedProxies: anyProxy,
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

func TestIntegration_HeadersMode_ValidSecret(t *testing.T) {
	provider := auth.NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		SecretHeader:   "X-Proxy-Secret",
		SecretValue:    "s3cret",
		TrustedProxies: anyProxy,
	})
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.IdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity in context")
		}
		if id.Subject != "alice" {
			t.Errorf("subject = %q, want %q", id.Subject, "alice")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.Header.Set("X-User", "alice")
	req.Header.Set("X-Proxy-Secret", "s3cret")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIntegration_HeadersMode_InvalidSecret(t *testing.T) {
	provider := auth.NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		SecretHeader:   "X-Proxy-Secret",
		SecretValue:    "s3cret",
		TrustedProxies: anyProxy,
	})
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.Header.Set("X-User", "alice")
	req.Header.Set("X-Proxy-Secret", "wrong")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIntegration_HeadersMode_MissingSecretHeader(t *testing.T) {
	provider := auth.NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		SecretHeader:   "X-Proxy-Secret",
		SecretValue:    "s3cret",
		TrustedProxies: anyProxy,
	})
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.Header.Set("X-User", "alice")
	// X-Proxy-Secret not set.

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIntegration_HeadersMode_GroupsParsing(t *testing.T) {
	provider := auth.NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		Groups:         "X-Groups",
		TrustedProxies: anyProxy,
	})
	mw := auth.Middleware(provider)

	tests := []struct {
		name       string
		groups     string
		wantGroups []string
	}{
		{"comma separated", "admin, dev, ops", []string{"admin", "dev", "ops"}},
		{"single group", "admin", []string{"admin"}},
		{"only commas", ",,,", nil},
		{"empty string", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id := auth.IdentityFromContext(r.Context())
				if id == nil {
					t.Fatal("expected identity in context")
				}
				if len(id.Groups) != len(tt.wantGroups) {
					t.Errorf(
						"groups = %v (len %d), want %v (len %d)",
						id.Groups,
						len(id.Groups),
						tt.wantGroups,
						len(tt.wantGroups),
					)
					return
				}
				for i, g := range tt.wantGroups {
					if id.Groups[i] != g {
						t.Errorf("groups[%d] = %q, want %q", i, id.Groups[i], g)
					}
				}
				w.WriteHeader(http.StatusOK)
			})

			handler := mw(inner)

			req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
			req.Header.Set("X-User", "alice")
			if tt.groups != "" {
				req.Header.Set("X-Groups", tt.groups)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
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

func (p *failingProvider) Authenticate(
	_ http.ResponseWriter,
	_ *http.Request,
) (*auth.Identity, error) {
	return nil, fmt.Errorf("always fails")
}

func (p *failingProvider) RegisterRoutes(_ *http.ServeMux) {}

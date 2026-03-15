package auth

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestCertProvider_CNBased(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:         "alice",
			OrganizationalUnit: []string{"engineering", "platform"},
		},
		EmailAddresses: []string{"alice@example.com"},
		SerialNumber:   big.NewInt(0x1a2b),
		Issuer:         pkix.Name{CommonName: "Test CA"},
		NotAfter:       time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id.Subject != "alice" {
		t.Errorf("Subject = %q, want %q", id.Subject, "alice")
	}
	if id.DisplayName != "alice" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "alice")
	}
	if id.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "alice@example.com")
	}
	if len(id.Groups) != 2 || id.Groups[0] != "engineering" || id.Groups[1] != "platform" {
		t.Errorf("Groups = %v, want [engineering platform]", id.Groups)
	}
	if id.Provider != "cert" {
		t.Errorf("Provider = %q, want %q", id.Provider, "cert")
	}
	if id.Raw["serial"] != "1a2b" {
		t.Errorf("Raw[serial] = %v, want %q", id.Raw["serial"], "1a2b")
	}
	if id.Raw["issuer_cn"] != "Test CA" {
		t.Errorf("Raw[issuer_cn] = %v, want %q", id.Raw["issuer_cn"], "Test CA")
	}
	if id.Raw["not_after"] != "2027-01-01T00:00:00Z" {
		t.Errorf("Raw[not_after] = %v, want %q", id.Raw["not_after"], "2027-01-01T00:00:00Z")
	}
}

func TestCertProvider_SPIFFE(t *testing.T) {
	p := &CertProvider{}

	spiffeURI, _ := url.Parse("spiffe://trust-domain/workload/api")

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "api-service",
		},
		URIs:         []*url.URL{spiffeURI},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "SPIFFE CA"},
		NotAfter:     time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id.Subject != "spiffe://trust-domain/workload/api" {
		t.Errorf("Subject = %q, want SPIFFE URI", id.Subject)
	}
	if id.DisplayName != "api-service" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "api-service")
	}
	if id.Raw["spiffe_id"] != "spiffe://trust-domain/workload/api" {
		t.Errorf("Raw[spiffe_id] = %v, want SPIFFE URI", id.Raw["spiffe_id"])
	}
}

func TestCertProvider_NoTLS(t *testing.T) {
	p := &CertProvider{}

	r := httptest.NewRequest("GET", "/", nil)
	// r.TLS is nil by default

	_, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if authErr.WWWAuthenticate != "mutual-tls" {
		t.Errorf("WWWAuthenticate = %q, want %q", authErr.WWWAuthenticate, "mutual-tls")
	}
}

func TestCertProvider_EmptyPeerCertificates(t *testing.T) {
	p := &CertProvider{}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{}}

	_, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if authErr.WWWAuthenticate != "mutual-tls" {
		t.Errorf("WWWAuthenticate = %q, want %q", authErr.WWWAuthenticate, "mutual-tls")
	}
}

func TestCertProvider_ExpiredCertificate(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "expired-client",
		},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotAfter:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), // expired
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	// Document current behavior: expired certs are accepted at the provider
	// level (TLS layer with RequireAndVerifyClientCert rejects them before
	// the request reaches this code).
	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "expired-client" {
		t.Errorf("Subject = %q, want %q", id.Subject, "expired-client")
	}
	if id.Raw["not_after"] != "2020-01-01T00:00:00Z" {
		t.Errorf("Raw[not_after] = %v, want %q", id.Raw["not_after"], "2020-01-01T00:00:00Z")
	}
}

func TestCertProvider_EmptyCommonName_NoEmail(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: ""},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No CN, no email, no SPIFFE → empty subject.
	if id.Subject != "" {
		t.Errorf("Subject = %q, want empty", id.Subject)
	}
	if id.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty", id.DisplayName)
	}
}

func TestCertProvider_EmailFallbackSubject(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject:        pkix.Name{CommonName: ""},
		EmailAddresses: []string{"alice@example.com"},
		SerialNumber:   big.NewInt(1),
		Issuer:         pkix.Name{CommonName: "Test CA"},
		NotAfter:       time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty CN → subject falls back to email.
	if id.Subject != "alice@example.com" {
		t.Errorf("Subject = %q, want %q", id.Subject, "alice@example.com")
	}
	if id.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "alice@example.com")
	}
}

func TestCertProvider_CNTakesPrecedenceOverEmail(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject:        pkix.Name{CommonName: "alice"},
		EmailAddresses: []string{"alice@example.com"},
		SerialNumber:   big.NewInt(1),
		Issuer:         pkix.Name{CommonName: "Test CA"},
		NotAfter:       time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CN present → CN is subject, email is just email.
	if id.Subject != "alice" {
		t.Errorf("Subject = %q, want %q", id.Subject, "alice")
	}
	if id.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "alice@example.com")
	}
}

func TestCertProvider_SPIFFETakesPrecedenceOverEmail(t *testing.T) {
	p := &CertProvider{}

	spiffeURI, _ := url.Parse("spiffe://trust-domain/workload/api")

	cert := &x509.Certificate{
		Subject:        pkix.Name{CommonName: ""},
		EmailAddresses: []string{"service@example.com"},
		URIs:           []*url.URL{spiffeURI},
		SerialNumber:   big.NewInt(1),
		Issuer:         pkix.Name{CommonName: "SPIFFE CA"},
		NotAfter:       time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SPIFFE takes precedence over email fallback.
	if id.Subject != "spiffe://trust-domain/workload/api" {
		t.Errorf("Subject = %q, want SPIFFE URI", id.Subject)
	}
	if id.Email != "service@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "service@example.com")
	}
}

func TestCertProvider_SPIFFEDisplayNameFallback(t *testing.T) {
	p := &CertProvider{}

	spiffeURI, _ := url.Parse("spiffe://trust-domain/workload/api")

	// SPIFFE cert with empty CN — DisplayName should fall back to URI path.
	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: ""},
		URIs:         []*url.URL{spiffeURI},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "SPIFFE CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.DisplayName != "/workload/api" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "/workload/api")
	}
}

func TestCertProvider_MultipleSPIFFEURIs(t *testing.T) {
	p := &CertProvider{}

	spiffe1, _ := url.Parse("spiffe://trust-domain/workload/api")
	spiffe2, _ := url.Parse("spiffe://trust-domain/workload/backend")

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "multi-spiffe",
		},
		URIs:         []*url.URL{spiffe1, spiffe2},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "SPIFFE CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use the first SPIFFE URI.
	if id.Subject != "spiffe://trust-domain/workload/api" {
		t.Errorf("Subject = %q, want first SPIFFE URI", id.Subject)
	}
}

func TestCertProvider_NonSPIFFEURIs(t *testing.T) {
	p := &CertProvider{}

	nonSpiffe, _ := url.Parse("https://example.com/identity")

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "non-spiffe",
		},
		URIs:         []*url.URL{nonSpiffe},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-SPIFFE URIs should be ignored; CN used as subject.
	if id.Subject != "non-spiffe" {
		t.Errorf("Subject = %q, want %q", id.Subject, "non-spiffe")
	}
	if _, ok := id.Raw["spiffe_id"]; ok {
		t.Error("Raw[spiffe_id] should not be set for non-SPIFFE URIs")
	}
}

func TestCertProvider_NilSerialNumber(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "no-serial"},
		SerialNumber: nil,
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Raw["serial"] != "" {
		t.Errorf("Raw[serial] = %v, want empty for nil serial", id.Raw["serial"])
	}
}

func TestCertProvider_DNSSANsOnly(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: ""},
		DNSNames:     []string{"api.example.com"},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No CN, no email, no SPIFFE — subject is empty.
	// DNS SANs are not used for identity extraction.
	if id.Subject != "" {
		t.Errorf("Subject = %q, want empty", id.Subject)
	}
}

func TestCertProvider_WWWAuthenticate_MiddlewareIntegration(t *testing.T) {
	p := &CertProvider{}
	handler := Middleware(p)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	}))

	r := httptest.NewRequest("GET", "/nodes", nil)
	// No TLS → cert required error.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "mutual-tls" {
		t.Errorf("WWW-Authenticate = %q, want %q", got, "mutual-tls")
	}
}

func TestCertProvider_WhoamiCacheControl(t *testing.T) {
	p := &CertProvider{}

	spiffeURI, _ := url.Parse("spiffe://trust-domain/workload/api")
	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "test"},
		URIs:         []*url.URL{spiffeURI},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest("GET", "/auth/whoami", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

// Verify CertProvider implements Provider interface.
var _ Provider = (*CertProvider)(nil)

// Verify RegisterRoutes registers the whoami endpoint.
func TestCertProvider_RegisterRoutes(t *testing.T) {
	p := &CertProvider{}
	p.RegisterRoutes(http.NewServeMux())
}

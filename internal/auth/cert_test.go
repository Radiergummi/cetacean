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
}

func TestCertProvider_ExpiredCertificate(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "expired-client",
		},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotAfter:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	// Expired certs are accepted at the provider level — the TLS layer
	// with RequireAndVerifyClientCert rejects them before reaching here.
	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "expired-client" {
		t.Errorf("Subject = %q, want %q", id.Subject, "expired-client")
	}
}

func TestCertProvider_EmptySubjectIsError(t *testing.T) {
	p := &CertProvider{}

	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: ""},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	_, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error for cert with no identifiable subject")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
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
	if id.Subject != "alice@example.com" {
		t.Errorf("Subject = %q, want %q", id.Subject, "alice@example.com")
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

func TestCertProvider_MultipleSPIFFEURIs_Rejected(t *testing.T) {
	p := &CertProvider{}

	spiffe1, _ := url.Parse("spiffe://trust-domain/workload/api")
	spiffe2, _ := url.Parse("spiffe://trust-domain/workload/backend")

	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "multi-spiffe"},
		URIs:         []*url.URL{spiffe1, spiffe2},
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{CommonName: "SPIFFE CA"},
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	_, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error for multiple SPIFFE URIs")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestCertProvider_NonSPIFFEURIs(t *testing.T) {
	p := &CertProvider{}

	nonSpiffe, _ := url.Parse("https://example.com/identity")

	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "non-spiffe"},
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
	if id.Subject != "non-spiffe" {
		t.Errorf("Subject = %q, want %q", id.Subject, "non-spiffe")
	}
	if _, ok := id.Raw["spiffe_id"]; ok {
		t.Error("Raw[spiffe_id] should not be set for non-SPIFFE URIs")
	}
}

func TestCertProvider_SPIFFEWithNonSPIFFEURIs(t *testing.T) {
	p := &CertProvider{}

	spiffeURI, _ := url.Parse("spiffe://trust-domain/workload/api")
	otherURI, _ := url.Parse("https://example.com/identity")

	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "mixed-uris"},
		URIs:         []*url.URL{otherURI, spiffeURI},
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
	if id.Subject != "spiffe://trust-domain/workload/api" {
		t.Errorf("Subject = %q, want SPIFFE URI", id.Subject)
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

func TestCertProvider_DNSSANsOnly_NoSubject(t *testing.T) {
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

	_, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error for cert with no identifiable subject")
	}
}

func TestCertProvider_WWWAuthenticate_MiddlewareIntegration(t *testing.T) {
	p := &CertProvider{}
	handler := Middleware(p)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	}))

	r := httptest.NewRequest("GET", "/nodes", nil)
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

	handler := WhoamiHandler(p, WriteIdentityJSON)

	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

// Verify CertProvider implements Provider interface.
var _ Provider = (*CertProvider)(nil)

func TestCertProvider_RegisterRoutes(t *testing.T) {
	p := &CertProvider{}
	p.RegisterRoutes(http.NewServeMux())
}

// --- SPIFFE ID validation tests ---

func TestValidateSPIFFEID(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"valid", "spiffe://example.com/workload/api", false},
		{"valid root path", "spiffe://example.com/workload", false},
		{"valid no path", "spiffe://example.com", false},
		{"valid trust domain chars", "spiffe://my-org.example_co/svc", false},
		{"empty trust domain", "spiffe:///workload", true},
		{"uppercase trust domain", "spiffe://Example.COM/workload", true},
		{"trust domain with port", "spiffe://example.com:8080/workload", true},
		{"query component", "spiffe://example.com/workload?foo=bar", true},
		{"fragment component", "spiffe://example.com/workload#section", true},
		{"dot segment", "spiffe://example.com/./workload", true},
		{"dotdot segment", "spiffe://example.com/../workload", true},
		{"empty path segment", "spiffe://example.com/workload//api", true},
		{"trailing slash", "spiffe://example.com/workload/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.uri)
			if err != nil {
				t.Fatalf("invalid test URI: %v", err)
			}
			err = validateSPIFFEID(u)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSPIFFEID(%q) error = %v, wantErr %v", tt.uri, err, tt.wantErr)
			}
		})
	}
}

func TestExtractSPIFFEID(t *testing.T) {
	t.Run("no URIs", func(t *testing.T) {
		id, err := extractSPIFFEID(nil)
		if err != nil {
			t.Fatal(err)
		}
		if id != "" {
			t.Errorf("got %q, want empty", id)
		}
	})

	t.Run("one valid SPIFFE URI", func(t *testing.T) {
		u, _ := url.Parse("spiffe://example.com/workload")
		id, err := extractSPIFFEID([]*url.URL{u})
		if err != nil {
			t.Fatal(err)
		}
		if id != "spiffe://example.com/workload" {
			t.Errorf("got %q", id)
		}
	})

	t.Run("multiple SPIFFE URIs rejected", func(t *testing.T) {
		u1, _ := url.Parse("spiffe://example.com/a")
		u2, _ := url.Parse("spiffe://example.com/b")
		_, err := extractSPIFFEID([]*url.URL{u1, u2})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("SPIFFE plus non-SPIFFE OK", func(t *testing.T) {
		spiffe, _ := url.Parse("spiffe://example.com/workload")
		other, _ := url.Parse("https://example.com/id")
		id, err := extractSPIFFEID([]*url.URL{other, spiffe})
		if err != nil {
			t.Fatal(err)
		}
		if id != "spiffe://example.com/workload" {
			t.Errorf("got %q", id)
		}
	})

	t.Run("invalid SPIFFE ID rejected", func(t *testing.T) {
		u, _ := url.Parse("spiffe://Example.COM/workload")
		_, err := extractSPIFFEID([]*url.URL{u})
		if err == nil {
			t.Fatal("expected error for invalid trust domain")
		}
	})
}

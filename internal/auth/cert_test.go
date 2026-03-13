package auth

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
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
	if err.Error() != "client certificate required" {
		t.Errorf("error = %q, want %q", err.Error(), "client certificate required")
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
	if err.Error() != "client certificate required" {
		t.Errorf("error = %q, want %q", err.Error(), "client certificate required")
	}
}

// Verify CertProvider implements Provider interface.
var _ Provider = (*CertProvider)(nil)

// Verify RegisterRoutes is a no-op (compiles and doesn't panic).
func TestCertProvider_RegisterRoutes(t *testing.T) {
	p := &CertProvider{}
	p.RegisterRoutes(http.NewServeMux())
}

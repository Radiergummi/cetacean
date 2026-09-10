package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newClientCert builds a self-signed certificate and returns it both parsed
// and DER-encoded, so a test can hand the same certificate to the TLS
// connection state or to the Client-Cert header.
func newClientCert(t *testing.T, commonName string, uris []*url.URL) (*x509.Certificate, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(0x1a2b),
		Subject:      pkix.Name{CommonName: commonName},
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		URIs:         uris,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}

	return parsed, der
}

// clientCertHeader encodes DER as the RFC 8941 Byte Sequence RFC 9440 asks
// for: base64, no line breaks, a colon at each end.
func clientCertHeader(der []byte) string {
	return ":" + base64.StdEncoding.EncodeToString(der) + ":"
}

func spiffeURI(t *testing.T, raw string) []*url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing SPIFFE URI: %v", err)
	}

	return []*url.URL{u}
}

// TestCertProvider_HeaderFromTrustedProxy covers RFC 9440's whole point: a
// TLS-terminating proxy forwards the certificate it verified, and identity is
// built from it by the same path a directly-presented certificate takes —
// SPIFFE URI SAN included.
func TestCertProvider_HeaderFromTrustedProxy(t *testing.T) {
	_, der := newClientCert(t, "alice", spiffeURI(t, "spiffe://example.org/workload/api"))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Client-Cert", clientCertHeader(der))

	id, err := (&CertProvider{}).Authenticate(httptest.NewRecorder(), fromTrustedProxy(r))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id.Subject != "spiffe://example.org/workload/api" {
		t.Errorf("Subject = %q, want the SPIFFE ID", id.Subject)
	}
	if id.DisplayName != "alice" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "alice")
	}
	if id.Provider != "cert" {
		t.Errorf("Provider = %q, want %q", id.Provider, "cert")
	}
	if id.Raw["spiffe_id"] != "spiffe://example.org/workload/api" {
		t.Errorf("Raw[spiffe_id] = %v, want the SPIFFE ID", id.Raw["spiffe_id"])
	}
}

// TestCertProvider_HeaderFromUntrustedPeerRejected is the security property.
// RFC 9440 §3: an origin server "MUST only accept the Client-Cert and
// Client-Cert-Chain header fields from a trusted TTRP". Anyone can send the
// header; only a trusted proxy may be believed.
func TestCertProvider_HeaderFromUntrustedPeerRejected(t *testing.T) {
	_, der := newClientCert(t, "mallory", nil)

	for name, request := range map[string]func(*http.Request) *http.Request{
		"untrusted peer": fromUntrustedPeer,
		"no verdict recorded": func(r *http.Request) *http.Request {
			return r
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Client-Cert", clientCertHeader(der))

			id, err := (&CertProvider{}).Authenticate(httptest.NewRecorder(), request(r))
			if err == nil {
				t.Fatalf("Client-Cert accepted from an untrusted source: %+v", id)
			}
		})
	}
}

// TestCertProvider_PeerCertificateWinsOverHeader pins the precedence: a
// certificate presented on this connection was verified by us, and no header
// may displace it.
func TestCertProvider_PeerCertificateWinsOverHeader(t *testing.T) {
	peerCert, _ := newClientCert(t, "alice", nil)
	_, headerDER := newClientCert(t, "mallory", nil)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{peerCert}}
	r.Header.Set("Client-Cert", clientCertHeader(headerDER))

	id, err := (&CertProvider{}).Authenticate(httptest.NewRecorder(), fromTrustedProxy(r))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id.Subject != "alice" {
		t.Errorf("Subject = %q, want the peer certificate's subject", id.Subject)
	}
}

func TestCertProvider_MalformedHeader(t *testing.T) {
	_, der := newClientCert(t, "alice", nil)
	encoded := base64.StdEncoding.EncodeToString(der)

	tests := map[string]string{
		"not a byte sequence":   encoded,
		"missing closing colon": ":" + encoded,
		"not base64":            ":not base64!:",
		"base64 of nothing":     "::",
		"base64 of non-certificate": ":" + base64.StdEncoding.EncodeToString(
			[]byte("this is not a certificate"),
		) + ":",
	}

	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Client-Cert", value)

			id, err := (&CertProvider{}).Authenticate(httptest.NewRecorder(), fromTrustedProxy(r))
			if err == nil {
				t.Fatalf("malformed Client-Cert accepted: %+v", id)
			}
			if strings.Contains(err.Error(), "client certificate required") {
				t.Errorf("error = %q, want it to name the malformed header", err.Error())
			}
		})
	}
}

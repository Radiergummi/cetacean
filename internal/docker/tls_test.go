package docker

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An engine on tcp:// with --tlsverify accepts only a client presenting a
// certificate from its CA, over TLS to a certificate the client trusts.
func TestNewClientReachesAnEngineRequiringClientCertificates(t *testing.T) {
	dir := t.TempDir()
	clientCA, clientCAKey := newCA(t)
	clientCert := issueClientCert(t, clientCA, clientCAKey, dir)
	writePEM(t, filepath.Join(dir, "cert.pem"), "CERTIFICATE", clientCert)

	server := httptest.NewUnstartedServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Api-Version", RequiredAPIVersion)
		}),
	)
	pool := x509.NewCertPool()
	pool.AddCert(clientCA)
	server.TLS = &tls.Config{ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}
	server.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshakes are expected
	server.StartTLS()
	t.Cleanup(server.Close)

	writePEM(t, filepath.Join(dir, "ca.pem"), "CERTIFICATE", server.Certificate().Raw)
	host := "tcp://" + strings.TrimPrefix(server.URL, "https://")

	files := TLS{
		CA:   filepath.Join(dir, "ca.pem"),
		Cert: filepath.Join(dir, "cert.pem"),
		Key:  filepath.Join(dir, "key.pem"),
	}

	for name, tc := range map[string]struct {
		tls   TLS
		reach bool
	}{
		"with the CA and a client certificate": {files, true},
		"without TLS":                          {TLS{}, false},
		"without a client certificate":         {TLS{CA: files.CA}, false},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewClient(host, tc.tls)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			t.Cleanup(func() { _ = client.Close() })

			ping, err := client.docker.Ping(context.Background())
			if reached := err == nil && ping.APIVersion != ""; reached != tc.reach {
				t.Errorf("reached the engine = %v, want %v (error: %v)", reached, tc.reach, err)
			}
		})
	}
}

func newCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test client CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	return cert, key
}

// issueClientCert writes the client key to dir/key.pem and returns the
// certificate's DER.
func issueClientCert(
	t *testing.T,
	ca *x509.Certificate,
	caKey *ecdsa.PrivateKey,
	dir string,
) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "cetacean"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	writePEM(t, filepath.Join(dir, "key.pem"), "EC PRIVATE KEY", keyDER)

	return der
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()

	data := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

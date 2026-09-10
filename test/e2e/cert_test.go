//go:build e2e

package e2e_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// Cert mode behind a TLS-terminating proxy must start without a CA: the proxy
// verified the certificate and forwards it in Client-Cert. This configuration
// used to exit at startup.
func TestCertModeBehindProxyStartsWithoutCA(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19004,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":       "cert",
			"CETACEAN_TRUSTED_PROXIES": "127.0.0.1/32",
		},
	})

	resp := getJSON(t, proc, "/-/health", nil) //nolint:bodyclose // closed in getJSON
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// Cert mode terminating TLS itself still requires a CA — without one there is
// nothing to verify client certificates against.
func TestCertModeTerminatingTLSRequiresCA(t *testing.T) {
	env := harness.Up(t)

	code, output := sut.StartExpectingExit(t, sut.Config{
		Port:       19004,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "cert",
			"CETACEAN_TLS_CERT":  filepath.Join(env.CertDir, "server.pem"),
			"CETACEAN_TLS_KEY":   filepath.Join(env.CertDir, "server-key.pem"),
		},
	})

	if code == 0 {
		t.Errorf("exit code = 0, want non-zero; output:\n%s", output)
	}

	if !strings.Contains(output, "cert.ca") {
		t.Errorf("output does not name the missing setting:\n%s", output)
	}
}

// Direct mTLS: the SUT terminates TLS itself and verifies the client
// certificate against its own configured CA.
func TestDirectMutualTLSAuthenticates(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19004,
		DockerHost: env.DockerHost,
		TLS:        true,
		CACert:     filepath.Join(env.CertDir, "ca.pem"),
		ClientCert: filepath.Join(env.CertDir, "client.pem"),
		ClientKey:  filepath.Join(env.CertDir, "client-key.pem"),
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":    "cert",
			"CETACEAN_AUTH_CERT_CA": filepath.Join(env.CertDir, "ca.pem"),
			"CETACEAN_TLS_CERT":     filepath.Join(env.CertDir, "server.pem"),
			"CETACEAN_TLS_KEY":      filepath.Join(env.CertDir, "server-key.pem"),
		},
	})

	var identity struct {
		Subject  string `json:"subject"`
		Provider string `json:"provider"`
	}

	resp := getJSON(t, proc, "/auth/whoami", &identity) //nolint:bodyclose // closed in getJSON
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if !strings.Contains(identity.Subject, "e2e-client") {
		t.Errorf("subject = %q, want it to name the client certificate CN", identity.Subject)
	}

	if identity.Provider != "cert" {
		t.Errorf("provider = %q, want \"cert\"", identity.Provider)
	}
}

// The SUT listens plain; Caddy terminates TLS, verifies the client
// certificate itself, and forwards it to the SUT in the RFC 9440 Client-Cert
// header. The SUT trusts Caddy's address and builds the same identity from
// the forwarded certificate that direct mTLS builds from a presented one.
func TestProxyForwardedClientCertAuthenticates(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	sut.Start(t, sut.Config{
		Port:       19004,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":       "cert",
			"CETACEAN_TRUSTED_PROXIES": "0.0.0.0/0",
		},
	})

	waitCaddyReady(t)

	client := tlsClientFor(t, env)

	var identity struct {
		Subject  string `json:"subject"`
		Provider string `json:"provider"`
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"https://localhost:19104/auth/whoami", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET through caddy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.Contains(identity.Subject, "e2e-client") {
		t.Errorf("subject = %q, want the forwarded certificate's CN", identity.Subject)
	}
}

// tlsClientFor builds an *http.Client presenting the e2e client certificate
// and trusting the e2e CA, exactly as sut.buildClient does for direct mTLS —
// but this client is only ever pointed at Caddy, never at the SUT itself.
func tlsClientFor(t *testing.T, env *harness.Env) *http.Client {
	t.Helper()

	caPEM, err := os.ReadFile(filepath.Join(env.CertDir, "ca.pem"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("CA bundle is not valid PEM")
	}

	cert, err := tls.LoadX509KeyPair(
		filepath.Join(env.CertDir, "client.pem"),
		filepath.Join(env.CertDir, "client-key.pem"),
	)
	if err != nil {
		t.Fatalf("load client keypair: %v", err)
	}

	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      pool,
				Certificates: []tls.Certificate{cert},
			},
		},
	}
}

// waitCaddyReady blocks until something is listening on Caddy's port. A
// plain TCP dial is used rather than a TLS handshake: Caddy requires a
// client certificate (client_auth mode require_and_verify), so a probe
// without one would never complete the handshake even once Caddy is ready.
func waitCaddyReady(t *testing.T) {
	t.Helper()

	dialer := net.Dialer{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(t.Context(), "tcp", "127.0.0.1:19104")
		if err == nil {
			conn.Close()

			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatal("caddy not listening on :19104 within 30s")
}

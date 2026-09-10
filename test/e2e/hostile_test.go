//go:build e2e

package e2e_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/proxy"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// A proxy that adds rather than replaces Client-Cert hands the real one and a
// forged one to the provider. Reading only the first would authenticate the
// attacker.
//
// The duplicated value is the same real, CA-signed certificate
// forgedClientCertHeader (cert_test.go) already builds for
// TestUntrustedPeerClientCertIsIgnored — this test does not need a second
// header encoding, only two of them.
func TestDuplicateClientCertIsRejected(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19009,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":       "cert",
			"CETACEAN_TRUSTED_PROXIES": "127.0.0.1/32",
		},
	})

	header := forgedClientCertHeader(t, env)

	front := proxy.Start(t, proc.BaseURL, func(r *http.Request) {
		r.Header.Add("Client-Cert", header)
		r.Header.Add("Client-Cert", header)
	})

	resp, err := http.Get(
		front + "/auth/whoami",
	) //nolint:noctx,gosec // test-only, fixed loopback URL
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("status = 200; a duplicated Client-Cert must not authenticate")
	}
}

// Forwarded naming no address must not suppress the X-Forwarded-For chain.
func TestForwardedWithoutAddressFallsBackToXFF(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19009,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
		},
	})

	front := proxy.Start(t, proc.BaseURL, func(r *http.Request) {
		r.Header.Set("Forwarded", "for=unknown")
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		r.Header.Set("X-Auth-User", "someone@example.com")
	})

	resp, err := http.Get(
		front + "/auth/whoami",
	) //nolint:noctx,gosec // test-only, fixed loopback URL
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// The request log records the resolved client address under "client_ip"
	// (internal/api/middleware.go's requestLogger); assert the chain was
	// consulted rather than collapsed to the proxy's own loopback address.
	if !strings.Contains(proc.Logs(), "203.0.113.9") {
		t.Errorf("logs do not record the XFF client address:\n%s", tail(proc.Logs()))
	}
}

// tail returns the last 2000 characters of s, for embedding a bounded amount
// of process output in a failure message.
func tail(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}

	return s[len(s)-max:]
}

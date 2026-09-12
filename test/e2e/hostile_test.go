//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/proxy"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// A proxy that adds rather than replaces Client-Cert hands the real one and a
// forged one to the provider; reading only the first would authenticate the
// attacker. /auth/whoami answers 401 for any identity failure at all, so the
// positive half below drives the same SUT and proxy with a SINGLE Client-Cert
// header — that is what makes the negative half's 401 attributable to the
// duplication check rather than to the header never arriving.
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

	single := proxy.Start(t, proc.BaseURL, func(r *http.Request) {
		r.Header.Set("Client-Cert", header)
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, single+"/auth/whoami", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a single Client-Cert header", resp.StatusCode)
	}

	var identity struct {
		Subject string `json:"subject"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.Contains(identity.Subject, "e2e-client") {
		t.Errorf("subject = %q, want it to name the client certificate CN", identity.Subject)
	}

	duplicated := proxy.Start(t, proc.BaseURL, func(r *http.Request) {
		r.Header.Add("Client-Cert", header)
		r.Header.Add("Client-Cert", header)
	})

	resp2, err := http.Get(
		duplicated + "/auth/whoami",
	) //nolint:noctx,gosec // test-only, fixed loopback URL
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf(
			"status = %d, want 401; a duplicated Client-Cert must not authenticate",
			resp2.StatusCode,
		)
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
	if !waitForLog(t, proc, "203.0.113.9") {
		t.Errorf("logs do not record the XFF client address:\n%s", tail(proc.Logs()))
	}
}

// logWaitTimeout bounds waitForLog. Generous, because it only ever elapses on
// a genuine failure: the line this waits for is written microseconds after the
// response, and the poll returns as soon as it lands.
const logWaitTimeout = 10 * time.Second

// waitForLog polls the binary's output until it contains want, reporting
// whether it arrived within logWaitTimeout. Reading Logs() once right after
// the response races the line into existence: requestLogger emits after the
// handler returns, and the record still has to cross the child's stderr, the
// pipe and the copier goroutine.
func waitForLog(t *testing.T, proc *sut.Process, want string) bool {
	t.Helper()

	deadline := time.Now().Add(logWaitTimeout)

	for time.Now().Before(deadline) {
		if strings.Contains(proc.Logs(), want) {
			return true
		}

		time.Sleep(200 * time.Millisecond)
	}

	return false
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

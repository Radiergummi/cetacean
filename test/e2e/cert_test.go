//go:build e2e

package e2e_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
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
// the forwarded certificate that direct mTLS builds from a presented one:
// same subject, same provider.
//
// This is the highest-stakes test in the lane, so every failure path below
// includes the SUT's own log output — a bare "status = 401, want 200" from
// this test alone gives no hint whether the SUT rejected the forwarded
// certificate, never received it, or something else entirely went wrong.
func TestProxyForwardedClientCertAuthenticates(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19004,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":       "cert",
			"CETACEAN_TRUSTED_PROXIES": "0.0.0.0/0",
		},
	})

	waitCaddyReady(t, env)

	client := tlsClientFor(t, env)

	var identity struct {
		Subject  string `json:"subject"`
		Provider string `json:"provider"`
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"https://localhost:19104/auth/whoami", nil)
	if err != nil {
		t.Fatalf("new request: %v\n--- binary output ---\n%s", err, proc.Logs())
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET through caddy: %v\n--- binary output ---\n%s", err, proc.Logs())
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200\n--- binary output ---\n%s", resp.StatusCode, proc.Logs())
	}

	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("decode: %v\n--- binary output ---\n%s", err, proc.Logs())
	}

	if !strings.Contains(identity.Subject, "e2e-client") {
		t.Errorf("subject = %q, want the forwarded certificate's CN\n--- binary output ---\n%s",
			identity.Subject, proc.Logs())
	}

	if identity.Provider != "cert" {
		t.Errorf(
			"provider = %q, want \"cert\"\n--- binary output ---\n%s",
			identity.Provider,
			proc.Logs(),
		)
	}
}

// A forged Client-Cert header must be ignored when it did not arrive through
// a trusted proxy — this is the one place in the feature where a bug is a
// vulnerability rather than an outage: an ordinary client presenting no
// certificate of its own, but naming someone else's real one in a header,
// must not be believed.
//
// CETACEAN_TRUSTED_PROXIES is set to 10.0.0.0/8, a range that does not
// contain the loopback peer this test connects from, rather than left
// unset. Leaving it genuinely unset was tried first and does not reach this
// code at all: config.ValidateCertMode refuses to start cert mode with
// neither TLS nor any trusted proxy configured (a documented, intentionally
// untested truth-table cell — it fails closed at startup, which is its own
// coverage). 10.0.0.0/8 is the only way to get a *running* server whose
// trust verdict for this peer is still false, which is what the request-time
// rejection actually depends on.
//
// That verdict is also why one case here covers both a non-matching CIDR and
// an empty list: internal/api/realip.go's isTrusted returns false in both
// situations by the same loop finding no match, and
// auth.FromTrustedProxy (internal/auth/peer.go) short-circuits on that
// boolean before ever asking whether a verdict was recorded at all. Empty
// and non-matching are one boolean, not two branches, so a second case would
// add coverage of nothing this one doesn't already reach; see the fix-round
// report for the trace.
//
// The forged header carries the real, CA-signed e2e-client certificate
// (built by forgedClientCertHeader from client.pem) rather than nonsense
// bytes: a forged header naming a certificate the SUT's own CA would
// otherwise accept is the actual attack this test defends against, and a
// malformed one would be rejected for the wrong reason (an unparsable
// header, not an untrusted peer).
func TestUntrustedPeerClientCertIsIgnored(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19004,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":       "cert",
			"CETACEAN_TRUSTED_PROXIES": "10.0.0.0/8",
		},
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		proc.BaseURL+"/auth/whoami", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Client-Cert", forgedClientCertHeader(t, env))

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /auth/whoami: %v\n--- binary output ---\n%s", err, proc.Logs())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v\n--- binary output ---\n%s", err, proc.Logs())
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf(
			"status = %d, want 401 (a forged Client-Cert from an untrusted peer must be rejected); body: %s\n--- binary output ---\n%s",
			resp.StatusCode,
			body,
			proc.Logs(),
		)
	}

	// A partial-trust bug — accepting the header but not fully building an
	// identity from it, say — must not slip through just because the status
	// code happened to be right.
	if strings.Contains(string(body), "e2e-client") {
		t.Errorf(
			"response names the forged certificate's CN; an untrusted peer's Client-Cert must be ignored entirely: body: %s",
			body,
		)
	}
}

// forgedClientCertHeader builds an RFC 9440 Client-Cert header value from the
// real client certificate in env.CertDir — the same encoding the Caddyfile
// produces (see compose.e2e.yaml) — for a test that presents it directly,
// without a proxy, to prove an untrusted peer cannot use it to authenticate.
func forgedClientCertHeader(t *testing.T, env *harness.Env) string {
	t.Helper()

	pemBytes, err := os.ReadFile(filepath.Join(env.CertDir, "client.pem"))
	if err != nil {
		t.Fatalf("read client.pem: %v", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("client.pem has no PEM block")
	}

	return ":" + base64.StdEncoding.EncodeToString(block.Bytes) + ":"
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

// waitCaddyReady blocks until Caddy answers a real request over the same
// path the test itself uses: a TLS handshake presenting the e2e client
// certificate, against the same "localhost" host name (Caddy's strict
// SNI-Host enforcement cares which one is used). A bare TCP dial is not
// enough here — Docker's published-port forwarder accepts the connection
// before Caddy itself is listening behind it, so a dial-only probe can
// return early and leave the test itself to fail on a connection reset
// rather than a clean timeout. Retrying an actual request is the only way to
// know the whole chain, not just the socket, is up.
func waitCaddyReady(t *testing.T, env *harness.Env) {
	t.Helper()

	client := tlsClientFor(t, env)
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			"https://localhost:19104/auth/whoami", nil)
		if err == nil {
			if resp, doErr := client.Do(req); doErr == nil {
				resp.Body.Close()

				return
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatal("caddy not answering requests on :19104 within 30s")
}

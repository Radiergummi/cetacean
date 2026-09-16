//go:build e2e

package e2e_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives the authorization server's opt-in, on port 19026: the
// configurations the binary refuses outright, and the two it accepts. A unit
// test can reach resolveGuard, but only the real binary proves that a
// deployment which would serve /mcp unauthenticated never finishes starting.

const oauthOptInPort = 19026

// startupRefusal asserts the binary exits non-zero and says why.
func startupRefusal(t *testing.T, env, wants map[string]string) {
	t.Helper()

	h := harness.Up(t)

	full := map[string]string{"CETACEAN_DATA_DIR": t.TempDir()}
	maps.Copy(full, env)

	code, output := sut.StartExpectingExit(t, sut.Config{
		Port:       oauthOptInPort,
		DockerHost: h.DockerHost,
		Env:        full,
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero; output:\n%s", output)
	}

	for want, why := range wants {
		if !strings.Contains(output, want) {
			t.Errorf("the refusal does not name %q (%s):\n%s", want, why, output)
		}
	}
}

// The combination this PR exists to refuse: /mcp is exempt from the auth
// middleware, so without a bearer check or a bypassed transport mode it would
// serve the cluster's write surface to anyone who can reach the port.
func TestMCPUnderAnAuthModeWithNoGuardRefusesToStart(t *testing.T) {
	startupRefusal(t,
		map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_MCP":                  "true",
		},
		map[string]string{
			"oauth.enabled":   "the setting that fixes it",
			"mcp.auth_bypass": "the other setting that fixes it",
		},
	)
}

// Consent has to ask somebody, and "none" authenticates nobody.
func TestTheAuthorizationServerUnderAuthModeNoneRefusesToStart(t *testing.T) {
	startupRefusal(t,
		map[string]string{
			"CETACEAN_AUTH_MODE":     "none",
			"CETACEAN_OAUTH_ENABLED": "true",
		},
		map[string]string{"auth.mode": "the setting that fixes it"},
	)
}

// oidc carries an ambient session cookie, and /mcp is exempt from cross-origin
// protection precisely because it should carry none.
func TestAnAmbientModeInAuthBypassRefusesToStart(t *testing.T) {
	startupRefusal(t,
		map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_MCP":                  "true",
			"CETACEAN_MCP_AUTH_BYPASS":      "oidc",
		},
		map[string]string{"oidc": "the mode it cannot accept"},
	)
}

// A config file written for an older release carries [mcp.oauth]. Starting
// anyway would read none of it and run on defaults the operator did not pick.
func TestAConfigFileCarryingAMovedSectionRefusesToStart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cetacean.toml")
	body := "[auth]\nmode = \"none\"\n\n[mcp.oauth]\ncimd_enabled = false\n"

	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	startupRefusal(t,
		map[string]string{"CETACEAN_CONFIG": file},
		map[string]string{"mcp.oauth.cimd_enabled": "the key nothing decodes"},
	)
}

// The shape an mTLS deployment now runs: no authorization server at all, the
// certificate authenticating every /mcp call through the upstream provider.
// docs/authentication.md promises exactly this, so it is worth a real client.
func TestMCPUnderCertBypassServesWithoutAnyBearerToken(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       oauthOptInPort,
		DockerHost: env.DockerHost,
		TLS:        true,
		CACert:     filepath.Join(env.CertDir, "ca.pem"),
		ClientCert: filepath.Join(env.CertDir, "client.pem"),
		ClientKey:  filepath.Join(env.CertDir, "client-key.pem"),
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":       "cert",
			"CETACEAN_AUTH_CERT_CA":    filepath.Join(env.CertDir, "ca.pem"),
			"CETACEAN_TLS_CERT":        filepath.Join(env.CertDir, "server.pem"),
			"CETACEAN_TLS_KEY":         filepath.Join(env.CertDir, "server-key.pem"),
			"CETACEAN_MCP":             "true",
			"CETACEAN_MCP_AUTH_BYPASS": "cert",
			"CETACEAN_DATA_DIR":        t.TempDir(),
		},
	})

	// mcpCall fails the test on anything but a 200, so reaching a tool list at
	// all is the assertion: the certificate authenticated the call with no
	// bearer token and no authorization server in the deployment.
	var tools struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(mcpCall(t, proc, "tools/list", nil), &tools); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	if len(tools.Tools) == 0 {
		t.Errorf("tools/list returned nothing:\n%s", proc.Logs())
	}

	// The authorization server is off, so the document describing one must not
	// be served — a client told to authorize would have nowhere to go. Asked
	// for JSON, an unrouted path answers 404 rather than falling back to the
	// dashboard the way a browser's request does.
	metaReq, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		proc.BaseURL+"/.well-known/oauth-authorization-server",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	metaReq.Header.Set("Accept", "application/json")

	meta, err := proc.Client().Do(metaReq)
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Body.Close() //nolint:errcheck // test cleanup

	if meta.StatusCode != http.StatusNotFound {
		t.Errorf(
			"/.well-known/oauth-authorization-server = %d, want 404 with oauth.enabled off",
			meta.StatusCode,
		)
	}
}

//go:build e2e

package e2e_test

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// oidcHost is used for every request in this lane, browser-navigation and
// API alike. It must be "localhost", not proc.BaseURL's "127.0.0.1": the
// auth-flow cookies OIDCProvider sets while driving the redirect to Dex
// (state, nonce, PKCE verifier, post-login redirect target) are host-only
// per RFC 6265, and CETACEAN_AUTH_OIDC_REDIRECT_URL below names
// "localhost:19002". A client that started the flow against 127.0.0.1 would
// hold those cookies under a different cookie domain and never present them
// back to the callback, which fails closed with "missing state cookie".
const oidcBaseURL = "http://localhost:19002"

// dexIssuer must resolve identically for the SUT and for this test's HTTP
// client, or the ID token's iss claim won't validate against what the SUT's
// OIDC discovery recorded. That is normally the hard part of testing OIDC
// against a containerised issuer, but it is a non-issue here: sut.Start runs
// the real cetacean binary as a host process (see test/e2e/sut), not inside
// the compose network, so both the SUT and this test resolve
// "localhost:19010" the same way — through Dex's published port — with no
// extra_hosts or container DNS trick required.
const dexIssuer = "http://localhost:19010/dex"

// loginFormPattern extracts a login form's action attribute. Verified
// against Dex v2.46.0's actual served login page (`docker run dexidp/dex`
// against the same config added to compose.e2e.yaml): the page has exactly
// one <form method="post" action="...">, and its two inputs are
// id="login"/name="login" and id="password"/name="password" — not guessed.
var loginFormPattern = regexp.MustCompile(`<form[^>]*\baction="([^"]*)"`)

// TestOIDCLoginEstablishesASession drives the full authorization-code flow
// against a real Dex issuer: an unauthenticated request to a protected path
// redirects to Dex, the static test user's credentials are posted to the
// login form Dex actually serves, and Dex redirects back to the SUT's
// callback. The static client is configured with oauth2.skipApprovalScreen
// (see compose.e2e.yaml's dex-config), so there is no consent screen to
// handle here — the login POST goes straight to the callback. That callback
// sets the signed session cookie (__Host-cetacean_session); this test's
// cookiejar carries it to the final /auth/whoami call, so a pass here
// genuinely covers the cookie round-trip, not just the token exchange.
func TestOIDCLoginEstablishesASession(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19002,
		DockerHost: env.DockerHost,
		Env: map[string]string{ //nolint:gosec // test-only, fixed dex client secret
			"CETACEAN_AUTH_MODE":               "oidc",
			"CETACEAN_AUTH_OIDC_ISSUER":        dexIssuer,
			"CETACEAN_AUTH_OIDC_CLIENT_ID":     "cetacean",
			"CETACEAN_AUTH_OIDC_CLIENT_SECRET": "cetacean-e2e-secret",
			"CETACEAN_AUTH_OIDC_REDIRECT_URL":  oidcBaseURL + "/auth/callback",
		},
	})

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}

	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	loginURL, body := requestProtectedPath(t, client, proc)

	action := loginFormPattern.FindSubmatch(body)
	if action == nil {
		t.Fatalf(
			"login form action not found in dex response\n--- dex page ---\n%s\n--- binary output ---\n%s",
			body,
			proc.Logs(),
		)
	}

	actionURL, err := loginURL.Parse(html.UnescapeString(string(action[1])))
	if err != nil {
		t.Fatalf("parse form action %q: %v", action[1], err)
	}

	finalURL := submitLogin(t, client, actionURL)

	// oauth2.skipApprovalScreen means this is already the callback's result:
	// landing back on the SUT is proof the code exchange, ID token
	// verification, and session cookie issuance all succeeded.
	if !strings.Contains(finalURL.Host, "19002") {
		t.Fatalf(
			"not redirected back to cetacean; landed on %s\n--- binary output ---\n%s",
			finalURL,
			proc.Logs(),
		)
	}

	assertWhoamiReportsDexIdentity(t, client, proc)
}

// requestProtectedPath makes the initial, unauthenticated request a browser
// would, and follows the redirect chain through to Dex's login form.
func requestProtectedPath(t *testing.T, client *http.Client, proc *sut.Process) (*url.URL, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		oidcBaseURL+"/services",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	// OIDCProvider.Authenticate only redirects a request that looks like a
	// browser navigation (Accept containing text/html); anything else gets a
	// bare 401 with no redirect at all.
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("initial GET: %v\n--- binary output ---\n%s", err, proc.Logs())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	loginURL := resp.Request.URL
	if !strings.Contains(loginURL.Host, "19010") {
		t.Fatalf(
			"not redirected to dex; landed on %s\n--- page ---\n%s\n--- binary output ---\n%s",
			loginURL, body, proc.Logs(),
		)
	}

	return loginURL, body
}

// submitLogin posts the static test user's credentials to the login form
// action Dex served, and returns the URL the redirect chain ends on.
func submitLogin(t *testing.T, client *http.Client, actionURL *url.URL) *url.URL {
	t.Helper()

	values := url.Values{
		"login":    {"dev@localhost"},
		"password": {"password"},
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, actionURL.String(), strings.NewReader(values.Encode()),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login POST: %v", err)
	}
	defer resp.Body.Close()

	return resp.Request.URL
}

// assertWhoamiReportsDexIdentity reads back the identity the session cookie
// now carries, confirming it names the Dex static user rather than merely
// confirming the redirect chain completed.
func assertWhoamiReportsDexIdentity(t *testing.T, client *http.Client, proc *sut.Process) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		oidcBaseURL+"/auth/whoami",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(
			"whoami status = %d, want 200\n--- binary output ---\n%s",
			resp.StatusCode,
			proc.Logs(),
		)
	}

	var identity struct {
		Subject  string `json:"subject"`
		Email    string `json:"email"`
		Provider string `json:"provider"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("decode identity: %v", err)
	}

	if identity.Email != "dev@localhost" {
		t.Errorf("email = %q, want dev@localhost", identity.Email)
	}

	if identity.Provider != "oidc" {
		t.Errorf("provider = %q, want oidc", identity.Provider)
	}
}

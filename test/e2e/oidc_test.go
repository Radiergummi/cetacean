//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
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

// oidcHost is used for every request in this lane. It must be "localhost", not
// proc.BaseURL's "127.0.0.1": the auth-flow cookies OIDCProvider sets are
// host-only per RFC 6265 and CETACEAN_AUTH_OIDC_REDIRECT_URL names
// "localhost:19002", so a flow started against 127.0.0.1 never presents them
// back to the callback.
const oidcBaseURL = "http://localhost:19002"

// dexIssuer must resolve identically for the SUT and for this test's HTTP
// client, or the ID token's iss claim won't validate against what the SUT's
// OIDC discovery recorded. sut.Start runs the real binary as a host process
// rather than inside the compose network, so both resolve "localhost:19010"
// through Dex's published port.
const dexIssuer = "http://localhost:19010/dex"

// sessionCookieName mirrors the unexported cookieName in internal/auth's
// session.go (the __Host- prefix and value are not exported, so this names
// it again rather than importing for one string).
const sessionCookieName = "__Host-cetacean_session"

// loginFormPattern extracts a login form's action attribute. Dex's served login
// page has exactly one <form method="post" action="...">, with inputs named
// "login" and "password".
var loginFormPattern = regexp.MustCompile(`<form[^>]*\baction="([^"]*)"`)

// TestOIDCLoginEstablishesASession drives the full authorization-code flow
// against a real Dex issuer. The static client sets oauth2.skipApprovalScreen,
// so the login POST goes straight to the callback. That callback sets the
// signed session cookie, which this test's cookiejar carries to /auth/whoami —
// so a pass covers the cookie round-trip, not just the token exchange.
func TestOIDCLoginEstablishesASession(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	// dex has no compose healthcheck, and main.go performs OIDC discovery
	// synchronously at startup, exiting on failure — so without this a cold start
	// kills the SUT instead of failing the test with a clear message.
	waitDexReady(t)

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

	// Negative control: a passing assertion at the end of this test should
	// mean "the login flow works", not merely "some path to 200 exists". Run
	// this against the same client, before its jar holds anything, so the
	// 200 later cannot be explained by state left over from this check.
	assertUnauthenticatedIsRefused(t, client, proc)

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

	assertSessionCookieIsSet(t, client)
	assertWhoamiReportsDexIdentity(t, client, proc)
}

// waitDexReady blocks until Dex's own OIDC discovery document is served and
// names the issuer this test configures. Polling the document rather than
// dialling the port catches an issuer mismatch here, by name, instead of
// later as a token-validation failure deep in the flow.
func waitDexReady(t *testing.T) {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(30 * time.Second)

	var lastErr error

	for time.Now().Before(deadline) {
		lastErr = probeDexDiscovery(t, client)
		if lastErr == nil {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("dex not serving its discovery document at %s within 30s: %v", dexIssuer, lastErr)
}

// probeDexDiscovery makes one attempt at the check waitDexReady polls.
func probeDexDiscovery(t *testing.T, client *http.Client) error {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodGet, dexIssuer+"/.well-known/openid-configuration", nil,
	)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var discovery struct {
		Issuer string `json:"issuer"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return fmt.Errorf("decode discovery document: %w", err)
	}

	if discovery.Issuer != dexIssuer {
		return fmt.Errorf("issuer = %q, want %q", discovery.Issuer, dexIssuer)
	}

	return nil
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

// whoamiRequest issues one GET /auth/whoami through client, the one request
// shape both the negative control and the final identity check need.
func whoamiRequest(t *testing.T, client *http.Client) *http.Response {
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

	return resp
}

// assertUnauthenticatedIsRefused is this lane's negative control: without it, a
// passing login test would only show that logging in works, not that it is
// required.
func assertUnauthenticatedIsRefused(t *testing.T, client *http.Client, proc *sut.Process) {
	t.Helper()

	resp := whoamiRequest(t, client)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf(
			"whoami status = %d, want 401 (an unauthenticated caller must be refused)\n--- binary output ---\n%s",
			resp.StatusCode,
			proc.Logs(),
		)
	}
}

// assertSessionCookieIsSet pins the mechanism this lane exists to prove: the
// callback actually left the signed session cookie in the jar. Without this,
// a passing identity check below could in principle be explained by
// something other than the session cookie the callback is supposed to set.
func assertSessionCookieIsSet(t *testing.T, client *http.Client) {
	t.Helper()

	u, err := url.Parse(oidcBaseURL)
	if err != nil {
		t.Fatalf("parse %s: %v", oidcBaseURL, err)
	}

	for _, c := range client.Jar.Cookies(u) {
		if c.Name == sessionCookieName {
			return
		}
	}

	t.Errorf("%s not present in jar after callback", sessionCookieName)
}

// assertWhoamiReportsDexIdentity reads back the identity the session cookie
// now carries, confirming it names the Dex static user rather than merely
// confirming the redirect chain completed.
func assertWhoamiReportsDexIdentity(t *testing.T, client *http.Client, proc *sut.Process) {
	t.Helper()

	resp := whoamiRequest(t, client)
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

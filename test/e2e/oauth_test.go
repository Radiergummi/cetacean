//go:build e2e

package e2e_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives Cetacean's MCP OAuth 2.1 authorization server the way an
// MCP client drives it: discover from a 401, register, walk the consent page,
// redeem a code with PKCE, call /mcp with the bearer token, rotate the refresh
// token, and survive a restart. Reserves port 19011.

const oauthPort = 19011

// oauthIssuer must be set explicitly: setupMCP refuses to start when OAuth is
// in play and no reachable issuer can be derived, and the derived one here
// would be "http://:19011". Everything else in the lane is discovered from it.
const oauthIssuer = "http://127.0.0.1:19011"

// oauthSigningKey fixes the HMAC key so access tokens minted before a restart
// still verify after it; an ephemeral key would be indistinguishable from lost
// persisted state. config.LoadMCP rejects anything shorter than 32 bytes.
const oauthSigningKey = "e2e-oauth-signing-key-32-bytes!!"

// oauthPolicy grants everything to group:ops and nothing to anyone else, so the
// lane can ask whether the `groups` claim minted at consent time reaches the
// ACL evaluator through the bearer path.
const oauthPolicy = `grants:
  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["read", "write"]
`

// The two personas, mirroring readPersonas' "ops" and "anonymous" entries so a
// finding here names the same people as the read and MCP sweeps.
var (
	oauthGranted   = readPersona{name: "ops", user: "ops@example.com", groups: "ops"}
	oauthUngranted = readPersona{name: "anonymous", user: "nobody@example.com"}
)

// oauthRedirectURI is a loopback callback nothing listens on: sut.Process's
// client returns http.ErrUseLastResponse, so the 302 is the response under
// test. Loopback http is what RFC 8252 §7.3 native clients use.
const oauthRedirectURI = "http://127.0.0.1:19999/callback"

// startOAuth brings up a headers-auth SUT with MCP and its OAuth 2.1
// authorization server enabled. dataDir is passed in rather than derived so
// TestMCPOAuthStateSurvivesARestart can point two consecutive processes at the
// same state file.
func startOAuth(
	t *testing.T,
	env *harness.Env,
	dataDir string,
	extra map[string]string,
) *sut.Process {
	t.Helper()

	policy := filepath.Join(dataDir, "acl.yaml")
	if err := os.WriteFile(policy, []byte(oauthPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	environment := map[string]string{
		"CETACEAN_AUTH_MODE":            "headers",
		"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
		"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
		"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
		"CETACEAN_ACL_POLICY_FILE":      policy,
		"CETACEAN_OPERATIONS_LEVEL":     "2",
		"CETACEAN_MCP":                  "true",
		"CETACEAN_MCP_ISSUER":           oauthIssuer,
		"CETACEAN_MCP_SIGNING_KEY":      oauthSigningKey,
		"CETACEAN_DATA_DIR":             dataDir,

		// The production default is 10 registrations per IP per hour, which the
		// cases below would exhaust; the limit itself is driven by its own test,
		// on a SUT whose bucket nothing else shares.
		"CETACEAN_MCP_DCR_RATE_LIMIT": "500",
	}

	maps.Copy(environment, extra)

	return sut.Start(t, sut.Config{
		Port:       oauthPort,
		DockerHost: env.DockerHost,
		Env:        environment,
	})
}

// ---------------------------------------------------------------------------
// HTTP plumbing
// ---------------------------------------------------------------------------

// httpOutcome is one response, fully read. Every helper in this file returns
// one rather than an *http.Response: a refusal is an outcome these cases
// assert on, so nothing may fail the test on a status by itself, and the body
// has to survive the deferred Close.
type httpOutcome struct {
	status int
	header http.Header
	body   string
}

// location parses the Location header of a redirect response.
func (o httpOutcome) location(t *testing.T) *url.URL {
	t.Helper()

	raw := o.header.Get("Location")
	if raw == "" {
		t.Fatalf("expected a Location header; got status %d, body: %s", o.status, o.body)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse Location %q: %v", raw, err)
	}

	return parsed
}

func send(t *testing.T, proc *sut.Process, req *http.Request) httpOutcome {
	t.Helper()

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", req.Method, req.URL, err)
	}

	return httpOutcome{status: resp.StatusCode, header: resp.Header.Clone(), body: string(body)}
}

// oauthGet issues a GET, optionally as a persona (an empty persona name sends
// no identity headers, which is how the unauthenticated case is driven).
func oauthGet(
	t *testing.T,
	proc *sut.Process,
	rawURL string,
	persona readPersona,
) httpOutcome {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", rawURL, err)
	}

	applyPersona(req, persona)

	return send(t, proc, req)
}

// oauthPostForm issues an application/x-www-form-urlencoded POST, attaching
// any cookies the caller carries forward from an earlier response.
func oauthPostForm(
	t *testing.T,
	proc *sut.Process,
	rawURL string,
	form url.Values,
	persona readPersona,
	cookies ...*http.Cookie,
) httpOutcome {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, rawURL, strings.NewReader(form.Encode()),
	)
	if err != nil {
		t.Fatalf("new request %s: %v", rawURL, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyPersona(req, persona)

	for _, cookie := range cookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}

	return send(t, proc, req)
}

func oauthPostJSON(t *testing.T, proc *sut.Process, rawURL string, body any) httpOutcome {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, rawURL, bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("new request %s: %v", rawURL, err)
	}

	req.Header.Set("Content-Type", "application/json")

	return send(t, proc, req)
}

func applyPersona(req *http.Request, persona readPersona) {
	if persona.user == "" {
		return
	}

	req.Header.Set("X-Auth-User", persona.user)

	if persona.groups != "" {
		req.Header.Set("X-Auth-Groups", persona.groups)
	}
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

// oauthDiscovery is everything a client learns before it can ask for a token.
type oauthDiscovery struct {
	resource      string
	issuer        string
	authorizeURL  string
	tokenURL      string
	revokeURL     string
	registerURL   string
	cimdSupported bool
	prmURL        string
	metadataBody  string
}

type asMetadataDocument struct {
	Issuer                                 string   `json:"issuer"`
	AuthorizationEndpoint                  string   `json:"authorization_endpoint"`
	TokenEndpoint                          string   `json:"token_endpoint"`
	RevocationEndpoint                     string   `json:"revocation_endpoint"`
	RegistrationEndpoint                   string   `json:"registration_endpoint"`
	ClientIDMetadataDocumentSupported      bool     `json:"client_id_metadata_document_supported"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	GrantTypesSupported                    []string `json:"grant_types_supported"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
}

type prmDocument struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceDocumentation  string   `json:"resource_documentation"`
}

// authParamPattern reads `key="value"` pairs out of a WWW-Authenticate header.
var authParamPattern = regexp.MustCompile(`([a-zA-Z_-]+)="([^"]*)"`)

// discoverOAuth follows the chain a real MCP client follows from nothing but the
// endpoint URL: an unauthenticated call yields a 401 whose WWW-Authenticate
// names the protected-resource metadata, which names the authorization servers,
// each serving RFC 8414 metadata naming the endpoints.
func discoverOAuth(t *testing.T, proc *sut.Process) oauthDiscovery {
	t.Helper()

	challenge, status := mcpWithToken(t, proc, "", "tools/list", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /mcp: status = %d, want 401 (envelope: %+v)", status, challenge)
	}

	header := challenge.wwwAuthenticate
	if !strings.HasPrefix(header, "Bearer ") {
		t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", header)
	}

	params := map[string]string{}
	for _, match := range authParamPattern.FindAllStringSubmatch(header, -1) {
		params[match[1]] = match[2]
	}

	prmURL := params["resource_metadata"]
	if prmURL == "" {
		t.Fatalf("WWW-Authenticate = %q carries no resource_metadata parameter", header)
	}

	if got := params["error"]; got != "invalid_token" {
		t.Errorf("WWW-Authenticate error = %q, want invalid_token", got)
	}

	prmOutcome := oauthGet(t, proc, prmURL, readPersona{})
	if prmOutcome.status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body: %s", prmURL, prmOutcome.status, prmOutcome.body)
	}

	var prm prmDocument
	if err := json.Unmarshal([]byte(prmOutcome.body), &prm); err != nil {
		t.Fatalf("decode PRM: %v\nbody: %s", err, prmOutcome.body)
	}

	if len(prm.AuthorizationServers) != 1 {
		t.Fatalf("PRM authorization_servers = %v, want exactly one", prm.AuthorizationServers)
	}

	asURL := prm.AuthorizationServers[0] + "/.well-known/oauth-authorization-server"

	asOutcome := oauthGet(t, proc, asURL, readPersona{})
	if asOutcome.status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body: %s", asURL, asOutcome.status, asOutcome.body)
	}

	var metadata asMetadataDocument
	if err := json.Unmarshal([]byte(asOutcome.body), &metadata); err != nil {
		t.Fatalf("decode AS metadata: %v\nbody: %s", err, asOutcome.body)
	}

	return oauthDiscovery{
		resource:      prm.Resource,
		issuer:        metadata.Issuer,
		authorizeURL:  metadata.AuthorizationEndpoint,
		tokenURL:      metadata.TokenEndpoint,
		revokeURL:     metadata.RevocationEndpoint,
		registerURL:   metadata.RegistrationEndpoint,
		cimdSupported: metadata.ClientIDMetadataDocumentSupported,
		prmURL:        prmURL,
		metadataBody:  asOutcome.body,
	}
}

// ---------------------------------------------------------------------------
// PKCE, registration, consent
// ---------------------------------------------------------------------------

// pkcePair produces an RFC 7636 §4.1 verifier (43 characters of base64url,
// inside the mandated [43,128] range and unreserved alphabet) and its S256
// challenge.
func pkcePair(t *testing.T) (verifier, challenge string) {
	t.Helper()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("rand: %v", err)
	}

	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))

	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ApplicationType         string   `json:"application_type"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
}

// registerClient performs RFC 7591 dynamic client registration and returns the
// raw outcome, so rejection cases can assert on the error body.
func registerClient(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	metadata map[string]any,
) httpOutcome {
	t.Helper()

	if discovery.registerURL == "" {
		t.Fatal("AS metadata advertises no registration_endpoint")
	}

	return oauthPostJSON(t, proc, discovery.registerURL, metadata)
}

// registerLoopbackClient registers the ordinary public native client every
// happy-path case in this file uses, and fails the test if registration did
// not succeed.
func registerLoopbackClient(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	name string,
) string {
	t.Helper()

	outcome := registerClient(t, proc, discovery, map[string]any{
		"client_name":   name,
		"redirect_uris": []string{oauthRedirectURI},
	})

	if outcome.status != http.StatusCreated {
		t.Fatalf("register %s: status = %d, body: %s", name, outcome.status, outcome.body)
	}

	var registration registrationResponse
	if err := json.Unmarshal([]byte(outcome.body), &registration); err != nil {
		t.Fatalf("decode registration: %v\nbody: %s", err, outcome.body)
	}

	if registration.ClientID == "" {
		t.Fatalf("registration returned an empty client_id: %s", outcome.body)
	}

	return registration.ClientID
}

// authorizeRequest is the set of query parameters an authorization request
// carries. Every field is explicit — including the ones a happy path would
// default — because the refusal cases differ from the happy path in exactly
// one of them, and a helper that filled them in would hide which.
type authorizeRequest struct {
	clientID     string
	redirectURI  string
	responseType string
	challenge    string
	method       string
	state        string

	// resource is omitted from the query entirely when empty, which is how
	// the RFC 8707 required-indicator refusal is driven.
	resource string
}

func (a authorizeRequest) query() url.Values {
	values := url.Values{}
	values.Set("response_type", a.responseType)
	values.Set("client_id", a.clientID)
	values.Set("redirect_uri", a.redirectURI)
	values.Set("code_challenge", a.challenge)
	values.Set("code_challenge_method", a.method)

	if a.state != "" {
		values.Set("state", a.state)
	}

	if a.resource != "" {
		values.Set("resource", a.resource)
	}

	return values
}

// newAuthorizeRequest builds a well-formed request that only the case's own
// mutation makes invalid.
func newAuthorizeRequest(
	discovery oauthDiscovery,
	clientID, challenge, state string,
) authorizeRequest {
	return authorizeRequest{
		clientID:     clientID,
		redirectURI:  oauthRedirectURI,
		responseType: "code",
		challenge:    challenge,
		method:       "S256",
		state:        state,
		resource:     discovery.resource,
	}
}

// hiddenInputPattern reads the consent form's hidden fields. The consent page
// is a fixed template literal emitting exactly this shape, so a regexp avoids
// promoting golang.org/x/net/html to a direct dependency. Values are
// attribute-escaped by html/template, hence the unescape.
var hiddenInputPattern = regexp.MustCompile(
	`<input type="hidden" name="([^"]+)" value="([^"]*)">`,
)

// consentPage is a rendered authorization page plus the CSRF nonce cookie
// issued alongside it. The two travel together because neither is any use
// without the other: verifyCSRFToken recomputes the HMAC from the cookie's
// nonce and the form's state and fingerprint.
type consentPage struct {
	outcome httpOutcome
	fields  url.Values
	cookie  *http.Cookie
}

// requestConsent drives GET /oauth/authorize and returns whatever came back.
// It does not assert a status: the refusal cases go through here too.
func requestConsent(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	persona readPersona,
	request authorizeRequest,
) consentPage {
	t.Helper()

	outcome := oauthGet(
		t, proc, discovery.authorizeURL+"?"+request.query().Encode(), persona,
	)

	page := consentPage{outcome: outcome, fields: url.Values{}}

	for _, match := range hiddenInputPattern.FindAllStringSubmatch(outcome.body, -1) {
		page.fields.Set(match[1], html.UnescapeString(match[2]))
	}

	for _, cookie := range readSetCookies(outcome.header) {
		if cookie.Name == "mcp_csrf_nonce" && cookie.Value != "" {
			page.cookie = cookie
		}
	}

	return page
}

// readSetCookies parses Set-Cookie headers off a recorded outcome. The
// response body is long gone by then, so http.Response.Cookies is not
// available; http.ReadResponse on a synthetic header block is the stdlib's
// only exported parser for this.
func readSetCookies(header http.Header) []*http.Cookie {
	return (&http.Response{Header: header}).Cookies()
}

// requireConsentPage asserts the authorization request rendered a consent
// form, and returns it. A refusal renders the error template instead, which
// has no hidden inputs at all.
func requireConsentPage(t *testing.T, page consentPage) consentPage {
	t.Helper()

	if page.outcome.status != http.StatusOK {
		t.Fatalf(
			"consent page: status = %d, want 200; body: %s",
			page.outcome.status, page.outcome.body,
		)
	}

	if page.cookie == nil {
		t.Fatal("consent page issued no mcp_csrf_nonce cookie")
	}

	for _, required := range []string{"csrf_token", "consent_fingerprint", "client_id"} {
		if page.fields.Get(required) == "" {
			t.Fatalf("consent form has no %s field; body: %s", required, page.outcome.body)
		}
	}

	return page
}

// decide submits the consent form with the given decision ("approve" or
// "deny"), optionally overriding form fields — which is how the CSRF binding
// cases tamper with exactly one value.
func decide(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	persona readPersona,
	page consentPage,
	decision string,
	overrides map[string]string,
) httpOutcome {
	t.Helper()

	form := url.Values{}
	maps.Copy(form, page.fields)
	form.Set("decision", decision)

	for key, value := range overrides {
		form.Set(key, value)
	}

	return oauthPostForm(t, proc, discovery.authorizeURL, form, persona, page.cookie)
}

// authorizationCode walks the whole browser half — consent page, approval,
// redirect — and returns the code plus the query the client was redirected
// with, so a caller can assert on `state` and the RFC 9207 `iss`.
func authorizationCode(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	persona readPersona,
	clientID, challenge, state string,
) (string, url.Values) {
	t.Helper()

	page := requireConsentPage(t, requestConsent(
		t, proc, discovery, persona,
		newAuthorizeRequest(discovery, clientID, challenge, state),
	))

	outcome := decide(t, proc, discovery, persona, page, "approve", nil)
	if outcome.status != http.StatusFound {
		t.Fatalf("approve: status = %d, want 302; body: %s", outcome.status, outcome.body)
	}

	query := outcome.location(t).Query()

	code := query.Get("code")
	if code == "" {
		t.Fatalf("approval redirect carries no code: %s", outcome.header.Get("Location"))
	}

	return code, query
}

// ---------------------------------------------------------------------------
// Token endpoint
// ---------------------------------------------------------------------------

type tokenSet struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// redeemCode posts an authorization_code grant. Every parameter is explicit so
// the mismatch cases can vary one.
func redeemCode(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	clientID, redirectURI, code, verifier, resource string,
) httpOutcome {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)

	if resource != "" {
		form.Set("resource", resource)
	}

	return oauthPostForm(t, proc, discovery.tokenURL, form, readPersona{})
}

func refreshGrant(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	refreshToken, resource string,
) httpOutcome {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	if resource != "" {
		form.Set("resource", resource)
	}

	return oauthPostForm(t, proc, discovery.tokenURL, form, readPersona{})
}

// requireTokens asserts a successful token response and decodes it.
func requireTokens(t *testing.T, outcome httpOutcome) tokenSet {
	t.Helper()

	if outcome.status != http.StatusOK {
		t.Fatalf("token endpoint: status = %d, body: %s", outcome.status, outcome.body)
	}

	var tokens tokenSet
	if err := json.Unmarshal([]byte(outcome.body), &tokens); err != nil {
		t.Fatalf("decode token response: %v\nbody: %s", err, outcome.body)
	}

	if tokens.AccessToken == "" {
		t.Fatalf("token response carries no access_token: %s", outcome.body)
	}

	if tokens.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", tokens.TokenType)
	}

	return tokens
}

// theftRefreshTokenDescription is the wording handleRefreshTokenGrant's theft
// branch answers with. It is `invalid_grant` on the wire like every other
// refusal — RFC 6749 §5.2 forbids telling the caller it was theft — so the
// description is the only thing that says which branch answered.
const theftRefreshTokenDescription = "refresh token is invalid"

// refreshReplayOutcome records what replaying a consumed refresh token did.
type refreshReplayOutcome struct {
	replayStatus      int
	replayDescription string

	// familyBurned reports whether the *live* token the replayed one rotated
	// into stopped working. That, not the refusal of the replay itself, is
	// the property: a server that merely forgot the consumed token also
	// refuses the replay, and leaves the thief's copy working.
	familyBurned bool
}

// driveRefreshReplay runs a whole grant through rotation and then replays the
// consumed token, reporting what happened to the family. resource is sent when
// non-empty and omitted otherwise, which is the axis finding D-5 turns on.
func driveRefreshReplay(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	persona readPersona,
	clientName, resource string,
) refreshReplayOutcome {
	t.Helper()

	original, _ := completeFlow(t, proc, discovery, persona, clientName)

	rotated := requireTokens(
		t, refreshGrant(t, proc, discovery, original.RefreshToken, resource),
	)

	replay := refreshGrant(t, proc, discovery, original.RefreshToken, resource)
	requireOAuthError(t, replay, http.StatusBadRequest, "invalid_grant")

	var failure oauthError
	if err := json.Unmarshal([]byte(replay.body), &failure); err != nil {
		t.Fatalf("decode replay refusal: %v\nbody: %s", err, replay.body)
	}

	if strings.Contains(strings.ToLower(replay.body), "theft") {
		t.Errorf(
			"the refusal names the detection: %s; RFC 6749 §5.2 says not to tell the "+
				"caller which failure it hit",
			replay.body,
		)
	}

	after := refreshGrant(t, proc, discovery, rotated.RefreshToken, resource)

	return refreshReplayOutcome{
		replayStatus:      replay.status,
		replayDescription: failure.ErrorDescription,
		familyBurned:      after.status != http.StatusOK,
	}
}

// requireOAuthError asserts an RFC 6749 §5.2 error response with the expected
// code.
func requireOAuthError(t *testing.T, outcome httpOutcome, wantStatus int, wantCode string) {
	t.Helper()

	if outcome.status != wantStatus {
		t.Errorf("status = %d, want %d; body: %s", outcome.status, wantStatus, outcome.body)
	}

	var failure oauthError
	if err := json.Unmarshal([]byte(outcome.body), &failure); err != nil {
		t.Fatalf("decode error response: %v\nbody: %s", err, outcome.body)
	}

	if failure.Error != wantCode {
		t.Errorf("error = %q, want %q (description: %q)",
			failure.Error, wantCode, failure.ErrorDescription)
	}
}

// completeFlow registers a client and runs the whole authorization code flow
// for persona, returning the tokens and the client it registered.
func completeFlow(
	t *testing.T,
	proc *sut.Process,
	discovery oauthDiscovery,
	persona readPersona,
	clientName string,
) (tokenSet, string) {
	t.Helper()

	clientID := registerLoopbackClient(t, proc, discovery, clientName)
	verifier, challenge := pkcePair(t)

	code, _ := authorizationCode(
		t, proc, discovery, persona, clientID, challenge, "state-"+clientName,
	)

	outcome := redeemCode(
		t, proc, discovery, clientID, oauthRedirectURI, code, verifier, discovery.resource,
	)

	return requireTokens(t, outcome), clientID
}

// ---------------------------------------------------------------------------
// Access tokens
// ---------------------------------------------------------------------------

// accessTokenClaims is the JWT payload internal/mcp/oauth/jwt.go mints. The
// lane decodes rather than verifies: the signature is the server's business,
// and the assertion worth making from outside is that the claims describe the
// identity and audience the flow established.
type accessTokenClaims struct {
	Issuer    string   `json:"iss"`
	Audience  string   `json:"aud"`
	Subject   string   `json:"sub"`
	Groups    []string `json:"groups"`
	ClientID  string   `json:"client_id"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	JTI       string   `json:"jti"`
}

func decodeAccessToken(t *testing.T, token string) accessTokenClaims {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("access token has %d segments, want 3 (compact JWS)", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}

	var claims accessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT payload: %v\npayload: %s", err, payload)
	}

	return claims
}

// mcpEnvelopeWithChallenge carries the WWW-Authenticate header alongside the
// JSON-RPC envelope, because the unauthenticated case's whole payload is that
// header rather than a body.
type mcpEnvelopeWithChallenge struct {
	mcpEnvelope

	wwwAuthenticate string
}

// mcpWithToken issues one JSON-RPC call against /mcp authenticated by an OAuth
// bearer token rather than by proxy headers — the path mcp_sweep_test.go's
// mcpAs deliberately bypasses. An empty token sends no Authorization header.
func mcpWithToken(
	t *testing.T,
	proc *sut.Process,
	token, method string,
	params map[string]any,
) (mcpEnvelopeWithChallenge, int) {
	t.Helper()

	if params == nil {
		params = map[string]any{}
	}

	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, proc.BaseURL+"/mcp", bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if name, ok := mcpNameHeader(method, params); ok {
		req.Header.Set("Mcp-Name", name)
	}

	outcome := send(t, proc, req)

	result := mcpEnvelopeWithChallenge{
		wwwAuthenticate: outcome.header.Get("WWW-Authenticate"),
	}

	var raw struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal([]byte(outcome.body), &raw); err != nil {
		// A 401 from bearerAuth has an empty body and only the challenge
		// header, which is exactly what discoverOAuth reads.
		if outcome.status != http.StatusOK {
			return result, outcome.status
		}

		t.Fatalf("decode %s response: %v\nbody: %s", method, err, outcome.body)
	}

	result.Result = raw.Result

	if raw.Error != nil {
		result.Error = &raw.Error.Message
	}

	return result, outcome.status
}

// toolNames lists the tools an identity is offered.
func toolNames(t *testing.T, proc *sut.Process, token string) []string {
	t.Helper()

	envelope, status := mcpWithToken(t, proc, token, "tools/list", nil)
	if status != http.StatusOK {
		t.Fatalf("tools/list: status = %d, error: %v", status, envelope.Error)
	}

	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(envelope.Result, &listed); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}

	slices.Sort(names)

	return names
}

// ---------------------------------------------------------------------------
// The lane
// ---------------------------------------------------------------------------

// TestMCPOAuthFlow drives the authorization server end to end against a real
// cluster: discovery, registration, consent, PKCE, token exchange, the bearer
// path into /mcp, refresh rotation with theft detection, and revocation.
func TestMCPOAuthFlow(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startOAuth(t, env, t.TempDir(), nil)
	discovery := discoverOAuth(t, proc)

	t.Run("discovery", func(t *testing.T) {
		if discovery.issuer != oauthIssuer {
			t.Errorf("AS metadata issuer = %q, want %q", discovery.issuer, oauthIssuer)
		}

		if want := oauthIssuer + "/mcp"; discovery.resource != want {
			t.Errorf("PRM resource = %q, want %q", discovery.resource, want)
		}

		want := map[string]string{
			"authorization_endpoint": oauthIssuer + "/oauth/authorize",
			"token_endpoint":         oauthIssuer + "/oauth/token",
			"revocation_endpoint":    oauthIssuer + "/oauth/revoke",
			"registration_endpoint":  oauthIssuer + "/oauth/register",
		}

		got := map[string]string{
			"authorization_endpoint": discovery.authorizeURL,
			"token_endpoint":         discovery.tokenURL,
			"revocation_endpoint":    discovery.revokeURL,
			"registration_endpoint":  discovery.registerURL,
		}

		for name, expected := range want {
			if got[name] != expected {
				t.Errorf("%s = %q, want %q", name, got[name], expected)
			}
		}

		if !discovery.cimdSupported {
			t.Error(
				"client_id_metadata_document_supported is absent; CIMD is enabled by " +
					"default and a client has no other way to learn an https:// " +
					"client_id will be accepted",
			)
		}

		// OAuth 2.1 removes the implicit grant and mandates PKCE; the server
		// must advertise only what it will actually accept, since a client
		// that believes "plain" is available will be refused at /authorize.
		var metadata asMetadataDocument
		if err := json.Unmarshal([]byte(discovery.metadataBody), &metadata); err != nil {
			t.Fatalf("decode AS metadata: %v", err)
		}

		assertSetEqual(t, "code_challenge_methods_supported",
			metadata.CodeChallengeMethodsSupported, []string{"S256"})
		assertSetEqual(t, "grant_types_supported",
			metadata.GrantTypesSupported, []string{"authorization_code", "refresh_token"})
		assertSetEqual(t, "response_types_supported",
			metadata.ResponseTypesSupported, []string{"code"})
		assertSetEqual(t, "token_endpoint_auth_methods_supported",
			metadata.TokenEndpointAuthMethodsSupported, []string{"none"})
	})

	t.Run("openid_configuration_aliases_the_rfc8414_document", func(t *testing.T) {
		// RegisterRoutes serves both paths from HandleMetadata. A client that
		// only knows OIDC discovery must find the same server, not a subtly
		// different one.
		alias := oauthGet(
			t, proc, oauthIssuer+"/.well-known/openid-configuration", readPersona{},
		)

		if alias.status != http.StatusOK {
			t.Fatalf("openid-configuration: status = %d", alias.status)
		}

		if alias.body != discovery.metadataBody {
			t.Errorf(
				"openid-configuration differs from oauth-authorization-server\n"+
					"alias: %s\nRFC 8414: %s",
				alias.body, discovery.metadataBody,
			)
		}
	})

	t.Run("protected_resource_metadata", func(t *testing.T) {
		outcome := oauthGet(t, proc, discovery.prmURL, readPersona{})

		var prm prmDocument
		if err := json.Unmarshal([]byte(outcome.body), &prm); err != nil {
			t.Fatalf("decode PRM: %v", err)
		}

		if !slices.Contains(prm.BearerMethodsSupported, "header") {
			t.Errorf("bearer_methods_supported = %v, want it to include header",
				prm.BearerMethodsSupported)
		}

		if prm.AuthorizationServers[0] != oauthIssuer {
			t.Errorf("authorization_servers = %v, want [%q]",
				prm.AuthorizationServers, oauthIssuer)
		}
	})

	t.Run("register", func(t *testing.T) {
		outcome := registerClient(t, proc, discovery, map[string]any{
			"client_name":   "e2e-registration",
			"redirect_uris": []string{oauthRedirectURI},
		})

		if outcome.status != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body: %s", outcome.status, outcome.body)
		}

		var registration registrationResponse
		if err := json.Unmarshal([]byte(outcome.body), &registration); err != nil {
			t.Fatalf("decode registration: %v\nbody: %s", err, outcome.body)
		}

		if !strings.HasPrefix(registration.ClientID, "cetacean-") {
			t.Errorf("client_id = %q, want the cetacean- prefix HandleRegister mints",
				registration.ClientID)
		}

		if registration.TokenEndpointAuthMethod != "none" {
			t.Errorf("token_endpoint_auth_method = %q, want none (public clients only)",
				registration.TokenEndpointAuthMethod)
		}

		// SEP-837: application_type is required of clients, and the server
		// defaults an omitted one to "native" rather than OIDC's "web",
		// which would reject the loopback redirect MCP clients use.
		if registration.ApplicationType != "native" {
			t.Errorf("application_type = %q, want native by default",
				registration.ApplicationType)
		}

		assertSetEqual(t, "grant_types", registration.GrantTypes,
			[]string{"authorization_code", "refresh_token"})
		assertSetEqual(t, "response_types", registration.ResponseTypes, []string{"code"})
	})

	t.Run("register_refuses_confidential_clients", func(t *testing.T) {
		outcome := registerClient(t, proc, discovery, map[string]any{
			"client_name":                "e2e-confidential",
			"redirect_uris":              []string{oauthRedirectURI},
			"token_endpoint_auth_method": "client_secret_basic",
		})

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_client_metadata")
	})

	t.Run("register_refuses_plain_http_redirect", func(t *testing.T) {
		// Only https and loopback http are acceptable; a plaintext redirect
		// to another host would hand the code to the network.
		outcome := registerClient(t, proc, discovery, map[string]any{
			"client_name":   "e2e-plain-http",
			"redirect_uris": []string{"http://example.com/callback"},
		})

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_client_metadata")
	})

	t.Run("register_refuses_web_client_on_loopback", func(t *testing.T) {
		outcome := registerClient(t, proc, discovery, map[string]any{
			"client_name":      "e2e-web-loopback",
			"redirect_uris":    []string{oauthRedirectURI},
			"application_type": "web",
		})

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_redirect_uri")
	})

	t.Run("register_refuses_missing_redirect_uris", func(t *testing.T) {
		outcome := registerClient(t, proc, discovery, map[string]any{
			"client_name": "e2e-no-redirect",
		})

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_client_metadata")
	})

	t.Run("authorize_requires_an_authenticated_user", func(t *testing.T) {
		// /oauth/authorize is deliberately not in auth.isExempt's list: the
		// consent decision is a human's, so the middleware must challenge
		// before the handler ever runs.
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-unauthenticated")
		_, challenge := pkcePair(t)

		page := requestConsent(
			t, proc, discovery, readPersona{},
			newAuthorizeRequest(discovery, clientID, challenge, "state-unauth"),
		)

		if page.outcome.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body: %s", page.outcome.status, page.outcome.body)
		}

		if !strings.Contains(page.outcome.body, "AUT001") {
			t.Errorf("401 body does not name AUT001: %s", page.outcome.body)
		}
	})

	t.Run("authorize_refuses_an_unregistered_redirect_uri", func(t *testing.T) {
		// The sink for an open redirect. The refusal must render an error
		// page, never a redirect to the URI it is refusing.
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-open-redirect")
		_, challenge := pkcePair(t)

		request := newAuthorizeRequest(discovery, clientID, challenge, "state-open")
		request.redirectURI = "http://127.0.0.1:19998/elsewhere"

		page := requestConsent(t, proc, discovery, oauthGranted, request)

		if page.outcome.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", page.outcome.status, page.outcome.body)
		}

		if location := page.outcome.header.Get("Location"); location != "" {
			t.Errorf("refusal redirected to %q; an unregistered redirect_uri must never "+
				"be used as a redirect target", location)
		}
	})

	t.Run("authorize_requires_S256", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-plain-pkce")
		_, challenge := pkcePair(t)

		request := newAuthorizeRequest(discovery, clientID, challenge, "state-plain")
		request.method = "plain"

		page := requestConsent(t, proc, discovery, oauthGranted, request)
		assertRedirectError(t, page.outcome, "invalid_request")
	})

	t.Run("authorize_requires_the_resource_indicator", func(t *testing.T) {
		// RFC 8707 is required by default (mcp.require_resource_indicator).
		// Without it a token would have no stated audience, which is the
		// whole point of the parameter.
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-no-resource")
		_, challenge := pkcePair(t)

		request := newAuthorizeRequest(discovery, clientID, challenge, "state-no-resource")
		request.resource = ""

		page := requestConsent(t, proc, discovery, oauthGranted, request)
		assertRedirectError(t, page.outcome, "invalid_target")
	})

	t.Run("authorize_refuses_a_foreign_resource", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-foreign-resource")
		_, challenge := pkcePair(t)

		request := newAuthorizeRequest(discovery, clientID, challenge, "state-foreign")
		request.resource = "https://elsewhere.example.com/mcp"

		page := requestConsent(t, proc, discovery, oauthGranted, request)
		assertRedirectError(t, page.outcome, "invalid_target")
	})

	t.Run("consent_page_names_the_client_and_the_user", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-consent-copy")
		_, challenge := pkcePair(t)

		page := requireConsentPage(t, requestConsent(
			t, proc, discovery, oauthGranted,
			newAuthorizeRequest(discovery, clientID, challenge, "state-copy"),
		))

		// A consent page that does not say who is authorizing what cannot be
		// consented to. The self-registered badge matters too: a DCR client's
		// name is self-reported and the page must not imply otherwise.
		for _, want := range []string{
			"e2e-consent-copy", oauthGranted.user, oauthRedirectURI, "Self-registered",
		} {
			if !strings.Contains(page.outcome.body, want) {
				t.Errorf("consent page does not mention %q", want)
			}
		}

		if got := page.outcome.header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store; the page carries the CSRF "+
				"token, the code challenge and the user's identity", got)
		}

		if got := page.outcome.header.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options = %q, want DENY", got)
		}
	})

	t.Run("consent_requires_the_csrf_cookie", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-csrf-cookie")
		_, challenge := pkcePair(t)

		page := requireConsentPage(t, requestConsent(
			t, proc, discovery, oauthGranted,
			newAuthorizeRequest(discovery, clientID, challenge, "state-csrf"),
		))

		// Same form, same token, no cookie: verifyCSRFToken has no nonce to
		// recompute the HMAC from.
		form := url.Values{}
		maps.Copy(form, page.fields)
		form.Set("decision", "approve")

		outcome := oauthPostForm(t, proc, discovery.authorizeURL, form, oauthGranted)

		if outcome.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", outcome.status, outcome.body)
		}

		if outcome.header.Get("Location") != "" {
			t.Error("a CSRF failure must not issue a redirect")
		}
	})

	t.Run("consent_token_is_bound_to_its_state", func(t *testing.T) {
		// The CSRF MAC covers the nonce, the state and the metadata
		// fingerprint. Swapping the state alone must invalidate it, or a
		// token captured from one authorization request would authorize a
		// different one.
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-csrf-state")
		_, challenge := pkcePair(t)

		page := requireConsentPage(t, requestConsent(
			t, proc, discovery, oauthGranted,
			newAuthorizeRequest(discovery, clientID, challenge, "state-original"),
		))

		outcome := decide(t, proc, discovery, oauthGranted, page, "approve",
			map[string]string{"state": "state-substituted"})

		if outcome.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", outcome.status, outcome.body)
		}
	})

	t.Run("consent_token_is_bound_to_the_client_metadata", func(t *testing.T) {
		// The fingerprint is covered by the same MAC, so tampering with it
		// invalidates the token rather than silently recording an approval
		// against metadata the user never saw.
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-csrf-fingerprint")
		_, challenge := pkcePair(t)

		page := requireConsentPage(t, requestConsent(
			t, proc, discovery, oauthGranted,
			newAuthorizeRequest(discovery, clientID, challenge, "state-fingerprint"),
		))

		outcome := decide(t, proc, discovery, oauthGranted, page, "approve",
			map[string]string{"consent_fingerprint": "not-the-fingerprint-shown"})

		if outcome.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", outcome.status, outcome.body)
		}
	})

	t.Run("denying_consent_redirects_with_access_denied", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-deny")
		_, challenge := pkcePair(t)

		page := requireConsentPage(t, requestConsent(
			t, proc, discovery, oauthGranted,
			newAuthorizeRequest(discovery, clientID, challenge, "state-deny"),
		))

		outcome := decide(t, proc, discovery, oauthGranted, page, "deny", nil)
		assertRedirectError(t, outcome, "access_denied")

		query := outcome.location(t).Query()
		if query.Get("code") != "" {
			t.Error("a denied authorization returned a code")
		}

		if got := query.Get("state"); got != "state-deny" {
			t.Errorf("state = %q, want it echoed back on the error redirect", got)
		}
	})

	t.Run("approval_redirects_with_a_code_state_and_issuer", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-approve")
		_, challenge := pkcePair(t)

		_, query := authorizationCode(
			t, proc, discovery, oauthGranted, clientID, challenge, "state-approve",
		)

		if got := query.Get("state"); got != "state-approve" {
			t.Errorf("state = %q, want state-approve", got)
		}

		// RFC 9207: without `iss`, a client configured with several
		// authorization servers can be tricked into redeeming this code at
		// the wrong one.
		if got := query.Get("iss"); got != oauthIssuer {
			t.Errorf("iss = %q, want %q", got, oauthIssuer)
		}
	})

	t.Run("a_self_registered_client_is_never_remembered", func(t *testing.T) {
		// Consent is remembered only for CIMD-verified clients: a DCR
		// client's metadata is self-reported and its client_id does not
		// survive a restart. A second authorization must therefore prompt
		// again rather than silently issue a code.
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-reprompt")
		_, challenge := pkcePair(t)

		requireConsentPage(t, requestConsent(
			t, proc, discovery, oauthGranted,
			newAuthorizeRequest(discovery, clientID, challenge, "state-first"),
		))

		page := requireConsentPage(t, requestConsent(
			t, proc, discovery, oauthGranted,
			newAuthorizeRequest(discovery, clientID, challenge, "state-second"),
		))

		if page.outcome.status == http.StatusFound {
			t.Error("the second authorization skipped the consent page for a " +
				"self-registered client")
		}
	})

	t.Run("token_exchange", func(t *testing.T) {
		tokens, clientID := completeFlow(t, proc, discovery, oauthGranted, "e2e-token")

		if tokens.RefreshToken == "" {
			t.Error("token response carries no refresh_token")
		}

		if tokens.ExpiresIn <= 0 {
			t.Errorf("expires_in = %d, want a positive lifetime", tokens.ExpiresIn)
		}

		claims := decodeAccessToken(t, tokens.AccessToken)

		if claims.Issuer != oauthIssuer {
			t.Errorf("iss = %q, want %q", claims.Issuer, oauthIssuer)
		}

		// The audience is the resource indicator the client asked for. A
		// token minted for one resource must not be usable at another, and
		// the claim is the only thing that says which.
		if claims.Audience != discovery.resource {
			t.Errorf("aud = %q, want %q", claims.Audience, discovery.resource)
		}

		if claims.Subject != oauthGranted.user {
			t.Errorf("sub = %q, want %q", claims.Subject, oauthGranted.user)
		}

		if !slices.Contains(claims.Groups, oauthGranted.groups) {
			t.Errorf("groups = %v, want it to carry %q from the authenticated identity",
				claims.Groups, oauthGranted.groups)
		}

		if claims.ClientID != clientID {
			t.Errorf("client_id = %q, want %q", claims.ClientID, clientID)
		}
	})

	t.Run("token_refuses_a_mismatched_pkce_verifier", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-pkce-mismatch")
		_, challenge := pkcePair(t)

		code, _ := authorizationCode(
			t, proc, discovery, oauthGranted, clientID, challenge, "state-pkce",
		)

		// A well-formed verifier for a different challenge: this is the
		// intercepted-code attack PKCE exists to stop, and it must fail on
		// the challenge comparison rather than on shape validation.
		other, _ := pkcePair(t)

		outcome := redeemCode(
			t, proc, discovery, clientID, oauthRedirectURI, code, other, discovery.resource,
		)

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_grant")
	})

	t.Run("token_refuses_a_malformed_verifier", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-short-verifier")
		_, challenge := pkcePair(t)

		code, _ := authorizationCode(
			t, proc, discovery, oauthGranted, clientID, challenge, "state-short",
		)

		outcome := redeemCode(
			t, proc, discovery, clientID, oauthRedirectURI, code, "x", discovery.resource,
		)

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_grant")
	})

	t.Run("an_authorization_code_is_single_use", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-replay-code")
		verifier, challenge := pkcePair(t)

		code, _ := authorizationCode(
			t, proc, discovery, oauthGranted, clientID, challenge, "state-replay",
		)

		first := redeemCode(
			t, proc, discovery, clientID, oauthRedirectURI, code, verifier, discovery.resource,
		)
		requireTokens(t, first)

		second := redeemCode(
			t, proc, discovery, clientID, oauthRedirectURI, code, verifier, discovery.resource,
		)
		requireOAuthError(t, second, http.StatusBadRequest, "invalid_grant")
	})

	t.Run("token_refuses_a_mismatched_redirect_uri", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-redirect-mismatch")
		verifier, challenge := pkcePair(t)

		code, _ := authorizationCode(
			t, proc, discovery, oauthGranted, clientID, challenge, "state-redirect",
		)

		outcome := redeemCode(
			t, proc, discovery, clientID, "http://127.0.0.1:19998/callback",
			code, verifier, discovery.resource,
		)

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_grant")
	})

	t.Run("token_refuses_a_foreign_resource", func(t *testing.T) {
		clientID := registerLoopbackClient(t, proc, discovery, "e2e-token-resource")
		verifier, challenge := pkcePair(t)

		code, _ := authorizationCode(
			t, proc, discovery, oauthGranted, clientID, challenge, "state-token-resource",
		)

		outcome := redeemCode(
			t, proc, discovery, clientID, oauthRedirectURI, code, verifier,
			"https://elsewhere.example.com/mcp",
		)

		requireOAuthError(t, outcome, http.StatusBadRequest, "invalid_target")
	})

	t.Run("token_refuses_an_unsupported_grant_type", func(t *testing.T) {
		form := url.Values{}
		form.Set("grant_type", "client_credentials")

		outcome := oauthPostForm(t, proc, discovery.tokenURL, form, readPersona{})
		requireOAuthError(t, outcome, http.StatusBadRequest, "unsupported_grant_type")
	})

	t.Run("the_access_token_reaches_mcp", func(t *testing.T) {
		tokens, _ := completeFlow(t, proc, discovery, oauthGranted, "e2e-bearer")

		names := toolNames(t, proc, tokens.AccessToken)
		if len(names) == 0 {
			t.Fatal("tools/list returned nothing for a granted OAuth identity")
		}

		if !slices.Contains(names, "find") {
			t.Errorf("tools/list = %v, want it to include find", names)
		}
	})

	t.Run("a_forged_token_is_rejected", func(t *testing.T) {
		// A syntactically valid compact JWS signed with a key that is not the
		// server's. Only the signature check can tell it apart from a real
		// one, so this is the assertion that HS256 verification is actually
		// wired into the request path.
		forged := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
			base64.RawURLEncoding.EncodeToString([]byte(
				`{"iss":"`+oauthIssuer+`","aud":"`+discovery.resource+
					`","sub":"ops@example.com","groups":["ops"],"exp":9999999999}`,
			)) + ".bm90LWEtc2lnbmF0dXJl"

		_, status := mcpWithToken(t, proc, forged, "tools/list", nil)
		if status != http.StatusUnauthorized {
			t.Errorf("forged token: status = %d, want 401", status)
		}
	})

	t.Run("consent_groups_reach_the_acl", func(t *testing.T) {
		// The identity the ACL evaluates on the bearer path comes from the
		// JWT's own claims, minted from whoever was authenticated at the
		// consent page. Nothing else in the suite watches that hop.
		granted, _ := completeFlow(t, proc, discovery, oauthGranted, "e2e-acl-granted")
		ungranted, _ := completeFlow(t, proc, discovery, oauthUngranted, "e2e-acl-ungranted")

		grantedTools := toolNames(t, proc, granted.AccessToken)
		ungrantedTools := toolNames(t, proc, ungranted.AccessToken)

		// find is ungated — it ACL-filters its own results — so it is offered
		// to everyone and proves the ungranted listing is not simply empty
		// for some unrelated reason.
		if !slices.Contains(ungrantedTools, "find") {
			t.Errorf("ungranted tools = %v, want the ungated find to remain", ungrantedTools)
		}

		// scale_service is gated on a service write grant, which only
		// group:ops holds under oauthPolicy.
		if !slices.Contains(grantedTools, "scale_service") {
			t.Errorf("granted tools = %v, want scale_service", grantedTools)
		}

		if slices.Contains(ungrantedTools, "scale_service") {
			t.Errorf(
				"ungranted tools = %v, but scale_service requires a service write grant "+
					"the anonymous persona does not hold; the groups claim did not reach "+
					"the ACL",
				ungrantedTools,
			)
		}

		for _, name := range ungrantedTools {
			if !slices.Contains(grantedTools, name) {
				t.Errorf(
					"tool %q is offered to the ungranted persona but not to ops, whose "+
						"grants are a superset",
					name,
				)
			}
		}
	})

	t.Run("refresh_rotates", func(t *testing.T) {
		original, _ := completeFlow(t, proc, discovery, oauthGranted, "e2e-rotation")

		rotated := requireTokens(
			t, refreshGrant(t, proc, discovery, original.RefreshToken, discovery.resource),
		)

		if rotated.RefreshToken == original.RefreshToken {
			t.Fatal("the refresh token was not rotated; it was returned unchanged")
		}

		if rotated.AccessToken == original.AccessToken {
			t.Error("refreshing returned the same access token")
		}

		if _, status := mcpWithToken(
			t, proc, rotated.AccessToken, "tools/list", nil,
		); status != http.StatusOK {
			t.Fatalf("the rotated access token was refused: status = %d", status)
		}
	})

	t.Run("theft_detection_burns_the_grant_family", func(t *testing.T) {
		// Replaying a consumed refresh token must burn the whole grant family,
		// or a stolen token keeps working alongside the legitimate one and the
		// user is never re-prompted. Driven with the RFC 8707 `resource`
		// parameter, which is mandatory by default.
		replay := driveRefreshReplay(
			t, proc, discovery, oauthGranted, "e2e-theft", discovery.resource,
		)

		if !replay.familyBurned {
			t.Fatalf(
				"a replayed refresh token left its grant family live; the replay was "+
					"refused with %q, and the legitimate token kept rotating",
				replay.replayDescription,
			)
		}
	})

	t.Run("refusing_a_foreign_resource_does_not_burn_the_grant", func(t *testing.T) {
		// A mismatched resource parameter is almost always a client typo, and
		// the token must survive it — otherwise a misconfigured client
		// silently costs its user a re-authorization.
		tokens, _ := completeFlow(t, proc, discovery, oauthGranted, "e2e-typo")

		typo := refreshGrant(
			t, proc, discovery, tokens.RefreshToken, "https://elsewhere.example.com/mcp",
		)
		requireOAuthError(t, typo, http.StatusBadRequest, "invalid_target")

		requireTokens(
			t, refreshGrant(t, proc, discovery, tokens.RefreshToken, discovery.resource),
		)
	})

	t.Run("revocation_ends_the_grant", func(t *testing.T) {
		tokens, _ := completeFlow(t, proc, discovery, oauthGranted, "e2e-revoke")

		form := url.Values{}
		form.Set("token", tokens.RefreshToken)

		outcome := oauthPostForm(t, proc, discovery.revokeURL, form, readPersona{})
		if outcome.status != http.StatusOK {
			t.Fatalf("revoke: status = %d, want 200; body: %s", outcome.status, outcome.body)
		}

		after := refreshGrant(t, proc, discovery, tokens.RefreshToken, discovery.resource)
		requireOAuthError(t, after, http.StatusBadRequest, "invalid_grant")

		// RFC 7009 §2.2: an unknown token is still a 200, so a caller cannot
		// probe which tokens exist.
		unknown := url.Values{}
		unknown.Set("token", "not-a-token-this-server-ever-issued")

		probe := oauthPostForm(t, proc, discovery.revokeURL, unknown, readPersona{})
		if probe.status != http.StatusOK {
			t.Errorf("revoking an unknown token: status = %d, want 200", probe.status)
		}
	})

	t.Run("a_cimd_client_id_is_fetched_and_ssrf_guarded", func(t *testing.T) {
		// CIMD cannot be driven to a successful fetch from here: the SSRF guard
		// blocks loopback and private addresses. What can be proven end to end
		// is that an https:// client_id takes the CIMD path at all, and that the
		// guard is live in the shipped binary.
		_, challenge := pkcePair(t)

		request := newAuthorizeRequest(
			discovery, "https://127.0.0.1:9/cetacean-e2e-client.json", challenge, "state-cimd",
		)

		mark := len(proc.Logs())
		page := requestConsent(t, proc, discovery, oauthGranted, request)

		if page.outcome.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", page.outcome.status, page.outcome.body)
		}

		// The browser is told nothing about why: the fetcher's error names
		// DNS results and block reasons.
		if strings.Contains(page.outcome.body, "SSRF") ||
			strings.Contains(page.outcome.body, "127.0.0.1:9") {
			t.Errorf("the error page leaks fetch details: %s", page.outcome.body)
		}

		// Waited for rather than read once: the child's log reaches the
		// harness through a pipe a goroutine copies, so a record written
		// before the response was flushed can still arrive after the client
		// has read it.
		logs := awaitLog(t, proc, mark, "MCP CIMD fetch failed")

		if !strings.Contains(logs, "SSRF protection blocked request") {
			t.Error("the CIMD fetch failed for some reason other than the SSRF guard; " +
				"the guard may not be reached in the shipped wiring")
		}
	})
}

// Drives the same replay down the one path that still reaches
// RefreshTokenStore.Rotate: a refresh carrying no `resource` parameter, which
// only a server with mcp.require_resource_indicator off accepts. The store's
// detection is live, so D-5 is an ordering bug in the handler in front of it.
func TestMCPOAuthTheftDetectionWithoutTheResourceIndicator(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startOAuth(t, env, t.TempDir(), map[string]string{
		"CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR": "false",
	})

	discovery := discoverOAuth(t, proc)

	replay := driveRefreshReplay(t, proc, discovery, oauthGranted, "e2e-theft-bare", "")

	if replay.replayDescription != theftRefreshTokenDescription {
		t.Errorf(
			"replay refusal = %q, want %q — the wording Rotate's theft branch produces; "+
				"anything else means the replay was answered before detection ran",
			replay.replayDescription, theftRefreshTokenDescription,
		)
	}

	if !replay.familyBurned {
		t.Error(
			"replaying a consumed refresh token left its grant family live even with no " +
				"resource indicator in play: theft detection does not burn the family " +
				"on any path",
		)
	}
}

// TestMCPOAuthDCRRateLimit drives the per-IP registration limit at a configured
// value. /oauth/register is unauthenticated by necessity, so the rate limit is
// the only thing between it and an unbounded stream of registrations. It runs
// on its own SUT: the window is an hour and nothing resets it.
func TestMCPOAuthDCRRateLimit(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	const limit = 3

	proc := startOAuth(t, env, t.TempDir(), map[string]string{
		"CETACEAN_MCP_DCR_RATE_LIMIT": strconv.Itoa(limit),
	})

	discovery := discoverOAuth(t, proc)

	for attempt := 1; attempt <= limit; attempt++ {
		outcome := registerClient(t, proc, discovery, map[string]any{
			"client_name":   "e2e-rate-" + strconv.Itoa(attempt),
			"redirect_uris": []string{oauthRedirectURI},
		})

		if outcome.status != http.StatusCreated {
			t.Fatalf(
				"registration %d of %d was refused before the limit: status = %d, body: %s",
				attempt, limit, outcome.status, outcome.body,
			)
		}
	}

	outcome := registerClient(t, proc, discovery, map[string]any{
		"client_name":   "e2e-rate-over",
		"redirect_uris": []string{oauthRedirectURI},
	})

	requireOAuthError(t, outcome, http.StatusTooManyRequests, "too_many_requests")

	// A limiter that refuses without saying for how long leaves a client
	// retrying blind.
	if got := outcome.header.Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want 3600 (the limiter's one-hour window)", got)
	}
}

// TestMCPOAuthStateSurvivesARestart drives the second half of the rotation
// property: mcp-tokens.json holds the live tokens, the grant families and their
// rotation history, written as one unit. History that failed to persist would
// disable theft detection silently, with every token still working.
func TestMCPOAuthStateSurvivesARestart(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	dataDir := t.TempDir()

	// Both processes run with mcp.require_resource_indicator off and every
	// refresh below omits it: under D-5 a refresh carrying `resource` is
	// answered before Rotate is consulted, so the persisted rotation history
	// would be unobservable. Revert to the default once D-5 is fixed.
	withoutIndicator := map[string]string{
		"CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR": "false",
	}

	first := startOAuth(t, env, dataDir, withoutIndicator)
	discovery := discoverOAuth(t, first)

	survivor, _ := completeFlow(t, first, discovery, oauthGranted, "e2e-restart-survivor")
	victim, _ := completeFlow(t, first, discovery, oauthGranted, "e2e-restart-victim")

	// Consume the victim's token before the restart, so its replay afterwards
	// is a replay of a token this process never saw issued.
	rotatedVictim := requireTokens(
		t, refreshGrant(t, first, discovery, victim.RefreshToken, ""),
	)

	first.Stop()

	statePath := filepath.Join(dataDir, "mcp-tokens.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("no OAuth state was written to %s: %v", statePath, err)
	}

	second := startOAuth(t, env, dataDir, withoutIndicator)

	t.Run("the_access_token_still_verifies", func(t *testing.T) {
		// Stable signing key, so a token minted by the previous process is
		// still a valid one. Without CETACEAN_MCP_SIGNING_KEY this is what
		// the startup warning is about.
		if _, status := mcpWithToken(
			t, second, survivor.AccessToken, "tools/list", nil,
		); status != http.StatusOK {
			t.Errorf("an access token minted before the restart was refused: status = %d",
				status)
		}
	})

	t.Run("the_refresh_token_still_rotates", func(t *testing.T) {
		rotated := requireTokens(
			t, refreshGrant(t, second, discovery, survivor.RefreshToken, ""),
		)

		if rotated.RefreshToken == survivor.RefreshToken {
			t.Error("the refresh token was not rotated after the restart")
		}

		if _, status := mcpWithToken(
			t, second, rotated.AccessToken, "tools/list", nil,
		); status != http.StatusOK {
			t.Errorf("the post-restart access token was refused: status = %d", status)
		}
	})

	t.Run("theft_detection_survives", func(t *testing.T) {
		replay := refreshGrant(t, second, discovery, victim.RefreshToken, "")
		requireOAuthError(t, replay, http.StatusBadRequest, "invalid_grant")

		// The distinguishing assertion. A server that merely forgot the
		// consumed token would reject the replay above and leave the live one
		// working; only a server that still knows the token was *consumed*
		// burns the family.
		after := refreshGrant(t, second, discovery, rotatedVictim.RefreshToken, "")

		if after.status == http.StatusOK {
			t.Error(
				"replaying a pre-restart consumed refresh token did not burn its grant " +
					"family: the live token still rotates, so the rotation history did " +
					"not survive and theft detection is silently off after a restart",
			)

			return
		}

		requireOAuthError(t, after, http.StatusBadRequest, "invalid_grant")
	})
}

// TestMCPOAuthWithoutDCROrCIMD asserts the two client-identification paths can
// be switched off: an operator disabling them is removing attack surface, and
// the server must both stop advertising them and stop serving them.
func TestMCPOAuthWithoutDCROrCIMD(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startOAuth(t, env, t.TempDir(), map[string]string{
		"CETACEAN_MCP_DCR_ENABLED":  "false",
		"CETACEAN_MCP_CIMD_ENABLED": "false",
	})

	discovery := discoverOAuth(t, proc)

	t.Run("metadata_advertises_neither", func(t *testing.T) {
		if discovery.registerURL != "" {
			t.Errorf("registration_endpoint = %q, want it omitted when DCR is off",
				discovery.registerURL)
		}

		if discovery.cimdSupported {
			t.Error("client_id_metadata_document_supported is still advertised with CIMD off")
		}
	})

	t.Run("the_registration_endpoint_is_gone", func(t *testing.T) {
		// RegisterRoutes attaches /oauth/register only when DCR is enabled, so
		// with it off the request reaches the mux's catch-all, which answers a
		// write to an unregistered path 404 rather than serving the dashboard.
		outcome := oauthPostJSON(t, proc, oauthIssuer+"/oauth/register", map[string]any{
			"client_name":   "e2e-disabled-dcr",
			"redirect_uris": []string{oauthRedirectURI},
		})

		if outcome.status != http.StatusNotFound &&
			outcome.status != http.StatusMethodNotAllowed {
			t.Fatalf(
				"POST /oauth/register with DCR disabled: status = %d, want 404; body: %s",
				outcome.status, outcome.body,
			)
		}
	})

	t.Run("an_https_client_id_is_refused_without_fetching", func(t *testing.T) {
		_, challenge := pkcePair(t)

		request := newAuthorizeRequest(
			discovery, "https://example.com/cetacean-client.json", challenge, "state-no-cimd",
		)

		mark := len(proc.Logs())
		page := requestConsent(t, proc, discovery, oauthGranted, request)

		if page.outcome.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", page.outcome.status, page.outcome.body)
		}

		// An absence is only meaningful once the log this request produced has
		// arrived; this request's own record being present proves any CIMD
		// warning it would have emitted is already in the buffer.
		written := awaitLog(t, proc, mark, page.outcome.header.Get("Request-Id"))

		// Refused *before* the fetch: the point of disabling CIMD is that the
		// server stops making outbound requests to URLs a client chose.
		if strings.Contains(written, "MCP CIMD fetch failed") {
			t.Error("the server attempted a CIMD fetch with CIMD disabled")
		}
	})

	t.Run("an_opaque_client_id_is_refused_too", func(t *testing.T) {
		_, challenge := pkcePair(t)

		request := newAuthorizeRequest(
			discovery, "cetacean-never-registered", challenge, "state-no-dcr",
		)

		page := requestConsent(t, proc, discovery, oauthGranted, request)

		if page.outcome.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", page.outcome.status, page.outcome.body)
		}
	})
}

// ---------------------------------------------------------------------------
// Assertions shared by the cases above
// ---------------------------------------------------------------------------

// assertRedirectError asserts an OAuth error delivered as a redirect back to
// the client, carrying the RFC 9207 issuer alongside the error code.
func assertRedirectError(t *testing.T, outcome httpOutcome, wantError string) {
	t.Helper()

	if outcome.status != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", outcome.status, outcome.body)
	}

	query := outcome.location(t).Query()

	if got := query.Get("error"); got != wantError {
		t.Errorf("error = %q, want %q (description: %q)",
			got, wantError, query.Get("error_description"))
	}

	if got := query.Get("iss"); got != oauthIssuer {
		t.Errorf("iss = %q, want %q; RFC 9207 §2 requires it on error responses too",
			got, oauthIssuer)
	}
}

// assertSetEqual compares two string sets irrespective of order.
func assertSetEqual(t *testing.T, name string, got, want []string) {
	t.Helper()

	gotSorted := slices.Clone(got)
	wantSorted := slices.Clone(want)

	slices.Sort(gotSorted)
	slices.Sort(wantSorted)

	if !slices.Equal(gotSorted, wantSorted) {
		t.Errorf("%s = %v, want %v", name, gotSorted, wantSorted)
	}
}

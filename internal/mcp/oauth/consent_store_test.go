package oauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestConsentFingerprintIgnoresRedirectURIOrder(t *testing.T) {
	// CIMD documents are client-controlled and array order carries no meaning,
	// so re-prompting on a reordering would be noise.
	a := consentFingerprint(&ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"http://localhost:1/cb", "http://localhost:2/cb"},
	})
	b := consentFingerprint(&ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"http://localhost:2/cb", "http://localhost:1/cb"},
	})

	if a != b {
		t.Errorf("reordering redirect_uris changed the fingerprint: %q vs %q", a, b)
	}
}

func TestConsentFingerprintChangesWithTheDecision(t *testing.T) {
	base := &ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"http://localhost:1/cb"},
	}
	baseline := consentFingerprint(base)

	tests := []struct {
		name string
		meta *ClientMetadata
	}{
		{
			name: "renamed",
			meta: &ClientMetadata{
				ClientName:   "Totally Different",
				RedirectURIs: []string{"http://localhost:1/cb"},
			},
		},
		{
			name: "redirect uri added",
			meta: &ClientMetadata{
				ClientName:   "Acme CLI",
				RedirectURIs: []string{"http://localhost:1/cb", "http://evil.example/cb"},
			},
		},
		{
			name: "redirect uri removed",
			meta: &ClientMetadata{
				ClientName:   "Acme CLI",
				RedirectURIs: nil,
			},
		},
		{
			name: "redirect uri edited",
			meta: &ClientMetadata{
				ClientName:   "Acme CLI",
				RedirectURIs: []string{"http://localhost:1/cb2"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := consentFingerprint(tc.meta); got == baseline {
				t.Error("fingerprint did not change, so a stale approval would carry over")
			}
		})
	}
}

func TestConsentFingerprintSeparatesFields(t *testing.T) {
	tests := []struct {
		name string
		a    *ClientMetadata
		b    *ClientMetadata
	}{
		{
			name: "concatenation collision",
			// Without field separation, ("ab", "c") and ("a", "bc") would
			// hash alike and one client could impersonate another.
			a: &ClientMetadata{ClientName: "ab", RedirectURIs: []string{"c"}},
			b: &ClientMetadata{ClientName: "a", RedirectURIs: []string{"bc"}},
		},
		{
			name: "embedded NUL in ClientName",
			// client_name comes from a JSON document the client controls.
			// JSON can encode a literal NUL via \u0000. Without length
			// prefixing, ClientName="A\x00B" with RedirectURIs=nil would
			// collide with ClientName="A" with RedirectURIs=["B"].
			a: &ClientMetadata{ClientName: "A\x00B", RedirectURIs: nil},
			b: &ClientMetadata{ClientName: "A", RedirectURIs: []string{"B"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fpA := consentFingerprint(tc.a)
			fpB := consentFingerprint(tc.b)
			if fpA == fpB {
				t.Errorf("collision: two different clients share fingerprint %q", fpA)
			}
		})
	}
}

const (
	testSubject     = "user@example.com"
	testClientID    = "https://example.com/client.json"
	testResource    = "https://cetacean.example.com/mcp"
	testFingerprint = "fingerprint-a"
)

func TestConsentStoreRemembersAnApproval(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, testFingerprint)

	if !s.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Error("a remembered approval should be allowed")
	}
}

func TestConsentStoreRequiresAnExactMatch(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, testFingerprint)

	tests := []struct {
		name                                     string
		subject, clientID, resource, fingerprint string
	}{
		{
			name:    "another user",
			subject: "someone@example.com", clientID: testClientID,
			resource: testResource, fingerprint: testFingerprint,
		},
		{
			name:    "another client",
			subject: testSubject, clientID: "https://other.example/client.json",
			resource: testResource, fingerprint: testFingerprint,
		},
		{
			// RFC 8707: an approval for one MCP endpoint must not cover another.
			name:    "another resource",
			subject: testSubject, clientID: testClientID,
			resource: "https://other.example.com/mcp", fingerprint: testFingerprint,
		},
		{
			// The client changed its metadata after the approval.
			name:    "changed metadata",
			subject: testSubject, clientID: testClientID,
			resource: testResource, fingerprint: "fingerprint-b",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if s.Allows(tc.subject, tc.clientID, tc.resource, tc.fingerprint) {
				t.Error("a non-matching request should re-prompt")
			}
		})
	}
}

func TestConsentStoreReplacesAStaleRecord(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, "fingerprint-a")
	s.Remember(testSubject, testClientID, testResource, "fingerprint-b")

	// A user must never hold two live approvals for one client, one of which
	// they no longer remember granting.
	if got := len(s.Snapshot()); got != 1 {
		t.Errorf("records = %d, want the stale one replaced", got)
	}
	if s.Allows(testSubject, testClientID, testResource, "fingerprint-a") {
		t.Error("the replaced approval should no longer be allowed")
	}
}

func TestConsentStoreForgets(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, testFingerprint)
	s.Forget(testSubject, testClientID, testResource)

	if s.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Error("a forgotten approval should re-prompt")
	}
}

func TestConsentStoreNotifiesOnlyOnRealChanges(t *testing.T) {
	s := NewConsentStore()

	var writes int
	s.SetOnChange(func() { writes++ })

	s.Remember(testSubject, testClientID, testResource, testFingerprint)
	s.Remember(testSubject, testClientID, testResource, testFingerprint) // identical
	s.Forget(testSubject, "https://unknown.example/client.json", testResource)

	// Re-approving an unchanged client and forgetting an unknown one change
	// nothing, and a whole-file rewrite for each would be work for no reason.
	if writes != 1 {
		t.Errorf("writes = %d, want 1 (only the first Remember changed state)", writes)
	}
}

func TestConsentStoreSnapshotIsDeterministic(t *testing.T) {
	s := NewConsentStore()
	s.Remember("b@example.com", testClientID, testResource, testFingerprint)
	s.Remember("a@example.com", testClientID, testResource, testFingerprint)

	// Map iteration order is random; an unsorted snapshot would rewrite the
	// file with reordered records on every unrelated change.
	first := s.Snapshot()
	for range 5 {
		next := s.Snapshot()
		for i := range first {
			if first[i].Subject != next[i].Subject {
				t.Fatal("snapshot order is not stable")
			}
		}
	}
	if first[0].Subject != "a@example.com" {
		t.Errorf("first record = %q, want the sorted-first subject", first[0].Subject)
	}
}

func TestConsentStoreRestoreReplacesState(t *testing.T) {
	s := NewConsentStore()
	s.Remember("stale@example.com", testClientID, testResource, testFingerprint)

	s.Restore([]ConsentRecord{{
		Subject:     testSubject,
		ClientID:    testClientID,
		Resource:    testResource,
		Fingerprint: testFingerprint,
		GrantedAt:   time.Now(),
	}})

	if s.Allows("stale@example.com", testClientID, testResource, testFingerprint) {
		t.Error("Restore should replace state, not merge into it")
	}
	if !s.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Error("the restored record should be allowed")
	}
}

func TestConsentKeySeparatesFields(t *testing.T) {
	// Naive NUL-joining would let these two triples collide: "a\x00b" as a
	// subject joined with clientID "c" reads identically to subject "a"
	// joined with clientID "b\x00c" once concatenated with the resource.
	tests := []struct {
		name                           string
		subjectA, clientIDA, resourceA string
		subjectB, clientIDB, resourceB string
	}{
		{
			name:     "concatenation collision across fields",
			subjectA: "a", clientIDA: "b", resourceA: "c",
			subjectB: "ab", clientIDB: "", resourceB: "c",
		},
		{
			name: "embedded NUL in subject",
			// A subject can originate from an OIDC token claim, and JSON can
			// encode any byte including NUL. Naive "\x00"-joining would let
			// this subject spell a different (subject, clientID) pair.
			subjectA: "a\x00b", clientIDA: "c", resourceA: "d",
			subjectB: "a", clientIDB: "b\x00c", resourceB: "d",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keyA := consentKey(tc.subjectA, tc.clientIDA, tc.resourceA)
			keyB := consentKey(tc.subjectB, tc.clientIDB, tc.resourceB)
			if keyA == keyB {
				t.Errorf("collision: two different triples share key %q", keyA)
			}
		})
	}
}

func TestConsentSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	cfg := ServerConfig{
		Issuer:      "https://cetacean.test",
		MCPResource: testResource,
		MCP: config.MCPConfig{
			AccessTokenTTL:  time.Hour,
			RefreshTokenTTL: 720 * time.Hour,
		},
		SigningKey:     []byte("test-signing-key-32bytes-padded!!"),
		TokenStorePath: path,
	}

	before := NewServer(cfg)
	before.consent.Remember(testSubject, testClientID, testResource, testFingerprint)

	// A second Server over the same path stands in for the process restarting.
	after := NewServer(cfg)

	if !after.consent.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Fatal("an approval granted before the restart should still be allowed")
	}
}

func TestConsentIsWrittenThrough(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	consent := NewConsentStore()
	tokens := NewRefreshTokenStore()
	file := &stateFile{path: path, tokens: tokens, consent: consent}
	consent.SetOnChange(file.write)

	consent.Remember(testSubject, testClientID, testResource, testFingerprint)

	state, err := readState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if state.Version != oauthStateVersion {
		t.Errorf("version = %d, want %d", state.Version, oauthStateVersion)
	}
	if len(state.Consent) != 1 {
		t.Fatalf("consent records on disk = %d, want 1", len(state.Consent))
	}
	if state.Consent[0].Fingerprint != testFingerprint {
		t.Errorf("fingerprint = %q", state.Consent[0].Fingerprint)
	}
}

// consentServer builds a Server with a memory-only state file and one
// remembered approval for the token it returns.
func consentServer(t *testing.T) (*Server, string) {
	t.Helper()

	s := newTestServer(t)
	s.consent.Remember(testSubject, testClientID, s.cfg.MCPResource, testFingerprint)

	token := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  testSubject,
		ClientID: testClientID,
		Resource: s.cfg.MCPResource,
	}, time.Hour)

	return s, token
}

func TestRevocationClearsConsent(t *testing.T) {
	s, token := consentServer(t)

	form := url.Values{"token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleRevoke(rec, req)

	// A record outliving revocation degrades revoke into "grant one more
	// silent re-authorization", which is worse than not revoking, because it
	// appears to have worked.
	if s.consent.Allows(testSubject, testClientID, s.cfg.MCPResource, testFingerprint) {
		t.Error("revocation should have cleared the approval")
	}
}

func TestTheftClearsConsent(t *testing.T) {
	s, token := consentServer(t)

	rotated := s.refreshTokens.Rotate(token, time.Hour)
	if !rotated.OK {
		t.Fatal("the first rotation should succeed")
	}

	replay := s.refreshTokens.Rotate(token, time.Hour)
	if !replay.Theft {
		t.Fatal("re-presenting a consumed token should be detected as theft")
	}
	if replay.Data.Subject != testSubject {
		t.Fatalf("theft result should name the burned family, got subject %q", replay.Data.Subject)
	}
	s.consent.Forget(replay.Data.Subject, replay.Data.ClientID, replay.Data.Resource)

	if s.consent.Allows(testSubject, testClientID, s.cfg.MCPResource, testFingerprint) {
		t.Error("a burned grant family should not leave a silent re-grant behind")
	}
}

func TestExpiryDoesNotClearConsent(t *testing.T) {
	s := newTestServer(t)
	s.consent.Remember(testSubject, testClientID, s.cfg.MCPResource, testFingerprint)

	expired := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  testSubject,
		ClientID: testClientID,
		Resource: s.cfg.MCPResource,
	}, -time.Second)

	if s.refreshTokens.Rotate(expired, time.Hour).OK {
		t.Fatal("an expired token should not rotate")
	}

	// Consent outliving the refresh token is the entire point of the feature.
	if !s.consent.Allows(testSubject, testClientID, s.cfg.MCPResource, testFingerprint) {
		t.Error("expiry must not clear consent")
	}
}

func TestVersion1FileLoadsWithoutConsent(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	// A file written before consent records existed. It must load, not error:
	// an operator upgrading should keep their refresh tokens.
	v1 := []byte(`{"version":1,"tokens":{},"consumed":{},"grants":{}}`)
	if err := os.WriteFile(path, v1, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	state, err := readState(path)
	if err != nil {
		t.Fatalf("a v1 file should still load: %v", err)
	}
	if len(state.Consent) != 0 {
		t.Errorf("consent records = %d, want none", len(state.Consent))
	}
}

// authorizeGET drives a GET /oauth/authorize for a CIMD client whose metadata
// document is served by a local test server, and returns the response.
func authorizeGET(
	t *testing.T,
	s *Server,
	clientID, redirectURI string,
) *httptest.ResponseRecorder {
	t.Helper()

	target := "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {computeS256Challenge("verifier-verifier-verifier-1234")},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz"},
		"resource":              {s.cfg.MCPResource},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{
		Subject:  testSubject,
		Provider: "none",
	}))

	w := httptest.NewRecorder()
	s.HandleAuthorize(w, req)

	return w
}

// cimdServer stands up a CIMD client whose metadata document is served over
// TLS from loopback, and returns a Server wired to fetch it.
func cimdServer(t *testing.T) (srv *Server, clientID, redirectURI string, meta *ClientMetadata) {
	t.Helper()

	published := ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"https://example.com/cb"},
	}

	var documentURL string
	httpSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveMetadata(documentURL, published)(w, r)
	}))
	t.Cleanup(httpSrv.Close)
	documentURL = httpSrv.URL + "/client.json"

	srv = newTestServer(t)

	// The document is served with a self-signed certificate, so the fetcher
	// needs the test server's client to trust it. newTestServer already sets
	// AllowLoopback.
	srv.cimd.Client = httpSrv.Client()

	// consentFingerprint reads only ClientName and RedirectURIs, so this local
	// copy fingerprints identically to the fetched document.
	return srv, documentURL, published.RedirectURIs[0], &published
}

func TestApprovedClientSkipsTheConsentPage(t *testing.T) {
	s, clientID, redirectURI, meta := cimdServer(t)
	s.consent.Remember(testSubject, clientID, s.cfg.MCPResource, consentFingerprint(meta))

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect with a code)", w.Code, http.StatusFound)
	}

	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if location.Query().Get("code") == "" {
		t.Error("no authorization code in the redirect")
	}
	if strings.Contains(w.Body.String(), "Authorization Request") {
		t.Error("the consent page was rendered despite a matching approval")
	}
}

func TestChangedMetadataRePrompts(t *testing.T) {
	s, clientID, redirectURI, _ := cimdServer(t)

	// An approval granted against different metadata than the client now
	// publishes must not carry over.
	s.consent.Remember(testSubject, clientID, s.cfg.MCPResource, "fingerprint-from-before")

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (the consent page)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Authorization Request") {
		t.Error("expected the consent page to be rendered")
	}
}

func TestDynamicallyRegisteredClientNeverSkipsTheConsentPage(t *testing.T) {
	s := newTestServer(t)

	const clientID = "dcr-generated-client-id"
	const redirectURI = "http://127.0.0.1:1/cb"

	s.clients.register(&ClientRegistration{
		ClientID:     clientID,
		ClientName:   "Self-Registered CLI",
		RedirectURIs: []string{redirectURI},
	})

	// Seed a record that matches on every field. It must still be ignored:
	// a DCR client's metadata is self-reported, so a fingerprint over it
	// proves nothing about who the client is.
	s.consent.Remember(testSubject, clientID, s.cfg.MCPResource, consentFingerprint(&ClientMetadata{
		ClientName:   "Self-Registered CLI",
		RedirectURIs: []string{redirectURI},
	}))

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (the consent page)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Authorization Request") {
		t.Error("an unverified client skipped the consent page")
	}
}

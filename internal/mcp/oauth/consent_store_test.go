package oauth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// testConsentTTL is long enough that no test crosses it by accident; the tests
// that care about lapsing set their own.
const testConsentTTL = 2160 * time.Hour

// keyFor builds the approval identity the store is keyed on. Tests name the
// triple positionally; production code passes a ConsentKey it built by field.
func keyFor(subject, clientID, resource string) ConsentKey {
	return ConsentKey{Subject: subject, ClientID: clientID, Resource: resource}
}

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
	s := NewConsentStore(testConsentTTL)
	s.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)

	if !s.Allows(keyFor(testSubject, testClientID, testResource), testFingerprint) {
		t.Error("a remembered approval should be allowed")
	}
}

func TestConsentStoreRequiresAnExactMatch(t *testing.T) {
	s := NewConsentStore(testConsentTTL)
	s.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)

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
			if s.Allows(keyFor(tc.subject, tc.clientID, tc.resource), tc.fingerprint) {
				t.Error("a non-matching request should re-prompt")
			}
		})
	}
}

func TestConsentStoreReplacesAStaleRecord(t *testing.T) {
	s := NewConsentStore(testConsentTTL)
	s.Remember(keyFor(testSubject, testClientID, testResource), "fingerprint-a")
	s.Remember(keyFor(testSubject, testClientID, testResource), "fingerprint-b")

	// A user must never hold two live approvals for one client, one of which
	// they no longer remember granting.
	if got := len(s.Snapshot()); got != 1 {
		t.Errorf("records = %d, want the stale one replaced", got)
	}
	if s.Allows(keyFor(testSubject, testClientID, testResource), "fingerprint-a") {
		t.Error("the replaced approval should no longer be allowed")
	}
}

func TestConsentStoreForgets(t *testing.T) {
	s := NewConsentStore(testConsentTTL)
	s.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)
	s.Forget(keyFor(testSubject, testClientID, testResource))

	if s.Allows(keyFor(testSubject, testClientID, testResource), testFingerprint) {
		t.Error("a forgotten approval should re-prompt")
	}
}

func TestConsentStoreNotifiesOnlyOnRealChanges(t *testing.T) {
	s := NewConsentStore(testConsentTTL)

	var writes int
	s.SetOnChange(func() { writes++ })

	s.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)
	s.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint) // identical
	s.Forget(keyFor(testSubject, "https://unknown.example/client.json", testResource))

	// Re-approving an unchanged client and forgetting an unknown one change
	// nothing, and a whole-file rewrite for each would be work for no reason.
	if writes != 1 {
		t.Errorf("writes = %d, want 1 (only the first Remember changed state)", writes)
	}
}

func TestConsentStoreSnapshotIsDeterministic(t *testing.T) {
	s := NewConsentStore(testConsentTTL)
	s.Remember(keyFor("b@example.com", testClientID, testResource), testFingerprint)
	s.Remember(keyFor("a@example.com", testClientID, testResource), testFingerprint)

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
	s := NewConsentStore(testConsentTTL)
	s.Remember(keyFor("stale@example.com", testClientID, testResource), testFingerprint)

	s.Restore([]ConsentRecord{{
		ConsentKey:  keyFor(testSubject, testClientID, testResource),
		Fingerprint: testFingerprint,
		GrantedAt:   time.Now(),
	}})

	if s.Allows(keyFor("stale@example.com", testClientID, testResource), testFingerprint) {
		t.Error("Restore should replace state, not merge into it")
	}
	if !s.Allows(keyFor(testSubject, testClientID, testResource), testFingerprint) {
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
			keyA := keyFor(tc.subjectA, tc.clientIDA, tc.resourceA).hash()
			keyB := keyFor(tc.subjectB, tc.clientIDB, tc.resourceB).hash()
			if keyA == keyB {
				t.Errorf("collision: two different triples share key %q", keyA)
			}
		})
	}
}

func TestConsentSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	before := newPersistingServer(t, path, testResource)
	before.consent.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)

	// A second Server over the same path stands in for the process restarting.
	after := newPersistingServer(t, path, testResource)

	if !after.consent.Allows(keyFor(testSubject, testClientID, testResource), testFingerprint) {
		t.Fatal("an approval granted before the restart should still be allowed")
	}
}

func TestConsentIsWrittenThrough(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	consent := persisting(NewRefreshTokenStore(), path)

	consent.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)

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
	s.consent.Remember(keyFor(testSubject, testClientID, s.cfg.MCPResource), testFingerprint)

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
	if s.consent.Allows(keyFor(testSubject, testClientID, s.cfg.MCPResource), testFingerprint) {
		t.Error("revocation should have cleared the approval")
	}
}

func TestTheftResultNamesTheBurnedFamily(t *testing.T) {
	s, token := consentServer(t)

	rotated := s.refreshTokens.Rotate(token, time.Hour)
	if !rotated.OK {
		t.Fatal("the first rotation should succeed")
	}

	replay := s.refreshTokens.Rotate(token, time.Hour)
	if !replay.Theft {
		t.Fatal("re-presenting a consumed token should be detected as theft")
	}

	// Naming the burned family is what lets the caller clear its approval; the
	// handler that does so is covered by TestTheftAtTheTokenEndpointClearsConsent.
	want := keyFor(testSubject, testClientID, s.cfg.MCPResource)
	if got := replay.Data.ConsentKey(); got != want {
		t.Errorf("theft result should name the burned family, got %+v", got)
	}
}

func TestExpiryDoesNotClearConsent(t *testing.T) {
	s := newTestServer(t)
	s.consent.Remember(keyFor(testSubject, testClientID, s.cfg.MCPResource), testFingerprint)

	expired := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  testSubject,
		ClientID: testClientID,
		Resource: s.cfg.MCPResource,
	}, -time.Second)

	if s.refreshTokens.Rotate(expired, time.Hour).OK {
		t.Fatal("an expired token should not rotate")
	}

	// Consent outliving the refresh token is the entire point of the feature.
	if !s.consent.Allows(keyFor(testSubject, testClientID, s.cfg.MCPResource), testFingerprint) {
		t.Error("expiry must not clear consent")
	}
}

// grantedAt rewinds a remembered approval's grant time, standing in for one
// recorded that long ago. The store has no clock to inject and Remember always
// stamps time.Now(), so the record is edited in place.
//
// Deliberately not a Snapshot/Restore round-trip: Restore prunes what has
// lapsed, so aging a record that way would leave the store empty and the test
// would pass on the record being *absent* rather than on Allows judging it
// expired. Restore's pruning has its own test.
func grantedAt(t *testing.T, s *ConsentStore, age time.Duration) {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.records) == 0 {
		t.Fatal("nothing to age")
	}

	for hash, record := range s.records {
		record.GrantedAt = time.Now().Add(-age)
		s.records[hash] = record
	}
}

func TestLapsedApprovalIsNotHonoured(t *testing.T) {
	s := NewConsentStore(time.Hour)
	s.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)

	// An approval is only revocable by presenting a token from its grant
	// family. That family is torn down when its refresh token expires, so
	// without a lease of its own the record would outlive the only handle
	// anyone had on it and keep authorizing silently, forever.
	grantedAt(t, s, 2*time.Hour)

	if s.Allows(keyFor(testSubject, testClientID, testResource), testFingerprint) {
		t.Error("an approval past its lease must not skip the consent page")
	}
}

func TestApprovalWithinItsLeaseIsHonoured(t *testing.T) {
	s := NewConsentStore(24 * time.Hour)
	s.Remember(keyFor(testSubject, testClientID, testResource), testFingerprint)

	// The lease has to outlive the refresh token or the feature is pointless:
	// not re-prompting once the token expires is the whole reason to remember.
	grantedAt(t, s, 23*time.Hour)

	if !s.Allows(keyFor(testSubject, testClientID, testResource), testFingerprint) {
		t.Error("an approval inside its lease should still be honoured")
	}
}

func TestRestorePrunesLapsedApprovals(t *testing.T) {
	s := NewConsentStore(time.Hour)

	fresh := keyFor("fresh@example.com", testClientID, testResource)
	stale := keyFor("stale@example.com", testClientID, testResource)

	s.Restore([]ConsentRecord{
		{ConsentKey: fresh, Fingerprint: testFingerprint, GrantedAt: time.Now()},
		{
			ConsentKey:  stale,
			Fingerprint: testFingerprint,
			GrantedAt:   time.Now().Add(-2 * time.Hour),
		},
	})

	// Allows refuses a lapsed record but leaves it in place, so Restore is the
	// only thing that stops the file accumulating records that can never
	// authorize anything again.
	if got := len(s.Snapshot()); got != 1 {
		t.Errorf("records after restore = %d, want only the unexpired one", got)
	}
	if !s.Allows(fresh, testFingerprint) {
		t.Error("the unexpired record should have survived")
	}
	if s.Allows(stale, testFingerprint) {
		t.Error("the lapsed record should have been pruned")
	}
}

func TestReapprovalRenewsTheLease(t *testing.T) {
	s := NewConsentStore(time.Hour)
	key := keyFor(testSubject, testClientID, testResource)

	s.Remember(key, testFingerprint)
	grantedAt(t, s, 2*time.Hour)

	// Re-approving skips the write when nothing changed. A lapsed record has
	// changed in the one way that matters, so the short-circuit must not
	// swallow the renewal.
	s.Remember(key, testFingerprint)

	if !s.Allows(key, testFingerprint) {
		t.Error("re-approving a lapsed client should renew its lease")
	}
}

func TestZeroTTLDisablesRemembering(t *testing.T) {
	s := NewConsentStore(0)
	key := keyFor(testSubject, testClientID, testResource)

	var writes int
	s.SetOnChange(func() { writes++ })

	s.Remember(key, testFingerprint)

	if s.Allows(key, testFingerprint) {
		t.Error("a disabled store must never skip the consent page")
	}
	if got := len(s.Snapshot()); got != 0 {
		t.Errorf("records = %d, want none recorded while disabled", got)
	}
	if writes != 0 {
		t.Errorf("writes = %d, want none: nothing durable changed", writes)
	}
}

func TestDisabledConsentAlwaysPromptsThroughTheServer(t *testing.T) {
	s, clientID, redirectURI, meta := cimdServer(t)
	s.consent = NewConsentStore(0)

	// Even a record that matches on every field must not skip the page once an
	// operator has turned remembering off.
	s.consent.Restore([]ConsentRecord{{
		ConsentKey:  keyFor(testSubject, clientID, s.cfg.MCPResource),
		Fingerprint: consentFingerprint(meta),
		GrantedAt:   time.Now(),
	}})

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (the consent page)", w.Code, http.StatusOK)
	}
}

func TestConsentPageStatesTheLease(t *testing.T) {
	s, clientID, redirectURI, _ := cimdServer(t)
	s.consent = NewConsentStore(48 * time.Hour)

	body := authorizeGET(t, s, clientID, redirectURI).Body.String()

	// The page has to describe the lease it is actually offering, not an
	// open-ended promise it no longer keeps.
	if !strings.Contains(body, "2 days") {
		t.Errorf("consent page should state how long the approval lasts, got:\n%s", body)
	}
}

func TestConsentPageOmitsTheLeaseWhenDisabled(t *testing.T) {
	s, clientID, redirectURI, _ := cimdServer(t)
	s.consent = NewConsentStore(0)

	body := authorizeGET(t, s, clientID, redirectURI).Body.String()

	if strings.Contains(body, "will be remembered") {
		t.Errorf("a disabled store must not promise to remember, got:\n%s", body)
	}
}

func TestVersion1FileLoadsWithoutConsent(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	// A file written before consent records existed, holding a live refresh
	// token. The token is what makes this fixture worth having: v2 moved
	// tokens, consumed and grants into an embedded RefreshTokenSnapshot, and
	// an embedding that nested them under a key instead of flattening would
	// drop every operator's refresh tokens on upgrade. An empty token map
	// cannot see that, because the round-trip helpers are symmetric and would
	// round-trip a nested shape just as happily.
	const raw = "v1-fixture-refresh-token"

	now := time.Now().UTC()
	expiry := now.Add(time.Hour).Format(time.RFC3339Nano)

	v1 := fmt.Sprintf(`{
  "version": 1,
  "timestamp": %q,
  "tokens": {
    %q: {
      "subject": %q,
      "clientId": %q,
      "resource": %q,
      "grantId": "v1-grant",
      "expiresAt": %q,
      "grantExpiresAt": %q
    }
  },
  "consumed": {},
  "grants": {"v1-grant": [%q]}
}`,
		now.Format(time.RFC3339Nano),
		hashToken(raw), testSubject, testClientID, testResource, expiry, expiry,
		hashToken(raw),
	)

	if err := os.WriteFile(path, []byte(v1), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	state, err := readState(path)
	if err != nil {
		t.Fatalf("a v1 file should still load: %v", err)
	}
	if len(state.Consent) != 0 {
		t.Errorf("consent records = %d, want none", len(state.Consent))
	}

	// Drive the load path an upgrading operator's restart actually takes.
	s := newPersistingServer(t, path, testResource)

	data, ok := s.refreshTokens.Validate(raw)
	if !ok {
		t.Fatal("a v1 refresh token must still validate after the upgrade")
	}
	if data.Subject != testSubject {
		t.Errorf("subject = %q, want %q", data.Subject, testSubject)
	}
	if data.ClientID != testClientID {
		t.Errorf("clientId = %q, want %q", data.ClientID, testClientID)
	}
}

// authorizeVerifier is the PKCE verifier every authorize helper below derives
// its code_challenge from, so a GET and the POST that follows it agree.
const authorizeVerifier = "verifier-verifier-verifier-1234"

// authorizeGET drives a GET /oauth/authorize for a CIMD client whose metadata
// document is served by a local test server, and returns the response.
func authorizeGET(
	t *testing.T,
	s *Server,
	clientID, redirectURI string,
) *httptest.ResponseRecorder {
	t.Helper()

	target := authorizeURL(
		clientID,
		redirectURI,
		computeS256Challenge(authorizeVerifier),
		"xyz",
		s.cfg.MCPResource,
	)

	req := withIdentity(httptest.NewRequest(http.MethodGet, target, nil), testSubject, "")

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
	s.consent.Remember(keyFor(testSubject, clientID, s.cfg.MCPResource), consentFingerprint(meta))

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
	s.consent.Remember(keyFor(testSubject, clientID, s.cfg.MCPResource), "fingerprint-from-before")

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
	fingerprint := consentFingerprint(&ClientMetadata{
		ClientName:   "Self-Registered CLI",
		RedirectURIs: []string{redirectURI},
	})
	s.consent.Remember(keyFor(testSubject, clientID, s.cfg.MCPResource), fingerprint)

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (the consent page)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Authorization Request") {
		t.Error("an unverified client skipped the consent page")
	}
}

// approvePOST posts the consent form back the way a browser would: every
// hidden field the page rendered, plus the CSRF nonce cookie it set. Passing
// the recorded GET response rather than rebuilding the form by hand is the
// point — a field the page stopped rendering shows up as a failure here.
func approvePOST(
	t *testing.T,
	s *Server,
	page *httptest.ResponseRecorder,
	overrides url.Values,
) *httptest.ResponseRecorder {
	t.Helper()

	return submitConsent(t, s, page, "approve", testSubject, "", overrides)
}

// redirectedCode returns the authorization code from a recorded redirect.
func redirectedCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}

	return location.Query().Get("code")
}

func TestApprovingThroughTheConsentPageIsRemembered(t *testing.T) {
	s, clientID, redirectURI, meta := cimdServer(t)

	page := authorizeGET(t, s, clientID, redirectURI)
	if page.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want the consent page: %s", page.Code, page.Body.String())
	}

	approved := approvePOST(t, s, page, nil)
	if approved.Code != http.StatusFound {
		t.Fatalf("POST status = %d, want %d: %s",
			approved.Code, http.StatusFound, approved.Body.String())
	}
	if redirectedCode(t, approved) == "" {
		t.Fatal("no authorization code in the approval redirect")
	}

	// The wiring under test: the approve handler must write the record, not
	// merely issue a code. Seeding the store directly would prove nothing
	// about whether the endpoint ever calls Remember.
	approval := keyFor(testSubject, clientID, s.cfg.MCPResource)
	if !s.consent.Allows(approval, consentFingerprint(meta)) {
		t.Fatal("approving through the consent page recorded no approval")
	}

	// And the record it wrote must be the one the skip path reads.
	again := authorizeGET(t, s, clientID, redirectURI)
	if again.Code != http.StatusFound {
		t.Fatalf("second GET status = %d, want a redirect with a code", again.Code)
	}
	if redirectedCode(t, again) == "" {
		t.Error("the second authorization did not carry a code")
	}
}

// postRefreshGrant drives a refresh_token grant through the real token
// endpoint.
//
// No resource parameter: when one is supplied the handler validates it against
// the live token before rotating, so a replayed token is rejected as unknown
// and never reaches the theft branch. RequireResourceIndicator is off in
// newTestServer, so omitting it is a legitimate request.
func postRefreshGrant(t *testing.T, s *Server, token string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token},
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/oauth/token",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	s.HandleToken(w, req)

	return w
}

func TestTheftAtTheTokenEndpointClearsConsent(t *testing.T) {
	s, token := consentServer(t)

	// A legitimate refresh, then a replay of the token it consumed. Both go
	// through HandleToken on purpose: calling Rotate directly proves only that
	// the store detects theft, not that the endpoint reacts to it.
	if first := postRefreshGrant(t, s, token); first.Code != http.StatusOK {
		t.Fatalf("first refresh: status = %d, want 200: %s", first.Code, first.Body.String())
	}

	replay := postRefreshGrant(t, s, token)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replay: status = %d, want 400: %s", replay.Code, replay.Body.String())
	}

	// A replayed token burned the family. Silently re-granting on the next
	// authorize is exactly wrong.
	if s.consent.Allows(keyFor(testSubject, testClientID, s.cfg.MCPResource), testFingerprint) {
		t.Error("a replayed refresh token should have cleared the approval")
	}
}

func TestMetadataChangedMidFlowRePrompts(t *testing.T) {
	// The document is mutated between the GET and the POST, so the handler
	// goroutine and the test goroutine both touch it.
	var (
		mu        sync.Mutex
		published = ClientMetadata{
			ClientName:   "Acme CLI",
			RedirectURIs: []string{"https://example.com/cb"},
		}
		documentURL string
	)

	httpSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		current := published
		mu.Unlock()

		serveMetadata(documentURL, current)(w, r)
	}))
	t.Cleanup(httpSrv.Close)
	documentURL = httpSrv.URL + "/client.json"

	s := newTestServer(t)
	s.cimd.Client = httpSrv.Client()

	const redirectURI = "https://example.com/cb"

	page := authorizeGET(t, s, documentURL, redirectURI)
	if page.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want the consent page: %s", page.Code, page.Body.String())
	}

	// The client edits its document while the user is deciding, and the
	// fetcher's cached copy lapses — a consent tab left open longer than an
	// hour, a restart, an eviction. Without the fingerprint travelling from
	// the GET, the POST would fingerprint this new document and record an
	// approval for a client the user never saw.
	mu.Lock()
	published.ClientName = "Totally Different"
	mu.Unlock()

	s.cimd.mu.Lock()
	s.cimd.cache = nil
	s.cimd.mu.Unlock()

	stale := approvePOST(t, s, page, nil)

	if stale.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want the consent page again: %s",
			stale.Code, stale.Body.String())
	}
	if !strings.Contains(stale.Body.String(), "Totally Different") {
		t.Error("the second prompt should describe the client as it is now")
	}
	if got := len(s.consent.Snapshot()); got != 0 {
		t.Errorf("records = %d, want nothing recorded for metadata the user never saw", got)
	}

	// The re-prompt must be usable: a fresh nonce bound to the new
	// fingerprint, so approving it goes through.
	confirmed := approvePOST(t, s, stale, nil)
	if confirmed.Code != http.StatusFound {
		t.Fatalf("re-approval status = %d, want %d: %s",
			confirmed.Code, http.StatusFound, confirmed.Body.String())
	}

	mu.Lock()
	current := published
	mu.Unlock()

	approval := keyFor(testSubject, documentURL, s.cfg.MCPResource)
	if !s.consent.Allows(approval, consentFingerprint(&current)) {
		t.Error("approving the second prompt should record the metadata it displayed")
	}
}

func TestTamperedFingerprintFailsCSRF(t *testing.T) {
	s, clientID, redirectURI, _ := cimdServer(t)

	page := authorizeGET(t, s, clientID, redirectURI)
	if page.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want the consent page: %s", page.Code, page.Body.String())
	}

	// The fingerprint is not a secret and rides in a hidden field; the CSRF
	// HMAC is what makes it unforgeable. Swapping it must not merely
	// re-prompt — it must fail the token check outright.
	tampered := approvePOST(t, s, page, url.Values{
		consentFingerprintField: {"a-fingerprint-the-user-never-saw"},
	})

	if tampered.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s",
			tampered.Code, http.StatusBadRequest, tampered.Body.String())
	}
	if !strings.Contains(tampered.Body.String(), "CSRF") {
		t.Errorf("expected a CSRF failure, got: %s", tampered.Body.String())
	}
	if got := len(s.consent.Snapshot()); got != 0 {
		t.Errorf("records = %d, want none", got)
	}
}

func TestConsentPageDisclosesRemembering(t *testing.T) {
	const disclosure = "will be remembered"

	verified, clientID, redirectURI, _ := cimdServer(t)

	page := authorizeGET(t, verified, clientID, redirectURI)
	if page.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want the consent page: %s", page.Code, page.Body.String())
	}

	// Turning "approve once" into "approve until revoked" without telling the
	// person is not consent.
	if !strings.Contains(page.Body.String(), disclosure) {
		t.Error("the consent page for a verified client should say the approval is remembered")
	}

	// And it must not claim so where nothing is remembered.
	unverified := newTestServer(t)

	const dcrRedirectURI = "http://127.0.0.1:1/cb"

	unverified.clients.register(&ClientRegistration{
		ClientID:     "dcr-disclosure-client",
		ClientName:   "Self-Registered CLI",
		RedirectURIs: []string{dcrRedirectURI},
	})

	dcrPage := authorizeGET(t, unverified, "dcr-disclosure-client", dcrRedirectURI)
	if dcrPage.Code != http.StatusOK {
		t.Fatalf("DCR GET status = %d, want the consent page", dcrPage.Code)
	}
	if strings.Contains(dcrPage.Body.String(), disclosure) {
		t.Error("a DCR client's approval is never remembered, so the page must not say it is")
	}
}

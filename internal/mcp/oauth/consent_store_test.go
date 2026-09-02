package oauth

import (
	"testing"
	"time"
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

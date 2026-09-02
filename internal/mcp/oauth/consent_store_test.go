package oauth

import (
	"testing"
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

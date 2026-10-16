package oauth

import (
	"testing"
	"time"
)

// Revoking a subject withdraws every client they authorized, and the approvals
// that would let those clients back in without the consent screen; nobody
// else's grants are touched.
func TestRevokeSubjectWithdrawsEveryGrantAndApproval(t *testing.T) {
	s := newTestServer(t)

	issue := func(subject, client string) string {
		return s.refreshTokens.Issue(RefreshTokenData{
			Subject: subject, ClientID: client, Resource: s.resources.fallback,
		}, time.Hour)
	}
	aliceCLI, aliceAgent, bob := issue("alice", "cli"), issue("alice", "agent"), issue("bob", "cli")

	aliceKey := ConsentKey{Subject: "alice", ClientID: "cli", Resource: s.resources.fallback}
	bobKey := ConsentKey{Subject: "bob", ClientID: "cli", Resource: s.resources.fallback}
	s.consent.Remember(aliceKey, "fp")
	s.consent.Remember(bobKey, "fp")

	if got := s.RevokeSubject("alice"); got != 2 {
		t.Errorf("revoked %d grants, want 2", got)
	}

	for name, token := range map[string]string{"alice's cli": aliceCLI, "alice's agent": aliceAgent} {
		if _, ok := s.refreshTokens.Validate(token); ok {
			t.Errorf("%s refresh token still validates", name)
		}
	}
	if s.consent.Allows(aliceKey, "fp") {
		t.Error("alice's approval survived, so her client re-authorizes without a prompt")
	}

	if _, ok := s.refreshTokens.Validate(bob); !ok {
		t.Error("revoking alice revoked bob's grant too")
	}
	if !s.consent.Allows(bobKey, "fp") {
		t.Error("revoking alice forgot bob's approval")
	}

	if got := s.RevokeSubject("alice"); got != 0 {
		t.Errorf("a second revocation found %d grants, want 0", got)
	}
}

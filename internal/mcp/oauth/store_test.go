package oauth

import (
	"testing"
	"time"
)

// --- AuthCodeStore tests ---

func TestAuthCodeStoreRoundTrip(t *testing.T) {
	s := NewAuthCodeStore()
	code := s.Issue(AuthCodeData{
		ClientID:      "https://example.com/client",
		RedirectURI:   "http://localhost:8080/callback",
		CodeChallenge: "abc123",
		Resource:      "https://cetacean.example.com/mcp",
		Subject:       "user@example.com",
		Groups:        []string{"ops"},
	}, 60*time.Second)
	if code == "" {
		t.Fatal("empty code")
	}

	data, ok := s.Redeem(code)
	if !ok {
		t.Fatal("first redeem failed")
	}
	if data.Subject != "user@example.com" {
		t.Errorf("subject = %q", data.Subject)
	}
	if data.Resource != "https://cetacean.example.com/mcp" {
		t.Errorf("resource = %q", data.Resource)
	}
}

func TestAuthCodeSingleUse(t *testing.T) {
	s := NewAuthCodeStore()
	code := s.Issue(AuthCodeData{Subject: "u"}, 60*time.Second)
	if _, ok := s.Redeem(code); !ok {
		t.Fatal("first redeem should succeed")
	}
	if _, ok := s.Redeem(code); ok {
		t.Fatal("second redeem should fail")
	}
}

func TestAuthCodeExpired(t *testing.T) {
	s := NewAuthCodeStore()
	code := s.Issue(AuthCodeData{Subject: "u"}, -time.Second) // already expired
	if _, ok := s.Redeem(code); ok {
		t.Fatal("expired code should not be redeemable")
	}
}

func TestAuthCodeUnknownCode(t *testing.T) {
	s := NewAuthCodeStore()
	if _, ok := s.Redeem("definitely-not-a-real-code"); ok {
		t.Fatal("unknown code should not be redeemable")
	}
}

// --- RefreshTokenStore tests ---

func TestRefreshTokenStoreRoundTrip(t *testing.T) {
	s := NewRefreshTokenStore()
	token := s.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		Groups:   []string{"ops"},
		ClientID: "https://example.com/client",
		Resource: "https://cetacean.example.com/mcp",
	}, 720*time.Hour)

	data, ok := s.Validate(token)
	if !ok {
		t.Fatal("token should be valid")
	}
	if data.Subject != "user@example.com" {
		t.Errorf("subject = %q", data.Subject)
	}
	if data.Resource != "https://cetacean.example.com/mcp" {
		t.Errorf("resource = %q", data.Resource)
	}
}

func TestRefreshTokenRotation(t *testing.T) {
	s := NewRefreshTokenStore()
	old := s.Issue(RefreshTokenData{Subject: "u", ClientID: "c"}, 720*time.Hour)

	res := s.Rotate(old, 720*time.Hour)
	if !res.OK {
		t.Fatal("first rotation should succeed")
	}
	if res.NewToken == "" {
		t.Fatal("new token should be non-empty")
	}
	if res.NewToken == old {
		t.Fatal("new token must differ from old")
	}
	if res.Data.Subject != "u" {
		t.Errorf("data carried wrong subject: %q", res.Data.Subject)
	}

	// Old is now consumed; subsequent validate must fail.
	if _, ok := s.Validate(old); ok {
		t.Fatal("old token must be invalid after rotation")
	}

	// New token is valid.
	if _, ok := s.Validate(res.NewToken); !ok {
		t.Fatal("new token should validate")
	}
}

func TestRefreshTokenTheftDetection(t *testing.T) {
	s := NewRefreshTokenStore()
	original := s.Issue(RefreshTokenData{Subject: "u", ClientID: "c"}, 720*time.Hour)

	// Legitimate rotation succeeds.
	first := s.Rotate(original, 720*time.Hour)
	if !first.OK {
		t.Fatal("legitimate rotation should succeed")
	}

	// Theft: replay the original token. Must report Theft and revoke the family.
	replay := s.Rotate(original, 720*time.Hour)
	if replay.OK {
		t.Fatal("replayed rotation must NOT succeed")
	}
	if !replay.Theft {
		t.Fatal("replay must be flagged as Theft")
	}

	// The newly issued token must also be invalidated.
	if _, ok := s.Validate(first.NewToken); ok {
		t.Fatal("newly-issued token must be revoked after theft detection")
	}
}

func TestRefreshTokenExpired(t *testing.T) {
	s := NewRefreshTokenStore()
	token := s.Issue(RefreshTokenData{Subject: "u"}, -time.Second)

	if _, ok := s.Validate(token); ok {
		t.Fatal("expired token must not validate")
	}
	if res := s.Rotate(token, time.Hour); res.OK {
		t.Fatal("expired token must not rotate")
	}
}

func TestRefreshTokenRevokeGrant(t *testing.T) {
	s := NewRefreshTokenStore()
	t1 := s.Issue(RefreshTokenData{Subject: "u"}, time.Hour)
	r := s.Rotate(t1, time.Hour)
	if !r.OK {
		t.Fatal("setup rotation must succeed")
	}

	s.RevokeGrant(r.NewToken)
	if _, ok := s.Validate(r.NewToken); ok {
		t.Fatal("token must be invalid after RevokeGrant")
	}
	// The original is already consumed; RevokeGrant should also reject any replay of it.
	if res := s.Rotate(t1, time.Hour); res.OK {
		t.Fatal("after RevokeGrant a replay of the original must not produce a new token")
	}
}

func TestRefreshTokenUnknownToken(t *testing.T) {
	s := NewRefreshTokenStore()
	if _, ok := s.Validate("not-a-real-token"); ok {
		t.Fatal("unknown token should not validate")
	}
	if res := s.Rotate("not-a-real-token", time.Hour); res.OK || res.Theft {
		t.Fatalf("unknown token: got %+v, want {OK:false, Theft:false}", res)
	}
}

func TestRefreshTokenGrantIDDiffers(t *testing.T) {
	s := NewRefreshTokenStore()
	a := s.Issue(RefreshTokenData{Subject: "u"}, time.Hour)
	b := s.Issue(RefreshTokenData{Subject: "u"}, time.Hour)
	if a == b {
		t.Fatal("two issued tokens are byte-identical")
	}

	// Revoking a's family must not affect b's family.
	s.RevokeGrant(a)
	if _, ok := s.Validate(b); !ok {
		t.Fatal("revoking unrelated grant must not affect b")
	}
}

package oauth

import (
	"errors"
	"testing"
	"time"
)

const testKey = "test-secret-key-32-bytes-long!!!"

func TestJWTSignAndVerify(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte(testKey),
		Issuer:     "https://cetacean.example.com",
		Audience:   "https://cetacean.example.com/mcp",
	}
	claims := AccessTokenClaims{
		Subject:  "user@example.com",
		Groups:   []string{"ops", "dev"},
		ClientID: "cetacean-client-abc",
	}

	token, err := issuer.IssueAccessToken(claims, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}

	parsed, err := issuer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if parsed.Subject != "user@example.com" {
		t.Errorf("subject = %q", parsed.Subject)
	}
	if len(parsed.Groups) != 2 || parsed.Groups[0] != "ops" || parsed.Groups[1] != "dev" {
		t.Errorf("groups = %v", parsed.Groups)
	}
	if parsed.ClientID != "cetacean-client-abc" {
		t.Errorf("client_id = %q", parsed.ClientID)
	}
}

func TestJWTExpiredToken(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte(testKey),
		Issuer:     "https://cetacean.example.com",
		Audience:   "mcp",
	}
	token, err := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "user@example.com"},
		-time.Hour, // already expired
	)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWTWrongSigningKey(t *testing.T) {
	issuer1 := &TokenIssuer{
		SigningKey: []byte("key-one-32-bytes-long-padding!!!"),
		Issuer:     "https://cetacean.example.com",
		Audience:   "mcp",
	}
	issuer2 := &TokenIssuer{
		SigningKey: []byte("key-two-32-bytes-long-padding!!!"),
		Issuer:     "https://cetacean.example.com",
		Audience:   "mcp",
	}
	token, _ := issuer1.IssueAccessToken(AccessTokenClaims{Subject: "u@e"}, time.Hour)
	if _, err := issuer2.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for wrong signing key")
	}
}

func TestJWTWrongAudience(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte(testKey),
		Issuer:     "https://cetacean.example.com",
		Audience:   "mcp",
	}
	token, _ := issuer.IssueAccessToken(AccessTokenClaims{Subject: "u@e"}, time.Hour)
	other := *issuer
	other.Audience = "wrong"
	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestJWTWrongIssuer(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte(testKey),
		Issuer:     "https://cetacean.example.com",
		Audience:   "mcp",
	}
	token, _ := issuer.IssueAccessToken(AccessTokenClaims{Subject: "u@e"}, time.Hour)
	other := *issuer
	other.Issuer = "https://attacker.example.com"
	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestJWTMalformedToken(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte(testKey),
		Issuer:     "https://cetacean.example.com",
		Audience:   "mcp",
	}
	// Each case is paired with the sentinel error it must surface so callers
	// (the WWW-Authenticate mapping in Task 5) get the right error code.
	cases := []struct {
		token string
		want  error
	}{
		{"", ErrMalformedToken},           // empty
		{"not-a-jwt", ErrMalformedToken},  // single segment
		{"only.two", ErrMalformedToken},   // two segments
		{"a.b.c.d", ErrMalformedToken},    // four segments
		{"!!.!!.!!", ErrInvalidSig},       // three segments but sig won't match
	}
	for _, c := range cases {
		_, err := issuer.VerifyAccessToken(c.token)
		if err == nil {
			t.Errorf("token %q: expected error, got nil", c.token)
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("token %q: got %v, want errors.Is(%v)", c.token, err, c.want)
		}
	}
}

func TestJWTMissingSigningKey(t *testing.T) {
	issuer := &TokenIssuer{
		Issuer:   "https://cetacean.example.com",
		Audience: "mcp",
		// SigningKey deliberately zero
	}
	if _, err := issuer.IssueAccessToken(AccessTokenClaims{Subject: "u@e"}, time.Hour); !errors.Is(err, ErrMissingKey) {
		t.Errorf("IssueAccessToken with empty key: got %v, want ErrMissingKey", err)
	}
	if _, err := issuer.VerifyAccessToken("a.b.c"); !errors.Is(err, ErrMissingKey) {
		t.Errorf("VerifyAccessToken with empty key: got %v, want ErrMissingKey", err)
	}
}

func TestJWTReusedJTIsAreDistinct(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte(testKey),
		Issuer:     "https://cetacean.example.com",
		Audience:   "mcp",
	}
	t1, _ := issuer.IssueAccessToken(AccessTokenClaims{Subject: "u@e"}, time.Hour)
	t2, _ := issuer.IssueAccessToken(AccessTokenClaims{Subject: "u@e"}, time.Hour)
	if t1 == t2 {
		t.Fatal("two issued tokens are byte-identical; jti must randomize")
	}
}

package oauth

import (
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
	cases := []string{
		"",
		"not-a-jwt",
		"only.two",
		"a.b.c.d",  // too many segments
		"!!.!!.!!", // invalid base64
	}
	for _, c := range cases {
		if _, err := issuer.VerifyAccessToken(c); err == nil {
			t.Errorf("expected error for malformed token %q", c)
		}
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

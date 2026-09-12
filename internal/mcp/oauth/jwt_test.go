package oauth

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const testKey = "test-secret-key-32-bytes-long!!!"

// None of these tests is testing the constructor's error.
func mustTokenIssuer(t *testing.T, root []byte, issuer, audience string) *TokenIssuer {
	t.Helper()

	ti, err := NewTokenIssuer(root, issuer, audience)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	return ti
}

func TestJWTSignAndVerify(t *testing.T) {
	issuer := mustTokenIssuer(t,
		[]byte(testKey),
		"https://cetacean.example.com",
		"https://cetacean.example.com/mcp",
	)
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
	issuer := mustTokenIssuer(t, []byte(testKey), "https://cetacean.example.com", "mcp")
	token, err := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "user@example.com", ClientID: "c1"},
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
	issuer1 := mustTokenIssuer(t,
		[]byte("key-one-32-bytes-long-padding!!!"),
		"https://cetacean.example.com",
		"mcp",
	)
	issuer2 := mustTokenIssuer(t,
		[]byte("key-two-32-bytes-long-padding!!!"),
		"https://cetacean.example.com",
		"mcp",
	)
	token, _ := issuer1.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e", ClientID: "c1"},
		time.Hour,
	)
	if _, err := issuer2.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for wrong signing key")
	}
}

func TestJWTWrongAudience(t *testing.T) {
	issuer := mustTokenIssuer(t, []byte(testKey), "https://cetacean.example.com", "mcp")
	token, _ := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e", ClientID: "c1"},
		time.Hour,
	)
	other := *issuer
	other.Audience = "wrong"
	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestJWTWrongIssuer(t *testing.T) {
	issuer := mustTokenIssuer(t, []byte(testKey), "https://cetacean.example.com", "mcp")
	token, _ := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e", ClientID: "c1"},
		time.Hour,
	)
	other := *issuer
	other.Issuer = "https://attacker.example.com"
	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestJWTMalformedToken(t *testing.T) {
	issuer := mustTokenIssuer(t, []byte(testKey), "https://cetacean.example.com", "mcp")
	// Each case names the sentinel it must surface, since callers map those to
	// WWW-Authenticate error codes.
	cases := []struct {
		token string
		want  error
	}{
		{"", ErrMalformedToken},          // empty
		{"not-a-jwt", ErrMalformedToken}, // single segment
		{"only.two", ErrMalformedToken},  // two segments
		{"a.b.c.d", ErrMalformedToken},   // four segments
		{"!!.!!.!!", ErrMalformedToken},  // three segments but header isn't valid base64
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
		// signer deliberately zero
	}
	if _, err := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e"},
		time.Hour,
	); !errors.Is(
		err,
		ErrMissingKey,
	) {
		t.Errorf("IssueAccessToken with empty key: got %v, want ErrMissingKey", err)
	}
	if _, err := issuer.VerifyAccessToken("a.b.c"); !errors.Is(err, ErrMissingKey) {
		t.Errorf("VerifyAccessToken with empty key: got %v, want ErrMissingKey", err)
	}
}

func TestJWTReusedJTIsAreDistinct(t *testing.T) {
	issuer := mustTokenIssuer(t, []byte(testKey), "https://cetacean.example.com", "mcp")
	t1, _ := issuer.IssueAccessToken(AccessTokenClaims{Subject: "u@e", ClientID: "c1"}, time.Hour)
	t2, _ := issuer.IssueAccessToken(AccessTokenClaims{Subject: "u@e", ClientID: "c1"}, time.Hour)
	if t1 == t2 {
		t.Fatal("two issued tokens are byte-identical; jti must randomize")
	}
}

// Payload and key stay intact, so a rejection is attributable to the header.
func reheader(t *testing.T, issuer *TokenIssuer, token, header string) string {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	signingInput := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + parts[1]

	sig, err := signES256(issuer.signer, signingInput)
	if err != nil {
		t.Fatalf("signES256: %v", err)
	}

	return signingInput + "." + sig
}

// RFC 9068 §2.2. Read off the wire as raw JSON: a struct field zeroes out
// silently when its key is absent, which is what an omitempty would cause.
var requiredClaims = []string{"iss", "exp", "aud", "sub", "client_id", "iat", "jti"}

func TestJWTCarriesTheRFC9068Profile(t *testing.T) {
	issuer := mustTokenIssuer(t,
		[]byte(testKey),
		"https://cetacean.example.com",
		"https://cetacean.example.com/mcp",
	)

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "user@example.com",
		ClientID: "cetacean-client-abc",
	}, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	parts := strings.Split(token, ".")

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}

	var hdr jwtHeaderClaims
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}

	// RFC 9068 §2.1 prefers the unprefixed spelling.
	if hdr.Typ != "at+jwt" {
		t.Errorf("typ = %q, want at+jwt", hdr.Typ)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	for _, claim := range requiredClaims {
		raw, ok := claims[claim]
		if !ok {
			t.Errorf("%s: absent, but RFC 9068 §2.2 requires it", claim)

			continue
		}

		if len(raw) == 0 || string(raw) == `""` || string(raw) == "0" {
			t.Errorf("%s: present but empty (%s)", claim, raw)
		}
	}
}

func TestJWTRejectsAnyOtherTokenType(t *testing.T) {
	issuer := mustTokenIssuer(t, []byte(testKey), "https://cetacean.example.com", "mcp")

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "u@e",
		ClientID: "client-1",
	}, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// RFC 9068 §4: a resource server must reject any other value, an absent
	// one included.
	refused := []struct {
		name   string
		header string
	}{
		{"a bare JWT type", `{"alg":"ES256","typ":"JWT"}`},
		{"no type at all", `{"alg":"ES256"}`},
		{"an ID token", `{"alg":"ES256","typ":"id_token+jwt"}`},
		{"an empty type", `{"alg":"ES256","typ":""}`},
	}

	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			_, err := issuer.VerifyAccessToken(reheader(t, issuer, token, c.header))
			if !errors.Is(err, ErrMalformedToken) {
				t.Errorf("got %v, want errors.Is(ErrMalformedToken)", err)
			}
		})
	}

	t.Run("the media type spelled in full is accepted", func(t *testing.T) {
		full := `{"alg":"ES256","typ":"application/at+jwt"}`
		if _, err := issuer.VerifyAccessToken(reheader(t, issuer, token, full)); err != nil {
			t.Errorf("application/at+jwt: %v, want accepted (RFC 9068 §4)", err)
		}
	})
}

func TestJWTRefusesToMintWithoutARequiredClaim(t *testing.T) {
	issuer := mustTokenIssuer(t, []byte(testKey), "https://cetacean.example.com", "mcp")

	// sub and client_id come from the caller, so they are the two that can
	// arrive missing. No resource server may accept a token without them.
	cases := []struct {
		name   string
		claims AccessTokenClaims
	}{
		{"no subject", AccessTokenClaims{ClientID: "client-1"}},
		{"no client id", AccessTokenClaims{Subject: "u@e"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := issuer.IssueAccessToken(c.claims, time.Hour); !errors.Is(
				err,
				ErrIncompleteClaims,
			) {
				t.Errorf("got %v, want errors.Is(ErrIncompleteClaims)", err)
			}
		})
	}
}

func TestTokenIsES256WithAKeyID(t *testing.T) {
	issuer := mustTokenIssuer(t, testRoot, "https://swarm.example", "https://swarm.example/mcp")

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "alice",
		ClientID: "https://client.example/id.json",
	}, time.Hour)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}

	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}

	if hdr.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", hdr.Alg)
	}

	if hdr.Typ != accessTokenType {
		t.Errorf("typ = %q, want %q", hdr.Typ, accessTokenType)
	}

	km := mustDeriveKeys(t, testRoot)

	if hdr.Kid != km.kid {
		t.Errorf("kid = %q, want %q", hdr.Kid, km.kid)
	}
}

func TestVerifyRefusesASignatureThatIsNotSixtyFourBytes(t *testing.T) {
	issuer := mustTokenIssuer(t, testRoot, "https://swarm.example", "https://swarm.example/mcp")

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "alice",
		ClientID: "https://client.example/id.json",
	}, time.Hour)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	parts := strings.Split(token, ".")
	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))

	// Valid ECDSA over the right message, but DER-encoded where RFC 7518 §3.4
	// requires raw R||S.
	der, err := ecdsa.SignASN1(rand.Reader, issuer.signer, digest[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}

	forged := signingInput + "." + base64.RawURLEncoding.EncodeToString(der)

	if _, err := issuer.VerifyAccessToken(forged); !errors.Is(err, ErrInvalidSig) {
		t.Errorf("error = %v, want ErrInvalidSig", err)
	}
}

// An R whose top byte is zero is the only case where unpadded packing differs
// from correct packing, and it occurs in about one signature in 256 — hence the
// loop rather than a single assertion.
func TestPackedSignatureWithALeadingZeroInRVerifies(t *testing.T) {
	const maxAttempts = 4096

	s := newJWKSTestServer(t)

	var token string

	for range maxAttempts {
		candidate, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{
			Subject:  "alice",
			ClientID: "https://client.example/id.json",
		}, time.Hour)
		if err != nil {
			t.Fatalf("IssueAccessToken: %v", err)
		}

		sig, err := base64.RawURLEncoding.DecodeString(strings.Split(candidate, ".")[2])
		if err != nil {
			t.Fatalf("decode signature: %v", err)
		}

		if sig[0] == 0 {
			token = candidate

			break
		}
	}

	if token == "" {
		t.Skip("no signature with a leading zero byte in R turned up within the attempt bound")
	}

	var set jose.JSONWebKeySet
	if err := json.Unmarshal(fetchJWKS(t, s).Body.Bytes(), &set); err != nil {
		t.Fatalf("unmarshal key set: %v", err)
	}

	signature, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("go-jose could not parse the token: %v", err)
	}

	matching := set.Key(signature.Signatures[0].Header.KeyID)
	if len(matching) != 1 {
		t.Fatalf("published set has %d keys for the token's kid, want 1", len(matching))
	}

	if _, err := signature.Verify(matching[0].Key); err != nil {
		t.Fatalf("go-jose rejected a token with a leading zero byte in R: %v", err)
	}
}

func TestVerifyRefusesHS256(t *testing.T) {
	issuer := mustTokenIssuer(t, testRoot, "https://swarm.example", "https://swarm.example/mcp")

	header := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"alg":"HS256","typ":"at+jwt"}`),
	)

	_, err := issuer.VerifyAccessToken(header + ".e30.c2ln")
	if !errors.Is(err, ErrMalformedToken) {
		t.Errorf("error = %v, want ErrMalformedToken", err)
	}
}

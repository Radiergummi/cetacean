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

	"github.com/radiergummi/cetacean/internal/spec"
)

const testKey = "test-secret-key-32-bytes-long!!!"

// The issuer and the resource a signed token is bound to, shared by the tests
// that care about the signature rather than about either value.
const (
	testIssuer        = "https://swarm.example"
	testTokenAudience = testIssuer + "/resource"
)

// None of these tests is testing the constructor's error.
func mustTokenIssuer(t *testing.T, root []byte, issuer string) *TokenIssuer {
	t.Helper()

	ti, err := NewTokenIssuer(root, issuer)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	return ti
}

func TestJWTSignAndVerify(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc9068/tokens-are-signed")

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)
	claims := AccessTokenClaims{
		Subject:  "user@example.com",
		Groups:   []string{"ops", "dev"},
		ClientID: "cetacean-client-abc",
	}

	token, err := issuer.IssueAccessToken(claims, testTokenAudience, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}

	parsed, err := issuer.VerifyAccessToken(token, testTokenAudience)
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
	spec.Satisfies(t,
		"oauth/rfc9068/current-time-before-exp",
		"oauth/rfc7519/exp-is-in-the-future",
	)

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)
	token, err := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "user@example.com", ClientID: "c1"},
		testTokenAudience,
		-time.Hour, // already expired
	)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.VerifyAccessToken(token, testTokenAudience); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWTWrongSigningKey(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc9068/signature-validated-with-declared-alg",
		"oauth/rfc7515/a-signature-must-validate",
		"oauth/rfc7515/no-successful-validation-means-invalid",
	)

	issuer1 := mustTokenIssuer(t, []byte("key-one-32-bytes-long-padding!!!"), testIssuer)
	issuer2 := mustTokenIssuer(t, []byte("key-two-32-bytes-long-padding!!!"), testIssuer)
	token, _ := issuer1.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e", ClientID: "c1"},
		testTokenAudience,
		time.Hour,
	)
	if _, err := issuer2.VerifyAccessToken(token, testTokenAudience); err == nil {
		t.Fatal("expected error for wrong signing key")
	}
}

func TestJWTWrongAudience(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc9068/aud-names-this-resource",
		"oauth/rfc7519/aud-mismatch-is-rejected",
	)

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)
	token, _ := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e", ClientID: "c1"},
		testTokenAudience,
		time.Hour,
	)
	if _, err := issuer.VerifyAccessToken(token, testIssuer+"/other"); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestJWTWrongIssuer(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc9068/iss-exactly-matches")

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)
	token, _ := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e", ClientID: "c1"},
		testTokenAudience,
		time.Hour,
	)
	other := *issuer
	other.Issuer = "https://attacker.example.com"
	if _, err := other.VerifyAccessToken(token, testTokenAudience); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestJWTMalformedToken(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7519/failed-validation-rejects-the-jwt")

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)
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
		_, err := issuer.VerifyAccessToken(c.token, testTokenAudience)
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
		Issuer: testIssuer,
		// signer deliberately zero
	}
	if _, err := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e"},
		testTokenAudience,
		time.Hour,
	); !errors.Is(
		err,
		ErrMissingKey,
	) {
		t.Errorf("IssueAccessToken with empty key: got %v, want ErrMissingKey", err)
	}
	if _, err := issuer.VerifyAccessToken("a.b.c", testTokenAudience); !errors.Is(
		err,
		ErrMissingKey,
	) {
		t.Errorf("VerifyAccessToken with empty key: got %v, want ErrMissingKey", err)
	}
}

func TestJWTReusedJTIsAreDistinct(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7519/jti-is-collision-resistant")

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)
	claims := AccessTokenClaims{Subject: "u@e", ClientID: "c1"}
	t1, _ := issuer.IssueAccessToken(claims, testTokenAudience, time.Hour)
	t2, _ := issuer.IssueAccessToken(claims, testTokenAudience, time.Hour)
	if t1 == t2 {
		t.Fatal("two issued tokens are byte-identical; jti must randomize")
	}
}

// Payload and key stay intact, so a rejection is attributable to the header.
func reheader(t *testing.T, issuer *TokenIssuer, token, header string) string {
	t.Helper()

	return resign(t, issuer, base64.RawURLEncoding.EncodeToString([]byte(header)),
		segment(t, token, 1))
}

// segment returns one base64url part of a compact JWS, still encoded.
func segment(t *testing.T, token string, i int) string {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	return parts[i]
}

// resign joins an encoded header and payload and signs them afresh, which is
// what makes an edited token verifiable rather than merely malformed.
func resign(t *testing.T, issuer *TokenIssuer, header, payload string) string {
	t.Helper()

	signingInput := header + "." + payload

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
	spec.Satisfies(t,
		"oauth/rfc9068/typ-is-at-jwt",
		"oauth/rfc9068/claim-iss-required",
		"oauth/rfc9068/claim-exp-required",
		"oauth/rfc9068/claim-aud-required",
		"oauth/rfc9068/claim-sub-required",
		"oauth/rfc9068/claim-client-id-required",
		"oauth/rfc9068/claim-iat-required",
		"oauth/rfc9068/claim-jti-required",
		"oauth/rfc6750/tokens-are-audience-restricted",
	)

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "user@example.com",
		ClientID: "cetacean-client-abc",
	}, testTokenAudience, time.Hour)
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
	spec.Satisfies(t,
		"oauth/rfc9068/typ-is-at-jwt",
		"oauth/rfc9068/typ-verified-on-receipt",
	)

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "u@e",
		ClientID: "client-1",
	}, testTokenAudience, time.Hour)
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
			_, err := issuer.VerifyAccessToken(reheader(t, issuer, token, c.header),
				testTokenAudience)
			if !errors.Is(err, ErrMalformedToken) {
				t.Errorf("got %v, want errors.Is(ErrMalformedToken)", err)
			}
		})
	}

	t.Run("the media type spelled in full is accepted", func(t *testing.T) {
		full := `{"alg":"ES256","typ":"application/at+jwt"}`
		if _, err := issuer.VerifyAccessToken(
			reheader(t, issuer, token, full), testTokenAudience,
		); err != nil {
			t.Errorf("application/at+jwt: %v, want accepted (RFC 9068 §4)", err)
		}
	})
}

func TestJWTRefusesToMintWithoutARequiredClaim(t *testing.T) {
	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)

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
			if _, err := issuer.IssueAccessToken(
				c.claims, testTokenAudience, time.Hour,
			); !errors.Is(
				err,
				ErrIncompleteClaims,
			) {
				t.Errorf("got %v, want errors.Is(ErrIncompleteClaims)", err)
			}
		})
	}
}

func TestTokenIsES256WithAKeyID(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7515/alg-present-and-processed",
		"oauth/rfc7515/alg-accurately-represents-the-signature",
	)

	issuer := mustTokenIssuer(t, testRoot, testIssuer)

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "alice",
		ClientID: "https://client.example/id.json",
	}, testTokenAudience, time.Hour)
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
	spec.Satisfies(t,
		"oauth/rfc7518/ecdsa-signature-is-64-octets",
		"oauth/rfc7518/ecdsa-signature-keeps-leading-zeros",
	)

	issuer := mustTokenIssuer(t, testRoot, testIssuer)

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "alice",
		ClientID: "https://client.example/id.json",
	}, testTokenAudience, time.Hour)
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

	// A signature shorter than one coordinate is the case the length check is
	// actually load-bearing for: R and S are sliced out at fixed offsets, so
	// without it a truncated signature indexes past the end rather than failing.
	truncated := signingInput + "." + base64.RawURLEncoding.EncodeToString(der[:8])

	if _, err := issuer.VerifyAccessToken(truncated, testTokenAudience); err == nil {
		t.Error("a truncated signature was accepted")
	}

	if _, err := issuer.VerifyAccessToken(forged, testTokenAudience); !errors.Is(
		err,
		ErrInvalidSig,
	) {
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
		}, s.resources.fallback, time.Hour)
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

// "none" is the algorithm RFC 9068 §2.1 forbids outright. RS256 it requires
// among those supported, and this server issues and accepts ES256 alone — so
// this pins the divergence rather than asserting it away.
func TestVerifyRefusesEveryAlgorithmButES256(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc9068/alg-is-not-none",
		"oauth/rfc9068/rs256-among-supported-algorithms",
		"oauth/rfc7519/hs256-and-none-implemented",
		"oauth/rfc7519/unacceptable-algorithms-rejected",
		"oauth/rfc7515/unacceptable-algorithms-are-invalid",
		"oauth/rfc7518/unsecured-jws-not-accepted-by-default",
	)

	issuer := mustTokenIssuer(t, testRoot, testIssuer)

	for _, alg := range []string{"none", "HS256", "RS256"} {
		t.Run(alg, func(t *testing.T) {
			header := base64.RawURLEncoding.EncodeToString(
				[]byte(`{"alg":"` + alg + `","typ":"at+jwt"}`),
			)

			_, err := issuer.VerifyAccessToken(header+".e30.c2ln", testTokenAudience)
			if !errors.Is(err, ErrMalformedToken) {
				t.Errorf("error = %v, want ErrMalformedToken", err)
			}
		})
	}
}

// RFC 7519 §4 lets a parser take the lexically last of two members with the same
// name, which encoding/json does. A token carrying the expected audience first
// and another second must therefore be refused: the alternative — taking the
// first — would let a second member be appended to any token to widen it.
func TestADuplicateAudienceClaimTakesTheLastValue(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7519/duplicate-claim-names-resolved")

	issuer := mustTokenIssuer(t, []byte(testKey), testIssuer)

	token, err := issuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u@e", ClientID: "c1"},
		testTokenAudience,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(segment(t, token, 1))
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	// Appended by hand: encoding/json cannot emit two members of one name.
	const elsewhere = testIssuer + "/elsewhere"

	doubled := strings.Replace(
		string(raw),
		`"aud":"`+testTokenAudience+`"`,
		`"aud":"`+testTokenAudience+`","aud":"`+elsewhere+`"`,
		1,
	)
	if doubled == string(raw) {
		t.Fatalf("payload does not carry aud in the expected shape: %s", raw)
	}

	edited := resign(
		t, issuer,
		segment(t, token, 0),
		base64.RawURLEncoding.EncodeToString([]byte(doubled)),
	)

	if _, err := issuer.VerifyAccessToken(edited, testTokenAudience); err == nil {
		t.Error("a second aud member widened the token's audience")
	}

	// And the last value is what was read, rather than the claim being dropped.
	if _, err := issuer.VerifyAccessToken(edited, elsewhere); err != nil {
		t.Errorf("the last aud member was not the one honoured: %v", err)
	}
}

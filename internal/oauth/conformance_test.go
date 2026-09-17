package oauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/spec"
)

// RFC 8707 §2 lets a client send `resource` more than once, to ask for a token
// valid at several resources. This server binds a token to one, so it must refuse
// rather than honour the first and drop the rest — a token audienced for a
// resource the client did not settle on is the confusion the parameter prevents.
func TestRepeatedResourceIndicatorIsRefused(t *testing.T) {
	s := newTestServer(t)
	s.cfg.OAuth.RequireResourceIndicator = false

	both := []string{s.resources.fallback, s.resources.fallback + "/elsewhere"}

	t.Run("token endpoint", func(t *testing.T) {
		form := url.Values{
			"grant_type":   {"authorization_code"},
			"code":         {"whatever"},
			"redirect_uri": {"http://localhost:9/cb"},
			"client_id":    {"c"},
			"resource":     both,
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/oauth/token",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.HandleToken(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}

		var resp oauthErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// RFC 8707 §2 names invalid_target for a resource the server will not honour.
		if resp.Error != "invalid_target" {
			t.Errorf("error = %q, want invalid_target", resp.Error)
		}
	})

	t.Run("authorize endpoint", func(t *testing.T) {
		const redirectURI = "http://localhost:8615/cb"
		q := url.Values{
			"response_type":         {"code"},
			"client_id":             {registeredClient(t, s, []string{redirectURI})},
			"redirect_uri":          {redirectURI},
			"code_challenge":        {computeS256Challenge("verifier-long-enough-for-RFC-7636-ok")},
			"code_challenge_method": {"S256"},
			"resource":              both,
		}
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
		req = withIdentity(req, fixtureSubject, fixtureEmail)
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, req)

		// Redirect-borne, because redirect_uri was validated before this check.
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want a 302 carrying the error: %s", rec.Code, rec.Body.String())
		}
		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse Location: %v", err)
		}
		if got := loc.Query().Get("error"); got != "invalid_target" {
			t.Errorf("error = %q, want invalid_target", got)
		}
		if loc.Query().Get("code") != "" {
			t.Error("a code was issued for an ambiguous resource request")
		}
	})
}

// The token endpoint carries its parameters in the body (RFC 6749 §3.2), and
// r.Form merges the query in. A client that also echoed `resource` on the URL was
// read as the repeat above and refused a grant it had asked for unambiguously.
func TestAResourceEchoedInTheQueryIsNotARepeat(t *testing.T) {
	s := newTestServer(t)
	s.cfg.OAuth.RequireResourceIndicator = false

	resource := s.resources.fallback
	target := "/oauth/token?resource=" + url.QueryEscape(resource)

	for name, form := range map[string]url.Values{
		"authorization_code": {
			"grant_type":   {"authorization_code"},
			"code":         {"no-such-code"},
			"redirect_uri": {"http://localhost:9/cb"},
			"client_id":    {"c"},
			"resource":     {resource},
		},
		"refresh_token": {
			"grant_type":    {"refresh_token"},
			"refresh_token": {"no-such-token"},
			"client_id":     {"c"},
			"resource":      {resource},
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				target,
				strings.NewReader(form.Encode()),
			)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			s.HandleToken(rec, req)

			var resp oauthErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			// The grant itself is bogus, so the request fails either way. What
			// must not happen is failing on the target.
			if resp.Error == "invalid_target" {
				t.Fatalf("refused as a repeat: %s", resp.ErrorDescription)
			}
			if resp.Error != "invalid_grant" {
				t.Errorf("error = %q, want invalid_grant", resp.Error)
			}
		})
	}
}

// RFC 6750 §3.1: on a request that carried no credential at all, the challenge
// carries no error code — there is nothing wrong with a token never sent. An
// invalid one does carry it.
func TestChallengeOmitsTheErrorCodeWithoutACredential(t *testing.T) {
	s := newTestServer(t)

	bare := s.UnauthorizedHeader(s.resources.fallback, "")
	if strings.Contains(bare, "error=") {
		t.Errorf("challenge for a missing credential names an error: %q", bare)
	}
	if !strings.Contains(bare, `resource_metadata=`) {
		t.Errorf("challenge lost resource_metadata: %q", bare)
	}

	refused := s.UnauthorizedHeader(s.resources.fallback, "invalid_token")
	if !strings.Contains(refused, `error="invalid_token"`) {
		t.Errorf("challenge for an invalid token lost the error: %q", refused)
	}
}

// RFC 7519 §4.1.3 lets aud be an array or, for a single audience, a bare string.
// A validator that took only one shape would refuse a conformant token — and
// ours would then misclassify it as another issuer's and hand it to the provider.
func TestAudienceIsAcceptedInBothShapes(t *testing.T) {
	s := newTestServer(t)
	aud := s.resources.fallback

	// Re-sign a payload whose aud is an array, which is what most JWT libraries
	// emit. The header and key are ours, so only the claim shape differs.
	token, err := s.tokenIssuer.IssueAccessToken(
		AccessTokenClaims{Subject: "alice", ClientID: "c1"},
		aud,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	arrayed := repayload(t, s.tokenIssuer, token, func(m map[string]any) {
		m["aud"] = []any{aud}
	})

	if _, err := s.tokenIssuer.VerifyAccessToken(arrayed, aud); err != nil {
		t.Errorf("a single-element aud array was refused: %v", err)
	}

	// Membership, not identity: a token naming several audiences reaches any of
	// them, while an identifier is still never treated as containing another.
	several := repayload(t, s.tokenIssuer, token, func(m map[string]any) {
		m["aud"] = []any{"https://elsewhere.example", aud}
	})
	if _, err := s.tokenIssuer.VerifyAccessToken(several, aud); err != nil {
		t.Errorf("aud listing this resource among others was refused: %v", err)
	}

	absent := repayload(t, s.tokenIssuer, token, func(m map[string]any) {
		m["aud"] = []any{"https://elsewhere.example"}
	})
	if _, err := s.tokenIssuer.VerifyAccessToken(absent, aud); !errors.Is(
		err, ErrAudienceMismatch,
	) {
		t.Errorf("aud without this resource: %v, want ErrAudienceMismatch", err)
	}
}

// RFC 2045 makes a media type case-insensitive and RFC 7515 §4.1.9 carries that
// into typ. Refusing a conformant token over letter case would route it to the
// upstream provider as though it were another issuer's.
func TestTokenTypeIsCaseInsensitive(t *testing.T) {
	s := newTestServer(t)
	aud := s.resources.fallback

	token, err := s.tokenIssuer.IssueAccessToken(
		AccessTokenClaims{Subject: "alice", ClientID: "c1"},
		aud,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	for _, typ := range []string{"AT+JWT", "at+JWT", "application/AT+jwt"} {
		t.Run(typ, func(t *testing.T) {
			header := `{"alg":"ES256","typ":"` + typ + `"}`
			if _, err := s.tokenIssuer.VerifyAccessToken(
				reheader(t, s.tokenIssuer, token, header), aud,
			); err != nil {
				t.Errorf("typ %q refused: %v", typ, err)
			}
		})
	}
}

// RFC 7519 §4.1.5: a token must not be accepted before its nbf. Never minted
// here, so the check only ever sees one another issuer set.
func TestNotBeforeIsHonoured(t *testing.T) {
	s := newTestServer(t)
	aud := s.resources.fallback

	token, err := s.tokenIssuer.IssueAccessToken(
		AccessTokenClaims{Subject: "alice", ClientID: "c1"},
		aud,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	future := repayload(t, s.tokenIssuer, token, func(m map[string]any) {
		m["nbf"] = time.Now().Add(time.Hour).Unix()
	})
	if _, err := s.tokenIssuer.VerifyAccessToken(future, aud); err == nil {
		t.Error("a token not yet valid was accepted")
	}

	past := repayload(t, s.tokenIssuer, token, func(m map[string]any) {
		m["nbf"] = time.Now().Add(-time.Hour).Unix()
	})
	if _, err := s.tokenIssuer.VerifyAccessToken(past, aud); err != nil {
		t.Errorf("a token past its nbf was refused: %v", err)
	}
}

// RFC 3986 §6.2.3 makes an empty path equivalent to "/" for http and https, so a
// client that normalizes the identifier, or takes it from the API catalog's
// anchor, sends the slashed form of the deployment root.
func TestTheRootIdentifierIsAcceptedWithATrailingSlash(t *testing.T) {
	s := NewServer(ServerConfig{
		Issuer:     "https://cetacean.test",
		Resources:  []Resource{{Path: "", Realm: "cetacean"}},
		OAuth:      config.OAuthConfig{AccessTokenTTL: time.Hour},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	})

	// The canonical identifier is what the resource server verifies aud against,
	// so a grant founded on either spelling has to bind to that one: a token
	// stamped with the slashed form is refused by the resource it was minted for.
	canonical := s.ResourceIdentifier("")

	for _, spelling := range []string{"https://cetacean.test", "https://cetacean.test/"} {
		got, err := s.resources.effectiveResource([]string{spelling}, true)
		if err != nil {
			t.Errorf("%s was refused: %v", spelling, err)

			continue
		}
		if got != canonical {
			t.Errorf("%s bound to %q, want %q", spelling, got, canonical)
		}

		token, err := s.tokenIssuer.IssueAccessToken(
			AccessTokenClaims{Subject: "someone", ClientID: "client"}, got, time.Hour,
		)
		if err != nil {
			t.Fatalf("issue access token: %v", err)
		}
		if _, err := s.Identify(token, canonical); err != nil {
			t.Errorf("a token requested as %s is refused by the resource: %v", spelling, err)
		}
	}
}

// repayload rewrites a token's claims and re-signs it, so a test can present a
// claim shape this server never mints while keeping everything else valid.
func repayload(
	t *testing.T,
	issuer *TokenIssuer,
	token string,
	edit func(map[string]any),
) string {
	t.Helper()

	raw, err := base64.RawURLEncoding.DecodeString(segment(t, token, 1))
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	edit(claims)

	encoded, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return resign(t, issuer, segment(t, token, 0), base64.RawURLEncoding.EncodeToString(encoded))
}

// RFC 9207 §2.4: a client validates the iss of an authorization response only
// when the server advertises that it sends one. Every response here carries iss,
// so without the flag the mix-up defence goes unenforced by conformant clients.
func TestMetadataAdvertisesTheIssParameter(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.HandleMetadata(rec, httptest.NewRequest(
		http.MethodGet,
		"/.well-known/oauth-authorization-server",
		nil,
	))

	var doc map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	if doc["authorization_response_iss_parameter_supported"] != true {
		t.Errorf("authorization_response_iss_parameter_supported = %v, want true",
			doc["authorization_response_iss_parameter_supported"])
	}
}

// RFC 7515 Appendix A.3's worked ES256 example. ECDSA signing is randomised,
// so the RFC's signature cannot be reproduced — only verified, which is the
// half that matters: signES256 and verifyES256 are ours, and every other test
// here signs with the code it then verifies with.
func TestES256VerificationMatchesTheRFC7515Vector(t *testing.T) {
	const (
		signingInput = "eyJhbGciOiJFUzI1NiJ9." +
			"eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFt" +
			"cGxlLmNvbS9pc19yb290Ijp0cnVlfQ"
		signature = "DtEhU3ljbEg8L38VWAfUAqOyKAM6-Xx-F4GawxaepmXFCgfTjDxw5djxLa8ISlSA" +
			"pmWQxfKTUJqPP3-Kg6NU1Q"

		// The public half of the appendix's JWK.
		x = "f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU"
		y = "x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"
	)

	// SEC 1 uncompressed point: 0x04 || X || Y, the shape the coordinates of
	// an EC JWK concatenate to.
	point := append([]byte{4}, append(mustDecodeB64(t, x), mustDecodeB64(t, y)...)...)

	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		t.Fatalf("parse the RFC's public key: %v", err)
	}

	if !verifyES256(pub, signingInput, signature) {
		t.Error("the RFC's own signature does not verify")
	}

	// Without this, a verifier that returns true unconditionally passes.
	if verifyES256(pub, signingInput+"x", signature) {
		t.Error("a signature over different input verified")
	}
}

// RFC 7638 §3.1's worked example, which pins the thumbprint rule itself: the
// required members only, in lexicographic order, with no whitespace. thumbprint
// delegates that to go-jose, and this is what says go-jose reads the RFC the
// same way.
func TestJWKThumbprintMatchesTheRFC7638Vector(t *testing.T) {
	const (
		modulus = "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAt" +
			"VT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn6" +
			"4tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FD" +
			"W2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n9" +
			"1CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINH" +
			"aQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw"

		want = "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs"
	)

	key := &rsa.PublicKey{N: new(big.Int).SetBytes(mustDecodeB64(t, modulus)), E: 65537}

	sum, err := (&jose.JSONWebKey{Key: key}).Thumbprint(crypto.SHA256)
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}

	if got := base64.RawURLEncoding.EncodeToString(sum); got != want {
		t.Errorf("thumbprint = %q, want the RFC's %q", got, want)
	}
}

// The vector above is RSA, so it cannot cover our own kid. Building the EC
// member set by RFC 7638 §3.2's rule — crv, kty, x, y, no whitespace — is what
// makes the golden kid a statement about the spec rather than about the last
// run. The uncompressed point carries both coordinates at full width.
func TestKIDIsTheRFC7638ThumbprintOfTheSigningKey(t *testing.T) {
	km := mustDeriveKeys(t, testRoot)

	const coordinateBytes = 32

	point, err := km.signer.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("encode the signing key: %v", err)
	}

	encode := func(b []byte) string {
		return base64.RawURLEncoding.EncodeToString(b)
	}

	members := fmt.Sprintf(
		`{"crv":"P-256","kty":"EC","x":%q,"y":%q}`,
		encode(point[1:1+coordinateBytes]),
		encode(point[1+coordinateBytes:]),
	)

	sum := sha256.Sum256([]byte(members))

	if got := encode(sum[:]); got != km.kid {
		t.Errorf("kid = %q, but RFC 7638 computes %q", km.kid, got)
	}
}

// mustDecodeB64 reads a base64url value copied out of an RFC.
func mustDecodeB64(t *testing.T, s string) []byte {
	t.Helper()

	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}

	return b
}

// RFC 6749 §4.1.3 binds a code to the client it was issued to and to the
// redirect_uri used to obtain it, and RFC 9700 §4.5 names what the first one
// prevents: a stolen code redeemed by an attacker's client. Both checks were
// already there; deleting either left every test in this package passing.
func TestTheAuthorizationCodeIsBoundToItsClientAndRedirectURI(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc6749/code-bound-to-client",
		"oauth/rfc6749/code-bound-to-redirect-uri",
	)

	// At least 43 characters, or validateCodeVerifier refuses on length before
	// either binding is consulted — and answers invalid_grant either way, which
	// is how the same confusion hid a broken PKCE check here once.
	const (
		verifier = "test-verifier-long-enough-for-the-code-binding-cases"
		redirect = "http://localhost/cb"
	)

	exchange := func(t *testing.T, clientID, redirectURI string) oauthErrorResponse {
		t.Helper()

		s := newTestServer(t)

		code := seedAuthCode(s, AuthCodeData{
			ClientID:      "test-client",
			RedirectURI:   redirect,
			CodeChallenge: computeS256Challenge(verifier),
			Resource:      s.resources.fallback,
			Subject:       "user",
		})

		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"client_id":     {clientID},
			"code_verifier": {verifier},
		}

		req := httptest.NewRequest(
			http.MethodPost,
			"/oauth/token",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.HandleToken(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}

		var out oauthErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}

		return out
	}

	// The description is asserted, not just the code: every refusal on this
	// path is invalid_grant, so the code alone cannot say which check ran.
	assert := func(t *testing.T, got oauthErrorResponse, names string) {
		t.Helper()

		if got.Error != "invalid_grant" {
			t.Errorf("error = %q, want invalid_grant", got.Error)
		}

		if !strings.Contains(got.ErrorDescription, names) {
			t.Errorf("description = %q, want it to name %q", got.ErrorDescription, names)
		}
	}

	t.Run("another client", func(t *testing.T) {
		assert(t, exchange(t, "attacker-client", redirect), "client_id mismatch")
	})

	t.Run("another redirect_uri", func(t *testing.T) {
		assert(t, exchange(t, "test-client", "http://localhost/elsewhere"), "redirect_uri mismatch")
	})
}

// RFC 8414 §3.1 and RFC 9728 §3.1 have their documents queried with GET. The
// discovery routes are registered method-qualified, so a write to one is
// refused rather than answered with a metadata document.
func TestTheDiscoveryDocumentsAreServedOnlyForGET(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc8414/queried-with-get",
		"oauth/rfc9728/queried-with-get",
	)

	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")

	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/oauth-protected-resource" + testResourcePath,
	} {
		for _, method := range []string{
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
		} {
			t.Run(method+" "+path, func(t *testing.T) {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))

				if rec.Code != http.StatusMethodNotAllowed {
					t.Errorf("status = %d, want 405", rec.Code)
				}
			})
		}
	}
}

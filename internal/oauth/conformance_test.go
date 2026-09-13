package oauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/config"
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

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
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

	signingInput := parts[0] + "." + base64.RawURLEncoding.EncodeToString(encoded)

	sig, err := signES256(issuer.signer, signingInput)
	if err != nil {
		t.Fatalf("signES256: %v", err)
	}

	return signingInput + "." + sig
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

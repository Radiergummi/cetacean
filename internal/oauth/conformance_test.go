package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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

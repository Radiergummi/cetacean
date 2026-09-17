package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// RFC 8414 §2 recommends scopes_supported, and RFC 9728 allows it per resource.
// This server defines no scopes, so both documents say so explicitly: an absent
// field reads as "unspecified", where an empty array is an answer. Asserted as
// raw JSON because a Go nil slice and an empty one differ on the wire — null
// versus [] — and only the second says what is meant.
func TestBothDocumentsSayThereAreNoScopes(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc8414/scopes-supported-recommended",
		"oauth/rfc8414/document-at-well-known-path",
		"oauth/rfc8414/zero-element-claims-omitted",
		"oauth/rfc9728/scopes-supported-recommended",
		"oauth/rfc9728/zero-value-parameters-omitted",
	)

	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")

	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/oauth-protected-resource" + testResourcePath,
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			if body := rec.Body.String(); !strings.Contains(body, `"scopes_supported":[]`) {
				t.Errorf("document does not state an empty scopes_supported: %s", body)
			}

			// Decoded too, so the assertion above cannot pass on a substring that
			// happens to appear inside some other value.
			var doc map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
				t.Fatalf("decode: %v", err)
			}
			scopes, ok := doc["scopes_supported"].([]any)
			if !ok {
				t.Fatalf("scopes_supported = %#v, want an array", doc["scopes_supported"])
			}
			if len(scopes) != 0 {
				t.Errorf("scopes_supported = %v, want empty", scopes)
			}
		})
	}
}

// A client that sends a scope anyway is not refused — RFC 6749 §4.1.2.1 offers
// invalid_scope for an unknown one, but refusing would break clients that send
// `offline_access` out of habit and get what they wanted regardless. The scope is
// ignored, and omitted from the response because the granted and requested scopes
// both reduce to the empty set, which §5.1 makes optional to report.
func TestARequestedScopeIsIgnoredRatherThanRefused(t *testing.T) {
	s := newTestServer(t)

	const (
		redirectURI = "http://localhost:8617/cb"
		verifier    = "verifier-padded-to-the-RFC-7636-minimum-length"
	)
	clientID := registeredClient(t, s, []string{redirectURI})

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {computeS256Challenge(verifier)},
		"code_challenge_method": {"S256"},
		"resource":              {s.resources.fallback},
		"scope":                 {"offline_access openid something:made-up"},
	}

	page := httptest.NewRecorder()
	s.HandleAuthorize(page, withIdentity(
		httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil),
		fixtureSubject, fixtureEmail,
	))
	if page.Code != http.StatusOK {
		t.Fatalf("a requested scope was refused at authorize: %d: %s",
			page.Code, page.Body.String())
	}

	approved := submitConsent(t, s, page, "approve", fixtureSubject, fixtureEmail, nil)
	code := redirectedCode(t, approved)
	if code == "" {
		t.Fatalf("no code issued: %s", approved.Header().Get("Location"))
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"resource":      {s.resources.fallback},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("token exchange refused a requested scope: %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if _, present := body["scope"]; present {
		t.Errorf("token response names a scope this server does not define: %v", body["scope"])
	}
	// The refresh token is unconditional, which is what makes ignoring
	// offline_access safe rather than merely lenient.
	if body["refresh_token"] == nil {
		t.Error("no refresh token issued")
	}
}

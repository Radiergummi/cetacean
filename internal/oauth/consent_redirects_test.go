package oauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A remembered approval covers every redirect URI the fingerprint covers, so
// the page shows all of them, not only the one this request asked for.
func TestConsentPageShowsEveryRegisteredRedirectURI(t *testing.T) {
	s := newTestServer(t)

	const shown, other = "http://localhost:8711/cb", "http://localhost:8712/hidden-cb"
	target := authorizeURL(
		registeredClient(t, s, []string{shown, other}),
		shown,
		computeS256Challenge("verifier-padded-to-the-RFC-7636-minimum-length"),
		"state",
		s.resources.identifiers[0],
	)

	rec := httptest.NewRecorder()
	s.HandleAuthorize(
		rec,
		withIdentity(httptest.NewRequest(http.MethodGet, target, nil), "alice", ""),
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), other) {
		t.Errorf("the page does not show the registered %s", other)
	}
}

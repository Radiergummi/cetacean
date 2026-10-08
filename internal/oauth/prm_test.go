package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestProtectedResourceMetadataEndpoint(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc9728/resource-required",
		"oauth/rfc9728/document-at-well-known-url",
		"oauth/rfc9728/response-is-200-json",
		"oauth/rfc9728/resource-matches-the-retrieval-url",
		"oauth/rfc9728/resource-name-recommended",
	)

	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource"+
		testResourcePath, nil)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}

	body := rec.Body.Bytes()

	var doc protectedResourceMetadata
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode PRM: %v", err)
	}

	if doc.Resource != s.resources.fallback {
		t.Errorf("resource = %q, want %q", doc.Resource, s.resources.fallback)
	}
	if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != s.cfg.Issuer {
		t.Errorf("authorization_servers = %v, want [%q]", doc.AuthorizationServers, s.cfg.Issuer)
	}
	if len(doc.BearerMethodsSupported) == 0 || doc.BearerMethodsSupported[0] != "header" {
		t.Errorf("bearer_methods_supported = %v, want [header]", doc.BearerMethodsSupported)
	}

	if doc.ResourceName != "Cetacean Resource" {
		t.Errorf("resource_name = %q, want the resource's display name", doc.ResourceName)
	}
}

// Which documents exist is decided by the configured set, so an identifier the
// deployment does not serve is never advertised. The gate that keeps an unserved
// resource out of the set is what this leaves nothing to advertise.
func TestNoDocumentForAResourceOutsideTheSet(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/absent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

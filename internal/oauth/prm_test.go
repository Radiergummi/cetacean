package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtectedResourceMetadataEndpoint(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	s.HandleProtectedResourceMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}

	var doc protectedResourceMetadata
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatalf("decode PRM: %v", err)
	}

	if doc.Resource != s.cfg.MCPResource {
		t.Errorf("resource = %q, want %q", doc.Resource, s.cfg.MCPResource)
	}
	if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != s.cfg.Issuer {
		t.Errorf("authorization_servers = %v, want [%q]", doc.AuthorizationServers, s.cfg.Issuer)
	}
	if len(doc.BearerMethodsSupported) == 0 || doc.BearerMethodsSupported[0] != "header" {
		t.Errorf("bearer_methods_supported = %v, want [header]", doc.BearerMethodsSupported)
	}
}

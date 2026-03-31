package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

func TestHandleProfile_WithPermissions(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:alice"}, Permissions: []string{"read", "write"}},
	}})

	h := newTestHandlers(t, withACL(e))
	req := httptest.NewRequest("GET", "/profile", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}))
	w := httptest.NewRecorder()
	h.HandleProfile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	perms, ok := resp["permissions"]
	if !ok {
		t.Fatal("response should include 'permissions' key when policy is active")
	}
	permMap, ok := perms.(map[string]any)
	if !ok {
		t.Fatalf("permissions should be a map, got %T", perms)
	}
	if _, ok := permMap["service:webapp"]; !ok {
		t.Fatal("permissions should include service:webapp")
	}
}

func TestHandleProfile_NoPolicyNoPermissions(t *testing.T) {
	// Nil ACL evaluator → no permissions key.
	h := newTestHandlers(t)
	req := httptest.NewRequest("GET", "/profile", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}))
	w := httptest.NewRecorder()
	h.HandleProfile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if _, ok := resp["permissions"]; ok {
		t.Fatal("response should NOT include 'permissions' key when no policy is active")
	}
}

func TestHandleProfile_ProviderAndFileGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})
	e.SetSource(&mockACLSource{
		grants: []acl.Grant{
			{Resources: []string{"node:*"}, Permissions: []string{"write"}},
		},
	})

	h := newTestHandlers(t, withACL(e))
	req := httptest.NewRequest("GET", "/profile", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}))
	w := httptest.NewRecorder()
	h.HandleProfile(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	perms, ok := resp["permissions"].(map[string]any)
	if !ok {
		t.Fatal("expected permissions map")
	}
	if _, ok := perms["service:webapp"]; !ok {
		t.Fatal("should include file grant for service:webapp")
	}
	if _, ok := perms["node:*"]; !ok {
		t.Fatal("should include provider grant for node:*")
	}
}

func TestHandleProfile_NotAuthenticated(t *testing.T) {
	h := newTestHandlers(t)
	req := httptest.NewRequest("GET", "/profile", nil)
	// No identity in context.
	w := httptest.NewRecorder()
	h.HandleProfile(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401 for unauthenticated", w.Code)
	}
}

// mockACLSource implements acl.GrantSource for tests.
type mockACLSource struct {
	grants []acl.Grant
}

func (m *mockACLSource) GrantsFor(_ *auth.Identity) []acl.Grant {
	return m.grants
}

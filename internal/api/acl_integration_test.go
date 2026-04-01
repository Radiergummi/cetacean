package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// Fix 1: ACL list filtering via HTTP handler.
func TestHandleListServices_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp-api"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp-web"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc3",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "backend-worker"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:webapp-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/services", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected 2 filtered services, got %d", resp.Total)
	}
}

func TestHandleListServices_NilPolicyReturnsAll(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp-api"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp-web"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc3",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "backend-worker"}},
	})

	// No ACL evaluator -- nil policy means allow all.
	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/services", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 3 {
		t.Fatalf("expected 3 services with nil policy, got %d", resp.Total)
	}
}

// Fix 2: requireAnyGrant returns 403 with ACL001 when identity has no grants.
func TestHandleCluster_ACL001_NoGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		// Only alice has grants; bob has none.
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))

	// Bob has no matching grants.
	req := httptest.NewRequest("GET", "/cluster", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleCluster(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}

	var problem ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Status != http.StatusForbidden {
		t.Errorf("problem status=%d, want 403", problem.Status)
	}
	// The type URL should reference ACL001.
	if problem.Type == "" {
		t.Fatal("problem type should be set")
	}
}

func TestHandleCluster_ACL001_WithGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))

	// Alice has grants -- should pass requireAnyGrant.
	req := httptest.NewRequest("GET", "/cluster", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}))
	w := httptest.NewRecorder()
	h.HandleCluster(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 for identity with grants", w.Code)
	}
}

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
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

// Verify sub-resource endpoints enforce ACL read checks.

func TestHandleServiceTasks_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/services/svc1/tasks", nil)
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleServiceTasks(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied service tasks, got %d", w.Code)
	}
}

func TestHandleServiceLogs_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/services/svc1/logs", nil)
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleServiceLogs(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied service logs, got %d", w.Code)
	}
}

func TestHandleTaskLogs_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", NodeID: "node1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"task:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/tasks/task1/logs", nil)
	req.SetPathValue("id", "task1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleTaskLogs(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied task logs, got %d", w.Code)
	}
}

func TestHandleNodeTasks_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker1"},
	})
	c.SetTask(swarm.Task{ID: "task-allowed", ServiceID: "svc1", NodeID: "node1"})
	c.SetTask(swarm.Task{ID: "task-denied", ServiceID: "svc2", NodeID: "node1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"task:task-allowed"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/nodes/node1/tasks", nil)
	req.SetPathValue("id", "node1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleNodeTasks(w, req)

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
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered task, got %d", resp.Total)
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

// assertACLErrorCode decodes a problem detail response and checks that the
// type field contains the given error code.
func assertACLErrorCode(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if !strings.Contains(p.Type, code) {
		t.Errorf("expected %s in problem type, got %q", code, p.Type)
	}
}

// --- Task 1: List handler ACL filter tests ---

func TestHandleListNodes_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})
	c.SetNode(swarm.Node{
		ID:          "node2",
		Description: swarm.NodeDescription{Hostname: "worker-2"},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"node:worker-1"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/nodes", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered node, got %d", resp.Total)
	}
}

func TestHandleListTasks_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1"})
	c.SetTask(swarm.Task{ID: "task2"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"task:task1"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/tasks", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered task, got %d", resp.Total)
	}
}

func TestHandleListConfigs_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}},
	})
	c.SetConfig(swarm.Config{
		ID:   "cfg2",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "other-config"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"config:app-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/configs", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered config, got %d", resp.Total)
	}
}

func TestHandleListSecrets_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "app-secret"}},
	})
	c.SetSecret(swarm.Secret{
		ID:   "sec2",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "other-secret"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"secret:app-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/secrets", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered secret, got %d", resp.Total)
	}
}

func TestHandleListNetworks_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend"})
	c.SetNetwork(network.Summary{ID: "net2", Name: "backend"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"network:frontend"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/networks", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered network, got %d", resp.Total)
	}
}

func TestHandleListVolumes_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "data-vol"})
	c.SetVolume(volume.Volume{Name: "cache-vol"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"volume:data-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/volumes", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered volume, got %d", resp.Total)
	}
}

func TestHandleListStacks_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "monitoring_prometheus",
				Labels: map[string]string{"com.docker.stack.namespace": "monitoring"},
			},
		},
	})
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "production_api",
				Labels: map[string]string{"com.docker.stack.namespace": "production"},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"stack:monitoring"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/stacks", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered stack, got %d", resp.Total)
	}
}

// --- Task 2: Detail handler ACL denial tests ---

func TestHandleGetNode_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"node:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/nodes/node1", nil)
	req.SetPathValue("id", "node1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied node, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetService_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied service, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetTask_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"task:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied task, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetConfig_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"config:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetConfig(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied config, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetSecret_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "app-secret"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"secret:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied secret, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetNetwork_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"network:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/networks/net1", nil)
	req.SetPathValue("id", "net1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetNetwork(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied network, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetVolume_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "data-vol"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"volume:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/volumes/data-vol", nil)
	req.SetPathValue("name", "data-vol")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetVolume(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied volume, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

// --- Task 3: Cross-reference filtering in detail responses ---

func TestHandleGetConfig_CrossRefFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "shared-config"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "allowed-svc"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{
						{ConfigID: "cfg1", ConfigName: "shared-config"},
					},
				},
			},
		},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "denied-svc"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{
						{ConfigID: "cfg1", ConfigName: "shared-config"},
					},
				},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"config:*", "service:allowed-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Services []json.RawMessage `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Services) != 1 {
		t.Fatalf("expected 1 service in cross-references, got %d", len(resp.Services))
	}
}

func TestHandleGetSecret_CrossRefFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "shared-secret"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "allowed-svc"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{
						{SecretID: "sec1", SecretName: "shared-secret"},
					},
				},
			},
		},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "denied-svc"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{
						{SecretID: "sec1", SecretName: "shared-secret"},
					},
				},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"secret:*", "service:allowed-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Services []json.RawMessage `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Services) != 1 {
		t.Fatalf("expected 1 service in cross-references, got %d", len(resp.Services))
	}
}

// --- Task 4: Search endpoint ACL filtering ---

func TestHandleSearch_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp-api"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "backend-worker"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
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
	req := httptest.NewRequest("GET", "/search?q=w&limit=0", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Counts["services"] != 1 {
		t.Fatalf("expected services count=1, got %d", resp.Counts["services"])
	}
	if resp.Counts["nodes"] != 0 {
		t.Fatalf("expected nodes count=0, got %d", resp.Counts["nodes"])
	}
}

func TestHandleGetStack_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "monitoring_prometheus",
				Labels: map[string]string{"com.docker.stack.namespace": "monitoring"},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"stack:other"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/stacks/monitoring", nil)
	req.SetPathValue("name", "monitoring")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetStack(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied stack, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

// --- Task 5: Write handler ACL integration tests with name resolvers ---

func TestServiceScaleACL_DeniedByResourceName(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "webapp"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: func() *uint64 { v := uint64(1); return &v }()},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
		{
			Resources:   []string{"service:other"},
			Audience:    []string{"*"},
			Permissions: []string{"write"},
		},
	}})

	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(wc))

	handler := h.requireWriteACL(h.serviceName)(
		requireLevel(config.OpsOperational, config.OpsImpactful)(h.HandleScaleService),
	)

	body := `{"replicas": 3}`
	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	assertACLErrorCode(t, w, "ACL002")
}

func TestServiceScaleACL_AllowedByResourceName(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(1)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "webapp"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"write"},
		},
	}})

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			scaleServiceFn: func(_ context.Context, id string, r uint64) (swarm.Service, error) {
				return swarm.Service{ID: id}, nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(wc))

	handler := h.requireWriteACL(h.serviceName)(
		requireLevel(config.OpsOperational, config.OpsImpactful)(h.HandleScaleService),
	)

	body := `{"replicas": 3}`
	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("expected non-403, got 403; body: %s", w.Body.String())
	}
}

func TestTaskRemoveACL_ResolvesToParentService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"write"},
		},
	}})

	wc := &mockWriteClient{
		mockResourceRemover: mockResourceRemover{
			removeTaskFn: func(_ context.Context, id string) error {
				return nil
			},
		},
	}
	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(wc))

	handler := h.requireWriteACL(h.taskServiceResource)(
		requireLevel(config.OpsImpactful, config.OpsImpactful)(h.HandleRemoveTask),
	)

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("expected non-403, got 403; body: %s", w.Body.String())
	}
}

func TestTaskRemoveACL_DeniedWhenParentServiceNotGranted(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:other"},
			Audience:    []string{"*"},
			Permissions: []string{"write"},
		},
	}})

	wc := &mockWriteClient{}
	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(wc))

	handler := h.requireWriteACL(h.taskServiceResource)(
		requireLevel(config.OpsImpactful, config.OpsImpactful)(h.HandleRemoveTask),
	)

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	assertACLErrorCode(t, w, "ACL002")
}

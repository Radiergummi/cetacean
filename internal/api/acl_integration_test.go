package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	json "github.com/goccy/go-json"

	"time"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/jgf"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

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

// requireAnyGrant returns 403 with ACL001 when identity has no grants.
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
	assertACLErrorCode(t, w, "ACL001")
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
			Resources:   []string{"node:worker1"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
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

func TestHandleNodeTasks_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker1"},
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
	req := httptest.NewRequest("GET", "/nodes/node1/tasks", nil)
	req.SetPathValue("id", "node1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleNodeTasks(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
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

// --- List handler ACL filter tests ---

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

// --- Detail handler ACL denial tests ---

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

// --- Cross-reference filtering in detail responses ---

func TestHandleGetConfig_CrossRefFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "shared-config"}},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
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
		ID: "svc2",
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
		ID: "svc1",
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
		ID: "svc2",
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

// --- Search endpoint ACL filtering ---

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

// --- Write handler ACL integration with name resolvers ---

func TestServiceScaleACL_DeniedByResourceName(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "webapp"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{
					Replicas: func() *uint64 { v := uint64(1); return &v }(),
				},
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

	handler := h.requireWriteACL(
		resolveResource(
			"service",
			h.cache.GetService,
			func(s swarm.Service) string { return s.Spec.Name },
		),
	)(
		handlersAt(config.OpsImpactful).requireLevel(config.OpsOperational)(
			http.HandlerFunc(h.HandleScaleService),
		),
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
		ID: "svc1",
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
		scaleServiceFn: func(_ context.Context, id string, r uint64) (swarm.Service, error) {
			return swarm.Service{ID: id}, nil
		},
	}
	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(wc))

	handler := h.requireWriteACL(
		resolveResource(
			"service",
			h.cache.GetService,
			func(s swarm.Service) string { return s.Spec.Name },
		),
	)(
		handlersAt(config.OpsImpactful).requireLevel(config.OpsOperational)(
			http.HandlerFunc(h.HandleScaleService),
		),
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
		removeTaskFn: func(_ context.Context, id string) error {
			return nil
		},
	}
	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(wc))

	handler := h.requireWriteACL(h.taskServiceResource)(
		handlersAt(config.OpsImpactful).requireLevel(config.OpsImpactful)(
			http.HandlerFunc(h.HandleRemoveTask),
		),
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
		handlersAt(config.OpsImpactful).requireLevel(config.OpsImpactful)(
			http.HandlerFunc(h.HandleRemoveTask),
		),
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

func TestHandleStackSummary_ACL001_NoGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))
	req := httptest.NewRequest("GET", "/stacks/summary", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleStackSummary(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

// --- Cluster-wide endpoint ACL gates ---

func aclPolicyForAlice() *acl.Evaluator {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})
	return e
}

func TestHandleClusterCapacity_ACL001_NoGrants(t *testing.T) {
	h := newTestHandlers(t, withACL(aclPolicyForAlice()))
	req := httptest.NewRequest("GET", "/cluster/capacity", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleClusterCapacity(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleDiskUsage_ACL001_NoGrants(t *testing.T) {
	h := newTestHandlers(t, withACL(aclPolicyForAlice()))
	req := httptest.NewRequest("GET", "/disk-usage", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleDiskUsage(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleHistory_ACL001_NoGrants(t *testing.T) {
	h := newTestHandlers(t, withACL(aclPolicyForAlice()))
	req := httptest.NewRequest("GET", "/history", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleSwarm_ACL001_NoGrants(t *testing.T) {
	h := newTestHandlers(t, withACL(aclPolicyForAlice()), withSystemClient(&mockSystemClient{
		swarmInspectFn: func(_ context.Context) (swarm.Swarm, error) {
			return swarm.Swarm{}, nil
		},
	}))
	req := httptest.NewRequest("GET", "/swarm", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleSwarm(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleSwarm_RedactsJoinTokensWithoutSwarmWrite(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		// Read on everything, but no write on swarm:cluster.
		{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withACL(e), withSystemClient(&mockSystemClient{
		swarmInspectFn: func(_ context.Context) (swarm.Swarm, error) {
			return swarm.Swarm{
				JoinTokens: swarm.JoinTokens{
					Worker:  "SWMTKN-1-worker-secret",
					Manager: "SWMTKN-1-manager-secret",
				},
			}, nil
		},
	}))

	req := httptest.NewRequest("GET", "/swarm", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleSwarm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	body := w.Body.String()
	if strings.Contains(body, "SWMTKN") {
		t.Fatal("join tokens should be redacted for users without swarm:cluster write")
	}
}

func TestHandleSwarm_IncludesJoinTokensWithSwarmWrite(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"swarm:cluster"},
			Audience:    []string{"*"},
			Permissions: []string{"write"},
		},
	}})

	h := newTestHandlers(t, withACL(e), withSystemClient(&mockSystemClient{
		swarmInspectFn: func(_ context.Context) (swarm.Swarm, error) {
			return swarm.Swarm{
				JoinTokens: swarm.JoinTokens{
					Worker:  "SWMTKN-1-worker-secret",
					Manager: "SWMTKN-1-manager-secret",
				},
			}, nil
		},
	}))

	req := httptest.NewRequest("GET", "/swarm", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "admin"}))
	w := httptest.NewRecorder()
	h.HandleSwarm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "SWMTKN-1-worker-secret") {
		t.Fatal("join tokens should be present for users with swarm:cluster write")
	}
	if !strings.Contains(body, "SWMTKN-1-manager-secret") {
		t.Fatal("manager join token should be present")
	}
}

func TestServiceSubResourceGET_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "webapp"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{},
			},
		},
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
	identity := &auth.Identity{Subject: "user1"}

	subResources := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"env", h.HandleGetServiceEnv},
		{"labels", h.HandleGetServiceLabels},
		{"resources", h.HandleGetServiceResources},
		{"ports", h.HandleGetServicePorts},
		{"healthcheck", h.HandleGetServiceHealthcheck},
		{"placement", h.HandleGetServicePlacement},
		{"update-policy", h.HandleGetServiceUpdatePolicy},
		{"rollback-policy", h.HandleGetServiceRollbackPolicy},
		{"log-driver", h.HandleGetServiceLogDriver},
		{"container-config", h.HandleGetServiceContainerConfig},
		{"configs", h.HandleGetServiceConfigs},
		{"secrets", h.HandleGetServiceSecrets},
		{"networks", h.HandleGetServiceNetworks},
		{"mounts", h.HandleGetServiceMounts},
	}

	for _, sub := range subResources {
		t.Run(sub.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/services/svc1/"+sub.name, nil)
			req.SetPathValue("id", "svc1")
			req = req.WithContext(auth.ContextWithIdentity(req.Context(), identity))
			w := httptest.NewRecorder()
			sub.handler(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("GET /services/svc1/%s: expected 403, got %d", sub.name, w.Code)
			}
		})
	}
}

func TestNodeSubResourceGET_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"node:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	identity := &auth.Identity{Subject: "user1"}

	for _, sub := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"role", h.HandleGetNodeRole},
		{"labels", h.HandleGetNodeLabels},
	} {
		t.Run(sub.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/nodes/node1/"+sub.name, nil)
			req.SetPathValue("id", "node1")
			req = req.WithContext(auth.ContextWithIdentity(req.Context(), identity))
			w := httptest.NewRecorder()
			sub.handler(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("GET /nodes/node1/%s: expected 403, got %d", sub.name, w.Code)
			}
		})
	}
}

func TestConfigLabelsGET_ACLDenied(t *testing.T) {
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
	req := httptest.NewRequest("GET", "/configs/cfg1/labels", nil)
	req.SetPathValue("id", "cfg1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetConfigLabels(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestSecretLabelsGET_ACLDenied(t *testing.T) {
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
	req := httptest.NewRequest("GET", "/secrets/sec1/labels", nil)
	req.SetPathValue("id", "sec1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetSecretLabels(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandleClusterMetrics_ACL001_NoGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))
	req := httptest.NewRequest("GET", "/cluster/metrics", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleClusterMetrics(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleSearch_ACL001_NoGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))
	req := httptest.NewRequest("GET", "/search?q=test", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "bob"}))
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetUnlockKey_ACLDenied_ReadOnly(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		// User has read on services but no swarm:cluster write.
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withACL(e))

	// Compose the middleware chain as the router does: swarmACL wraps the handler.
	handler := h.requireWriteACL(swarmResource)(http.HandlerFunc(h.HandleGetUnlockKey))

	req := httptest.NewRequest("GET", "/swarm/unlock-key", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 (unlock key requires swarm:cluster write)", w.Code)
	}
	assertACLErrorCode(t, w, "ACL002")
}

func TestGetUnlockKey_BlockedAtOpsLevel0(t *testing.T) {
	h := newTestHandlers(t, withOpsLevel(config.OpsReadOnly), withSystemClient(&mockSystemClient{
		getUnlockKeyFn: func(_ context.Context) (string, error) {
			t.Fatal("handler should not be reached at ops level 0")
			return "", nil
		},
	}))

	handler := handlersAt(config.OpsReadOnly).requireLevel(config.OpsImpactful)(
		http.HandlerFunc(h.HandleGetUnlockKey),
	)

	req := httptest.NewRequest("GET", "/swarm/unlock-key", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 (unlock key blocked at ops level 0)", w.Code)
	}
}

// stubChecker is a recommendations.Checker that returns fixed results.
type stubChecker struct {
	results []recommendations.Recommendation
}

func (s *stubChecker) Name() string                                           { return "stub" }
func (s *stubChecker) Interval() time.Duration                                { return time.Hour }
func (s *stubChecker) Check(context.Context) []recommendations.Recommendation { return s.results }

func TestHandleRecommendations_ACLFiltering(t *testing.T) {
	recs := []recommendations.Recommendation{
		{
			Scope:      recommendations.ScopeService,
			TargetID:   "svc1",
			TargetName: "webapp",
			Message:    "webapp needs more memory",
		},
		{
			Scope:      recommendations.ScopeService,
			TargetID:   "svc2",
			TargetName: "billing",
			Message:    "billing is over-provisioned",
		},
		{
			Scope:      recommendations.ScopeNode,
			TargetID:   "node1",
			TargetName: "worker-1",
			Message:    "worker-1 disk pressure",
		},
	}

	engine := recommendations.NewEngine(&stubChecker{results: recs})

	// Force a tick so the engine has results. Run does one synchronous tick
	// before entering its loop, and returns immediately on a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.Run(ctx)

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		// Only grant read on service:webapp — billing and node recs should be filtered out.
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e), withRecEngine(engine))
	req := httptest.NewRequest("GET", "/recommendations", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleRecommendations(w, req)

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
		t.Fatalf("expected 1 recommendation (webapp only), got %d", resp.Total)
	}
}

func TestHandleTopology_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:       "svc1",
		Spec:     swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
		Endpoint: swarm.Endpoint{VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}}},
	})
	c.SetService(swarm.Service{
		ID:       "svc2",
		Spec:     swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-service"}},
		Endpoint: swarm.Endpoint{VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
		{Resources: []string{"node:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/topology", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleTopology(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var doc jgf.Document
	if err := json.NewDecoder(w.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Network graph should only contain webapp, not secret-service.
	networkGraph := doc.Graphs[0]
	if len(networkGraph.Nodes) != 1 {
		t.Errorf("network graph nodes=%d, want 1 (only webapp)", len(networkGraph.Nodes))
	}
	if _, ok := networkGraph.Nodes[jgf.URN("service", "svc1")]; !ok {
		t.Error("expected webapp (svc1) in network graph")
	}
}

// A service write grant must not attach what the caller cannot read: a mounted
// secret is readable from inside the container, so this is where it leaks.
func TestPatchServiceAttachments_RequireReadOnTheAttachment(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		handler func(*Handlers) http.HandlerFunc
		body    func(id, name string) string
	}{
		{
			name:    "secrets",
			path:    "/services/svc1/secrets",
			handler: func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceSecrets },
			body: func(id, name string) string {
				return fmt.Sprintf(`{"secrets":[{"secretID":%q,"secretName":%q}]}`, id, name)
			},
		},
		{
			name:    "configs",
			path:    "/services/svc1/configs",
			handler: func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceConfigs },
			body: func(id, name string) string {
				return fmt.Sprintf(`{"configs":[{"configID":%q,"configName":%q}]}`, id, name)
			},
		},
		{
			name:    "networks",
			path:    "/services/svc1/networks",
			handler: func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceNetworks },
			body: func(id, _ string) string {
				return fmt.Sprintf(`{"networks":[{"target":%q}]}`, id)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cache.New(nil)
			c.SetService(
				swarm.Service{
					ID: "svc1",
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{Name: "web"},
						TaskTemplate: swarm.TaskSpec{
							ContainerSpec: &swarm.ContainerSpec{
								Secrets: []*swarm.SecretReference{{SecretID: "x-held"}},
								Configs: []*swarm.ConfigReference{{ConfigID: "x-held"}},
							},
							Networks: []swarm.NetworkAttachmentConfig{{Target: "x-held"}},
						},
					},
				},
			)
			for id, name := range map[string]string{
				"x-mine":   "mine",
				"x-theirs": "theirs",
				"x-held":   "held",
			} {
				c.SetSecret(
					swarm.Secret{
						ID:   id,
						Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
					},
				)
				c.SetConfig(
					swarm.Config{
						ID:   id,
						Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}},
					},
				)
				c.SetNetwork(network.Summary{ID: id, Name: name})
			}

			e := acl.NewEvaluator()
			e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
				{
					Resources:   []string{"service:web"},
					Audience:    []string{"*"},
					Permissions: []string{"write"},
				},
				{
					Resources:   []string{"*:mine"},
					Audience:    []string{"*"},
					Permissions: []string{"read"},
				},
			}})

			written := false
			mock := &mockWriteClient{
				updateServiceSecretsFn: func(context.Context, string, []*swarm.SecretReference) (swarm.Service, error) {
					written = true
					return swarm.Service{ID: "svc1"}, nil
				},
				updateServiceConfigsFn: func(context.Context, string, []*swarm.ConfigReference) (swarm.Service, error) {
					written = true
					return swarm.Service{ID: "svc1"}, nil
				},
				updateServiceNetworksFn: func(context.Context, string, []swarm.NetworkAttachmentConfig) (swarm.Service, error) {
					written = true
					return swarm.Service{ID: "svc1"}, nil
				},
			}
			h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(mock))

			send := func(id, name string) *httptest.ResponseRecorder {
				req := httptest.NewRequest("PATCH", tc.path, strings.NewReader(tc.body(id, name)))
				req.Header.Set("Content-Type", "application/merge-patch+json")
				req.SetPathValue("id", "svc1")
				req = req.WithContext(
					auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "dev"}),
				)
				w := httptest.NewRecorder()
				tc.handler(h)(w, req)

				return w
			}

			// The client-supplied name must not stand in for the real one.
			if w := send("x-theirs", "mine"); w.Code != http.StatusForbidden {
				t.Fatalf(
					"unreadable attachment: status=%d, want 403; body: %s",
					w.Code,
					w.Body.String(),
				)
			}
			if w := send("unknown", "mine"); w.Code != http.StatusBadRequest {
				t.Fatalf(
					"unknown attachment: status=%d, want 400; body: %s",
					w.Code,
					w.Body.String(),
				)
			} else {
				assertACLErrorCode(t, w, "SVC021")
			}
			if written {
				t.Fatal("a refused attachment reached the writer")
			}

			if w := send("x-mine", "mine"); w.Code != http.StatusOK {
				t.Fatalf(
					"readable attachment: status=%d, want 200; body: %s",
					w.Code,
					w.Body.String(),
				)
			}

			// Re-sending what the service already carries must not need read on it.
			if w := send("x-held", "held"); w.Code != http.StatusOK {
				t.Fatalf(
					"already-attached reference: status=%d, want 200; body: %s",
					w.Code,
					w.Body.String(),
				)
			}
		})
	}
}

// Without a policy nothing is denied, so a reference the cache has not seen yet
// is reported as unknown, never as an access refusal.
func TestPatchServiceAttachments_UnknownReferenceWithoutPolicy(t *testing.T) {
	cases := map[string]struct {
		path    string
		handler func(*Handlers) http.HandlerFunc
		body    string
	}{
		"secrets": {
			"/services/svc1/secrets",
			func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceSecrets },
			`{"secrets":[{"secretID":"unknown","secretName":"db"}]}`,
		},
		"configs": {
			"/services/svc1/configs",
			func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceConfigs },
			`{"configs":[{"configID":"unknown","configName":"app"}]}`,
		},
		"networks": {
			"/services/svc1/networks",
			func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceNetworks },
			`{"networks":[{"target":"unknown"}]}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := cache.New(nil)
			c.SetService(swarm.Service{ID: "svc1"})
			h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

			req := httptest.NewRequest("PATCH", tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/merge-patch+json")
			req.SetPathValue("id", "svc1")
			w := httptest.NewRecorder()
			tc.handler(h)(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; body: %s", w.Code, w.Body.String())
			}
			assertACLErrorCode(t, w, "SVC021")
		})
	}
}

// Creation is authorized on the name being created, the key MCP checks, so a
// grant scoped to a team's prefix can create inside it and nowhere else.
func TestCreateDataResource_AuthorizesTheNameBeingCreated(t *testing.T) {
	for _, kind := range []string{"secret", "config"} {
		t.Run(kind, func(t *testing.T) {
			e := acl.NewEvaluator()
			e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
				{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
				{
					Resources:   []string{kind + ":team-*"},
					Audience:    []string{"*"},
					Permissions: []string{"write"},
				},
			}})

			var created []string
			mock := &mockWriteClient{
				createConfigFn: func(_ context.Context, spec swarm.ConfigSpec) (string, error) {
					created = append(created, spec.Name)
					return "new-id", nil
				},
				createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
					created = append(created, spec.Name)
					return "new-id", nil
				},
			}
			router := newTestRouterWithCache(t, cache.New(nil), withACL(e), withWriteClient(mock))

			create := func(name string) int {
				body := fmt.Sprintf(`{"name":%q,"data":"aGVsbG8="}`, name)
				req := httptest.NewRequest("POST", "/"+kind+"s", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				return w.Code
			}

			if code := create("other-db"); code != http.StatusForbidden {
				t.Errorf("outside the grant: status=%d, want 403", code)
			}
			if code := create("team-db"); code != http.StatusCreated {
				t.Errorf("inside the grant: status=%d, want 201", code)
			}
			if len(created) != 1 || created[0] != "team-db" {
				t.Errorf("created %v, want only team-db", created)
			}

			req := httptest.NewRequest("GET", "/"+kind+"s", nil)
			req.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if allow := w.Header().Get("Allow"); !strings.Contains(allow, "POST") {
				t.Errorf("Allow = %q, want POST for a caller who can create some %s", allow, kind)
			}
		})
	}
}

func TestCreateDataResource_RefusedWithoutAnyWriteGrant(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})
	router := newTestRouterWithCache(
		t,
		cache.New(nil),
		withACL(e),
		withWriteClient(&mockWriteClient{}),
	)

	for _, kind := range []string{"secret", "config"} {
		req := httptest.NewRequest(
			"POST",
			"/"+kind+"s",
			strings.NewReader(`{"name":"db","data":"aGVsbG8="}`),
		)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("POST /%ss: status=%d, want 403", kind, w.Code)
		}

		req = httptest.NewRequest("GET", "/"+kind+"s", nil)
		req.Header.Set("Accept", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if allow := w.Header().Get("Allow"); strings.Contains(allow, "POST") {
			t.Errorf("GET /%ss: Allow = %q, want no POST", kind, allow)
		}
	}
}

// A plugin's name is not known until its image is pulled, so installing one
// still needs a type-wide grant.
func TestInstallPlugin_RequiresTypeWideWriteGrant(t *testing.T) {
	for _, tc := range []struct {
		grant      string
		wantCreate bool
	}{
		{"plugin:team-*", false},
		{"plugin:*", true},
	} {
		t.Run(tc.grant, func(t *testing.T) {
			e := acl.NewEvaluator()
			e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
				{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
				{
					Resources:   []string{tc.grant},
					Audience:    []string{"*"},
					Permissions: []string{"write"},
				},
			}})
			installed := false
			router := newTestRouterWithCache(
				t,
				cache.New(nil),
				withACL(e),
				withPluginClient(&mockPluginClient{
					pluginListFn: func(context.Context) (types.PluginsListResponse, error) {
						return nil, nil
					},
					pluginInstallFn: func(context.Context, string) (*types.Plugin, error) {
						installed = true
						return &types.Plugin{Name: "team-plugin:latest"}, nil
					},
				}),
			)

			req := httptest.NewRequest(
				"POST",
				"/plugins",
				strings.NewReader(`{"remote":"team-plugin:latest"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			wantCode := http.StatusForbidden
			if tc.wantCreate {
				wantCode = http.StatusCreated
			}
			if w.Code != wantCode || installed != tc.wantCreate {
				t.Errorf(
					"POST /plugins: status=%d installed=%v, want %d",
					w.Code,
					installed,
					wantCode,
				)
			}

			req = httptest.NewRequest("GET", "/plugins", nil)
			req.Header.Set("Accept", "application/json")
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("GET /plugins: status=%d, want 200", w.Code)
			}
			if allow := w.Header().Get("Allow"); strings.Contains(allow, "POST") != tc.wantCreate {
				t.Errorf("GET /plugins: Allow = %q, want POST: %v", allow, tc.wantCreate)
			}
		})
	}
}

// Rotation is create-then-repoint, and the attach check resolves through the
// cache the watcher fills only later, so the create must seed it.
func TestCreatedDataResourceIsImmediatelyAttachable(t *testing.T) {
	for _, kind := range []string{"secret", "config"} {
		t.Run(kind, func(t *testing.T) {
			c := cache.New(nil)
			c.SetService(
				swarm.Service{
					ID:   "svc1",
					Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
				},
			)
			mock := &mockWriteClient{
				createConfigFn: func(context.Context, swarm.ConfigSpec) (string, error) {
					return "fresh", nil
				},
				createSecretFn: func(context.Context, swarm.SecretSpec) (string, error) {
					return "fresh", nil
				},
				updateServiceSecretsFn: func(context.Context, string, []*swarm.SecretReference) (swarm.Service, error) {
					return swarm.Service{ID: "svc1"}, nil
				},
				updateServiceConfigsFn: func(context.Context, string, []*swarm.ConfigReference) (swarm.Service, error) {
					return swarm.Service{ID: "svc1"}, nil
				},
			}
			router := newTestRouterWithCache(t, c, withWriteClient(mock))

			send := func(method, path, contentType, body string) int {
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", contentType)
				req.Header.Set("Accept", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				return w.Code
			}

			if code := send(
				"POST", "/"+kind+"s", "application/json", `{"name":"rot_v1","data":"aGVsbG8="}`,
			); code != http.StatusCreated {
				t.Fatalf("create: status=%d, want 201", code)
			}

			body := fmt.Sprintf(`{%q:[{%q:"fresh",%q:"rot_v1"}]}`, kind+"s", kind+"ID", kind+"Name")
			if code := send(
				"PATCH", "/services/svc1/"+kind+"s", "application/merge-patch+json", body,
			); code != http.StatusOK {
				t.Errorf("attach the new %s: status=%d, want 200", kind, code)
			}
		})
	}
}

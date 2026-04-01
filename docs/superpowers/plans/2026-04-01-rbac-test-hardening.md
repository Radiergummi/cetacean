# RBAC Test Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close all critical and high-priority test gaps in the RBAC/ACL system so that accidental removal of any ACL guard is caught by tests.

**Architecture:** All new tests are added to the existing `internal/api/acl_integration_test.go` file, following the established pattern of `newTestHandlers(t, withCache(c), withACL(e))` + injected identity via `auth.ContextWithIdentity`. Each task covers one category of gap and is independently committable.

**Tech Stack:** Go stdlib `testing`, `httptest`, `github.com/goccy/go-json`

---

## File Structure

All tests go into one file:
- **Modify:** `internal/api/acl_integration_test.go` — all new ACL integration tests

The existing file has 282 lines with tests for services list filtering, cluster ACL001, service tasks/logs denial, task logs denial, and node tasks filtering. We add tests for every other handler.

---

### Task 1: List Handler ACL Filter Tests

**Files:**
- Modify: `internal/api/acl_integration_test.go`

Tests that verify `acl.Filter()` is actually called in each list handler — if the call were removed, these tests would fail (they'd return all items instead of the filtered subset).

- [ ] **Step 1: Write the tests**

Add to `internal/api/acl_integration_test.go`:

```go
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
		{Resources: []string{"node:worker-1"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/nodes", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct{ Total int `json:"total"` }
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered node, got %d", resp.Total)
	}
}

func TestHandleListTasks_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", NodeID: "n1"})
	c.SetTask(swarm.Task{ID: "task2", ServiceID: "svc2", NodeID: "n1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"task:task1"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/tasks", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct{ Total int `json:"total"` }
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
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "db-config"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"config:app-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/configs", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct{ Total int `json:"total"` }
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
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db-secret"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"secret:app-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/secrets", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct{ Total int `json:"total"` }
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered secret, got %d", resp.Total)
	}
}

func TestHandleListNetworks_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(networkSummary("net1", "frontend"))
	c.SetNetwork(networkSummary("net2", "backend"))

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"network:frontend"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/networks", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct{ Total int `json:"total"` }
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered network, got %d", resp.Total)
	}
}

func TestHandleListVolumes_ACLFiltering(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volumeEntry("data-vol"))
	c.SetVolume(volumeEntry("logs-vol"))

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"volume:data-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/volumes", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct{ Total int `json:"total"` }
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
				Name:   "webapp_api",
				Labels: map[string]string{"com.docker.stack.namespace": "webapp"},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"stack:monitoring"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/stacks", nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct{ Total int `json:"total"` }
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 filtered stack, got %d", resp.Total)
	}
}
```

Note: `networkSummary` and `volumeEntry` are helper functions that may already exist in the test file. If not, add them — check the existing handlers_test.go for how networks and volumes are created in tests.

- [ ] **Step 2: Check for and add any missing test helpers**

Search `handlers_test.go` for `networkSummary` and `volumeEntry` helpers. If they don't exist, add them to `acl_integration_test.go`:

```go
// Only add if not already defined elsewhere in the test package.
```

Use `Grep` to find how networks and volumes are created in existing tests and match the pattern.

- [ ] **Step 3: Run the tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleList.*ACLFiltering" -v`
Expected: All 7 new tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/api/acl_integration_test.go
git commit -m "test(acl): add ACL filter tests for all list handlers"
```

---

### Task 2: Detail Handler ACL Denial Tests

**Files:**
- Modify: `internal/api/acl_integration_test.go`

Tests that verify each detail handler returns 403 with ACL001 when the identity cannot read the resource.

- [ ] **Step 1: Write the tests**

Add to `internal/api/acl_integration_test.go`:

```go
func TestHandleGetNode_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"node:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/nodes/node1", nil)
	req.SetPathValue("id", "node1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
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
		{Resources: []string{"service:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetTask_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", NodeID: "n1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"task:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
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
		{Resources: []string{"config:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetConfig(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
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
		{Resources: []string{"secret:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetNetwork_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(networkSummary("net1", "frontend"))

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"network:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/networks/net1", nil)
	req.SetPathValue("id", "net1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetNetwork(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}

func TestHandleGetVolume_ACLDenied(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volumeEntry("data-vol"))

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"volume:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/volumes/data-vol", nil)
	req.SetPathValue("name", "data-vol")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetVolume(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
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
		{Resources: []string{"stack:other"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	req := httptest.NewRequest("GET", "/stacks/monitoring", nil)
	req.SetPathValue("name", "monitoring")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	h.HandleGetStack(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL001")
}
```

Also add the `assertACLErrorCode` helper at the top of the file (or bottom, after existing tests):

```go
// assertACLErrorCode decodes the response body as a ProblemDetail and asserts
// the type URI contains the expected error code.
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
```

Add `"strings"` to the import block.

- [ ] **Step 2: Run the tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleGet.*ACLDenied" -v`
Expected: All 8 tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/acl_integration_test.go
git commit -m "test(acl): add 403 denial tests for all detail handlers"
```

---

### Task 3: Cross-Reference Filtering in Detail Responses

**Files:**
- Modify: `internal/api/acl_integration_test.go`

Tests that the "services using this resource" list in config/secret/network/volume detail responses is filtered by ACL — a user who can read a config should not see service names they can't access.

- [ ] **Step 1: Write the tests**

Add to `internal/api/acl_integration_test.go`:

```go
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
					Configs: []swarm.ConfigReference{
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
					Configs: []swarm.ConfigReference{
						{ConfigID: "cfg1", ConfigName: "shared-config"},
					},
				},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"config:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
		{Resources: []string{"service:allowed-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
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
		t.Fatalf("expected 1 cross-referenced service (allowed-svc), got %d", len(resp.Services))
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
					Secrets: []swarm.SecretReference{
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
					Secrets: []swarm.SecretReference{
						{SecretID: "sec1", SecretName: "shared-secret"},
					},
				},
			},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"secret:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
		{Resources: []string{"service:allowed-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
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
		t.Fatalf("expected 1 cross-referenced service, got %d", len(resp.Services))
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleGet.*CrossRefFiltering" -v`
Expected: Both pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/acl_integration_test.go
git commit -m "test(acl): add cross-reference filtering tests for config/secret detail"
```

---

### Task 4: Search Endpoint ACL Filtering

**Files:**
- Modify: `internal/api/acl_integration_test.go`

Tests that `HandleSearch` filters results by ACL — a user scoped to specific services should not see other resources in search results.

- [ ] **Step 1: Write the test**

```go
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
		{Resources: []string{"service:webapp-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
		// No node grants — nodes should be filtered out of search results.
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
		Services struct {
			Count int `json:"count"`
		} `json:"services"`
		Nodes struct {
			Count int `json:"count"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Services.Count != 1 {
		t.Errorf("expected 1 service in search results, got %d", resp.Services.Count)
	}
	if resp.Nodes.Count != 0 {
		t.Errorf("expected 0 nodes in search results (no node grants), got %d", resp.Nodes.Count)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestHandleSearch_ACLFiltering -v`
Expected: Pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/acl_integration_test.go
git commit -m "test(acl): add search endpoint ACL filtering test"
```

---

### Task 5: Write Handler ACL Integration via `requireWriteACL` + Name Resolver

**Files:**
- Modify: `internal/api/acl_integration_test.go`

Tests that the `requireWriteACL` middleware, when composed with the actual name resolver functions (`serviceName`, `taskServiceResource`), correctly gates access. This tests the full chain: request → path value → cache lookup → resource name → ACL check.

- [ ] **Step 1: Write the tests**

```go
func TestServiceScaleACL_DeniedByResourceName(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "webapp"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptr(uint64(1))}},
		},
		ServiceStatus: &swarm.ServiceStatus{RunningTasks: 1, DesiredTasks: 1},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		// Read on all services, but write only on "other" — not "webapp".
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
		{Resources: []string{"service:other"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(&stubWriteClient{}))

	// Compose the middleware chain as the router does.
	handler := h.requireWriteACL(h.serviceName)(
		requireLevel(config.OpsOperational, config.OpsImpactful)(h.HandleScaleService),
	)

	body := strings.NewReader(`{"replicas": 3}`)
	req := httptest.NewRequest("PUT", "/services/svc1/scale", body)
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL002")
}

func TestServiceScaleACL_AllowedByResourceName(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "webapp"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptr(uint64(1))}},
		},
		ServiceStatus: &swarm.ServiceStatus{RunningTasks: 1, DesiredTasks: 1},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(&stubWriteClient{}))

	handler := h.requireWriteACL(h.serviceName)(
		requireLevel(config.OpsOperational, config.OpsImpactful)(h.HandleScaleService),
	)

	body := strings.NewReader(`{"replicas": 3}`)
	req := httptest.NewRequest("PUT", "/services/svc1/scale", body)
	req.SetPathValue("id", "svc1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("expected scale to be allowed, got 403")
	}
}

func TestTaskRemoveACL_ResolvesToParentService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", NodeID: "n1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		// Write grant on "service:webapp" — task removal should resolve task→service.
		{Resources: []string{"service:webapp"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(&stubWriteClient{}))

	handler := h.requireWriteACL(h.taskServiceResource)(
		requireLevel(config.OpsImpactful, config.OpsImpactful)(h.HandleRemoveTask),
	)

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should be allowed because taskServiceResource resolves task1→svc1→"service:webapp".
	if w.Code == http.StatusForbidden {
		t.Fatalf("expected task removal to be allowed via parent service grant, got 403")
	}
}

func TestTaskRemoveACL_DeniedWhenParentServiceNotGranted(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1", NodeID: "n1"})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:other"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(&stubWriteClient{}))

	handler := h.requireWriteACL(h.taskServiceResource)(
		requireLevel(config.OpsImpactful, config.OpsImpactful)(h.HandleRemoveTask),
	)

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	assertACLErrorCode(t, w, "ACL002")
}
```

Note: `stubWriteClient` should already exist in `write_handlers_test.go`. If not, check what mock write client is used there and reference the same type. `ptr` is a helper for `&uint64(1)` — check if it exists in the test package.

- [ ] **Step 2: Run the tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestServiceScaleACL|TestTaskRemoveACL" -v`
Expected: All 4 pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/acl_integration_test.go
git commit -m "test(acl): add write handler ACL integration tests with name resolvers"
```

---

### Task 6: Evaluator Edge Cases — Task→Service→Stack Chain and Case Sensitivity

**Files:**
- Modify: `internal/acl/evaluator_test.go`

- [ ] **Step 1: Write the tests**

Add to `internal/acl/evaluator_test.go`:

```go
func TestEvaluator_TaskInheritsThroughStack(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"stack:monitoring"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})
	e.SetResolver(&stubResolver{
		services: map[string]string{"task-prom-1": "prometheus"},
		stacks:   map[string]string{"service:prometheus": "monitoring"},
	})

	id := &auth.Identity{Subject: "user1"}

	if !e.Can(id, "read", "task:task-prom-1") {
		t.Fatal("task should be readable via task→service→stack chain")
	}
}

func TestEvaluator_CaseSensitiveResourceNames(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:WebApp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	id := &auth.Identity{Subject: "user1"}

	if !e.Can(id, "read", "service:WebApp") {
		t.Fatal("exact case match should be allowed")
	}
	if e.Can(id, "read", "service:webapp") {
		t.Fatal("different case should not match (Docker names are case-sensitive)")
	}
}

func TestEvaluator_CaseSensitiveAudience(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:Alice"},
			Permissions: []string{"read"},
		},
	}})

	if !e.Can(&auth.Identity{Subject: "Alice"}, "read", "service:foo") {
		t.Fatal("exact case audience should match")
	}
	if e.Can(&auth.Identity{Subject: "alice"}, "read", "service:foo") {
		t.Fatal("different case audience should not match")
	}
}

func TestEvaluator_OverlappingGrantsUnion(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"group:ops"},
			Permissions: []string{"read"},
		},
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"group:dev"},
			Permissions: []string{"write"},
		},
	}})

	id := &auth.Identity{Subject: "alice", Groups: []string{"ops", "dev"}}

	if !e.Can(id, "read", "service:backend") {
		t.Fatal("ops group should grant read on all services")
	}
	if !e.Can(id, "write", "service:webapp") {
		t.Fatal("dev group should grant write on webapp")
	}
	if e.Can(id, "write", "service:backend") {
		t.Fatal("dev group write should not extend to backend")
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ -run "TestEvaluator_TaskInherits|TestEvaluator_CaseSensitive|TestEvaluator_OverlappingGrantsUnion" -v`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add internal/acl/evaluator_test.go
git commit -m "test(acl): add task→service→stack chain, case sensitivity, and grant union tests"
```

---

### Task 7: `requireAnyGrant` on Remaining Endpoints and ACL001 Error Code Assertion Fix

**Files:**
- Modify: `internal/api/acl_integration_test.go`

Tests that `requireAnyGrant` blocks access on endpoints that use it (besides `/cluster` which is already tested). Also fixes the existing `TestHandleCluster_ACL001_NoGrants` to assert the actual error code.

- [ ] **Step 1: Write the tests and fix**

```go
func TestHandleStackSummary_ACL001_NoGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:*"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
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
```

Also fix the existing `TestHandleCluster_ACL001_NoGrants` — change the assertion from:
```go
if problem.Type == "" {
    t.Fatal("problem type should be set")
}
```
to use the new helper:
```go
assertACLErrorCode(t, w, "ACL001")
```

This requires removing the existing manual decode in that test and resetting `w.Body` before calling the helper. The simplest fix: replace the entire problem decoding block with `assertACLErrorCode(t, w, "ACL001")`.

- [ ] **Step 2: Run the tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle.*ACL001" -v`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/acl_integration_test.go
git commit -m "test(acl): add requireAnyGrant tests and fix ACL001 error code assertion"
```

---

### Task 8: Full Test Suite Verification

**Files:** None new.

- [ ] **Step 1: Run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`
Expected: All packages pass.

- [ ] **Step 2: Run lint**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint`
Expected: 0 issues.

- [ ] **Step 3: Run format check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make fmt-check`
Expected: Clean.

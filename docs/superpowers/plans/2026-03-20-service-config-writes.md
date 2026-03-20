# Service Config Write Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GET + write endpoints for five service sub-resources: placement (PUT), ports (PATCH), update policy (PATCH), rollback policy (PATCH), and log driver (PATCH).

**Architecture:** Each endpoint follows the existing resources/healthcheck template: Docker client method (inspect → mutate → update → re-inspect), GET handler with ETag, write handler with merge-patch or PUT, router registration behind `tier1` middleware. All five are structurally identical with different spec fields.

**Tech Stack:** Go stdlib, Docker Engine API (`swarm` types), existing `mergePatch` helper, existing test mock patterns

**Spec:** `docs/superpowers/specs/2026-03-20-service-config-writes-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/docker/client.go` | Five new `UpdateService*` methods |
| Modify | `internal/api/handlers.go` | Five new methods on `DockerWriteClient` interface |
| Modify | `internal/api/write_handlers.go` | Five GET + five write handlers |
| Modify | `internal/api/router.go` | Ten new route registrations |
| Modify | `internal/api/write_handlers_test.go` | Tests for all new handlers |
| Modify | `internal/config/config.go` | Update `OpsOperational` godoc |
| Modify | `docs/configuration.md` | Add new endpoints to tier 1 list |
| Modify | `CLAUDE.md` | Architecture + conventions updates |
| Modify | `api/openapi.yaml` | New endpoint definitions |
| Modify | `CHANGELOG.md` | Release notes |

---

### Task 1: Docker client methods

**Files:**
- Modify: `internal/docker/client.go`

- [ ] **Step 1: Add `UpdateServicePlacement`**

Append after the existing `UpdateServiceResources` method:

```go
func (c *Client) UpdateServicePlacement(ctx context.Context, id string, placement *swarm.Placement) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.Placement = placement
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 2: Add `UpdateServicePorts`**

```go
func (c *Client) UpdateServicePorts(ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.EndpointSpec == nil {
		svc.Spec.EndpointSpec = &swarm.EndpointSpec{}
	}
	svc.Spec.EndpointSpec.Ports = ports
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

Note: preserves existing `EndpointSpec.Mode` by only setting `Ports`. Guards nil `EndpointSpec`.

- [ ] **Step 3: Add `UpdateServiceUpdatePolicy`**

```go
func (c *Client) UpdateServiceUpdatePolicy(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.UpdateConfig = policy
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 4: Add `UpdateServiceRollbackPolicy`**

```go
func (c *Client) UpdateServiceRollbackPolicy(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.RollbackConfig = policy
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 5: Add `UpdateServiceLogDriver`**

```go
func (c *Client) UpdateServiceLogDriver(ctx context.Context, id string, driver *swarm.Driver) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.LogDriver = driver
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 6: Verify compilation**

Run: `go build ./internal/docker/`
Expected: compiles cleanly

- [ ] **Step 7: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat(docker): add client methods for placement, ports, update/rollback policy, log driver"
```

---

### Task 2: Update `DockerWriteClient` interface

**Files:**
- Modify: `internal/api/handlers.go`

- [ ] **Step 1: Add five new methods to `DockerWriteClient`**

Add before the closing `}` of the interface, after `UpdateServiceHealthcheck`:

```go
	UpdateServicePlacement(
		ctx context.Context,
		id string,
		placement *swarm.Placement,
	) (swarm.Service, error)
	UpdateServicePorts(
		ctx context.Context,
		id string,
		ports []swarm.PortConfig,
	) (swarm.Service, error)
	UpdateServiceUpdatePolicy(
		ctx context.Context,
		id string,
		policy *swarm.UpdateConfig,
	) (swarm.Service, error)
	UpdateServiceRollbackPolicy(
		ctx context.Context,
		id string,
		policy *swarm.UpdateConfig,
	) (swarm.Service, error)
	UpdateServiceLogDriver(
		ctx context.Context,
		id string,
		driver *swarm.Driver,
	) (swarm.Service, error)
```

- [ ] **Step 2: Add mock implementations to `write_handlers_test.go`**

Add fields to `mockWriteClient` struct:

```go
	updateServicePlacementFn      func(ctx context.Context, id string, placement *swarm.Placement) (swarm.Service, error)
	updateServicePortsFn          func(ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error)
	updateServiceUpdatePolicyFn   func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error)
	updateServiceRollbackPolicyFn func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error)
	updateServiceLogDriverFn      func(ctx context.Context, id string, driver *swarm.Driver) (swarm.Service, error)
```

Add corresponding methods (following existing pattern):

```go
func (m *mockWriteClient) UpdateServicePlacement(ctx context.Context, id string, placement *swarm.Placement) (swarm.Service, error) {
	if m.updateServicePlacementFn != nil {
		return m.updateServicePlacementFn(ctx, id, placement)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServicePorts(ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error) {
	if m.updateServicePortsFn != nil {
		return m.updateServicePortsFn(ctx, id, ports)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceUpdatePolicy(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
	if m.updateServiceUpdatePolicyFn != nil {
		return m.updateServiceUpdatePolicyFn(ctx, id, policy)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceRollbackPolicy(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
	if m.updateServiceRollbackPolicyFn != nil {
		return m.updateServiceRollbackPolicyFn(ctx, id, policy)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceLogDriver(ctx context.Context, id string, driver *swarm.Driver) (swarm.Service, error) {
	if m.updateServiceLogDriverFn != nil {
		return m.updateServiceLogDriverFn(ctx, id, driver)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/api/`
Expected: compiles cleanly

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers.go internal/api/write_handlers_test.go
git commit -m "feat(api): add service config write methods to DockerWriteClient interface"
```

---

### Task 3: Placement handlers (GET + PUT)

**Files:**
- Modify: `internal/api/write_handlers.go`
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/api/write_handlers_test.go`:

```go
func TestHandleGetServicePlacement(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				Placement: &swarm.Placement{
					Constraints: []string{"node.role==manager"},
				},
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/svc1/placement", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServicePlacement(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	placement := resp["placement"].(map[string]any)
	constraints := placement["Constraints"].([]any)
	if len(constraints) != 1 || constraints[0] != "node.role==manager" {
		t.Errorf("unexpected placement: %v", resp)
	}
}

func TestHandleGetServicePlacement_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/missing/placement", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServicePlacement(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePutServicePlacement(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				Placement: &swarm.Placement{
					Constraints: []string{"node.role==worker"},
				},
			},
		},
	}
	mock := &mockWriteClient{
		updateServicePlacementFn: func(ctx context.Context, id string, placement *swarm.Placement) (swarm.Service, error) {
			return updated, nil
		},
	}

	h := NewHandlers(c, nil, nil, nil, mock, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`{"Constraints":["node.role==worker"]}`)
	req := httptest.NewRequest("PUT", "/services/svc1/placement", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServicePlacement(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandlePutServicePlacement_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest("PUT", "/services/svc1/placement", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServicePlacement(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePutServicePlacement_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`{"Constraints":[]}`)
	req := httptest.NewRequest("PUT", "/services/missing/placement", body)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandlePutServicePlacement(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandleGetServicePlacement -v 2>&1 | head -10`
Expected: FAIL — methods don't exist

- [ ] **Step 3: Implement handlers**

Add to `internal/api/write_handlers.go`:

```go
func (h *Handlers) HandleGetServicePlacement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	placement := svc.Spec.TaskTemplate.Placement
	if placement == nil {
		placement = &swarm.Placement{}
	}
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/placement", "ServicePlacement", map[string]any{
		"placement": placement,
	}))
}

func (h *Handlers) HandlePutServicePlacement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var placement swarm.Placement
	if err := json.NewDecoder(r.Body).Decode(&placement); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	slog.Info("updating service placement", "service", id)

	updated, err := h.writeClient.UpdateServicePlacement(r.Context(), id, &placement)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	resultPlacement := updated.Spec.TaskTemplate.Placement
	if resultPlacement == nil {
		resultPlacement = &swarm.Placement{}
	}
	writeJSON(w, NewDetailResponse("/services/"+id+"/placement", "ServicePlacement", map[string]any{
		"placement": resultPlacement,
	}))
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run TestHandle.*ServicePlacement -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat(api): add placement GET + PUT handlers"
```

---

### Task 4: Ports handlers (GET + PATCH)

**Files:**
- Modify: `internal/api/write_handlers.go`
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/api/write_handlers_test.go`:

```go
func TestHandleGetServicePorts(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			EndpointSpec: &swarm.EndpointSpec{
				Ports: []swarm.PortConfig{
					{Protocol: "tcp", TargetPort: 80, PublishedPort: 8080, PublishMode: swarm.PortConfigPublishModeIngress},
				},
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/svc1/ports", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServicePorts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandleGetServicePorts_NilEndpointSpec(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/svc1/ports", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServicePorts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	ports := resp["ports"].([]any)
	if len(ports) != 0 {
		t.Errorf("expected empty ports, got %v", ports)
	}
}

func TestHandlePatchServicePorts(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			EndpointSpec: &swarm.EndpointSpec{
				Ports: []swarm.PortConfig{
					{Protocol: "tcp", TargetPort: 80, PublishedPort: 9090},
				},
			},
		},
	}
	mock := &mockWriteClient{
		updateServicePortsFn: func(ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error) {
			return updated, nil
		},
	}

	h := NewHandlers(c, nil, nil, nil, mock, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`{"ports":[{"Protocol":"tcp","TargetPort":80,"PublishedPort":9090}]}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/ports", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServicePorts(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServicePorts_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest("PATCH", "/services/svc1/ports", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServicePorts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePatchServicePorts_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/ports", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServicePorts(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandle.*ServicePorts -v 2>&1 | head -10`
Expected: FAIL

- [ ] **Step 3: Implement handlers**

Add to `internal/api/write_handlers.go`:

```go
func (h *Handlers) HandleGetServicePorts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	var ports []swarm.PortConfig
	if svc.Spec.EndpointSpec != nil {
		ports = svc.Spec.EndpointSpec.Ports
	}
	if ports == nil {
		ports = []swarm.PortConfig{}
	}
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/ports", "ServicePorts", map[string]any{
		"ports": ports,
	}))
}

func (h *Handlers) HandlePatchServicePorts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "expected Content-Type: application/merge-patch+json")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var patch struct {
		Ports []swarm.PortConfig `json:"ports"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	slog.Info("updating service ports", "service", id)

	updated, err := h.writeClient.UpdateServicePorts(r.Context(), id, patch.Ports)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	var resultPorts []swarm.PortConfig
	if updated.Spec.EndpointSpec != nil {
		resultPorts = updated.Spec.EndpointSpec.Ports
	}
	if resultPorts == nil {
		resultPorts = []swarm.PortConfig{}
	}
	writeJSON(w, NewDetailResponse("/services/"+id+"/ports", "ServicePorts", map[string]any{
		"ports": resultPorts,
	}))
}
```

Note: ports PATCH decodes the full `{"ports": [...]}` wrapper and passes the array directly — since arrays can't be partially merged, this is a full replace with merge-patch Content-Type for consistency.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run TestHandle.*ServicePorts -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat(api): add ports GET + PATCH handlers"
```

---

### Task 5: Update policy handlers (GET + PATCH)

**Files:**
- Modify: `internal/api/write_handlers.go`
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/api/write_handlers_test.go`:

```go
func TestHandleGetServiceUpdatePolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			UpdateConfig: &swarm.UpdateConfig{
				Parallelism: 2,
				Order:       "start-first",
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/svc1/update-policy", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceUpdatePolicy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServiceUpdatePolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	mock := &mockWriteClient{
		updateServiceUpdatePolicyFn: func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
			return swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{UpdateConfig: policy}}, nil
		},
	}

	h := NewHandlers(c, nil, nil, nil, mock, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`{"Order":"start-first"}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/update-policy", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceUpdatePolicy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServiceUpdatePolicy_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("PATCH", "/services/svc1/update-policy", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceUpdatePolicy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePatchServiceUpdatePolicy_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("PATCH", "/services/svc1/update-policy", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceUpdatePolicy(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}
```

- [ ] **Step 2: Implement handlers**

Add to `internal/api/write_handlers.go`. These follow the `HandlePatchServiceResources` pattern exactly (marshal current → generic map → mergePatch → unmarshal back):

```go
func (h *Handlers) HandleGetServiceUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	policy := svc.Spec.UpdateConfig
	if policy == nil {
		policy = &swarm.UpdateConfig{}
	}
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/update-policy", "ServiceUpdatePolicy", map[string]any{
		"updatePolicy": policy,
	}))
}

func (h *Handlers) HandlePatchServiceUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "expected Content-Type: application/merge-patch+json")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.UpdateConfig
	if current == nil {
		current = &swarm.UpdateConfig{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current update policy")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current update policy")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged update policy")
		return
	}
	var result swarm.UpdateConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid update policy specification")
		return
	}

	slog.Info("updating service update policy", "service", id)

	updated, err := h.writeClient.UpdateServiceUpdatePolicy(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	resultPolicy := updated.Spec.UpdateConfig
	if resultPolicy == nil {
		resultPolicy = &swarm.UpdateConfig{}
	}
	writeJSON(w, NewDetailResponse("/services/"+id+"/update-policy", "ServiceUpdatePolicy", map[string]any{
		"updatePolicy": resultPolicy,
	}))
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/api/ -run TestHandle.*ServiceUpdatePolicy -v`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat(api): add update policy GET + PATCH handlers"
```

---

### Task 6: Rollback policy handlers (GET + PATCH)

**Files:**
- Modify: `internal/api/write_handlers.go`
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/api/write_handlers_test.go`:

```go
func TestHandleGetServiceRollbackPolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			RollbackConfig: &swarm.UpdateConfig{
				Parallelism: 1,
				Order:       "stop-first",
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/svc1/rollback-policy", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceRollbackPolicy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandlePatchServiceRollbackPolicy_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("PATCH", "/services/svc1/rollback-policy", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceRollbackPolicy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePatchServiceRollbackPolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	mock := &mockWriteClient{
		updateServiceRollbackPolicyFn: func(ctx context.Context, id string, policy *swarm.UpdateConfig) (swarm.Service, error) {
			return swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{RollbackConfig: policy}}, nil
		},
	}

	h := NewHandlers(c, nil, nil, nil, mock, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`{"FailureAction":"continue"}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/rollback-policy", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceRollbackPolicy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}
```

- [ ] **Step 2: Implement handlers**

Identical structure to update policy, but reads/writes `svc.Spec.RollbackConfig`, uses `@type` `ServiceRollbackPolicy`, key `rollbackPolicy`, path `/rollback-policy`, and calls `h.writeClient.UpdateServiceRollbackPolicy`.

```go
func (h *Handlers) HandleGetServiceRollbackPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	policy := svc.Spec.RollbackConfig
	if policy == nil {
		policy = &swarm.UpdateConfig{}
	}
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/rollback-policy", "ServiceRollbackPolicy", map[string]any{
		"rollbackPolicy": policy,
	}))
}

func (h *Handlers) HandlePatchServiceRollbackPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "expected Content-Type: application/merge-patch+json")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.RollbackConfig
	if current == nil {
		current = &swarm.UpdateConfig{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current rollback policy")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current rollback policy")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged rollback policy")
		return
	}
	var result swarm.UpdateConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid rollback policy specification")
		return
	}

	slog.Info("updating service rollback policy", "service", id)

	updated, err := h.writeClient.UpdateServiceRollbackPolicy(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	resultPolicy := updated.Spec.RollbackConfig
	if resultPolicy == nil {
		resultPolicy = &swarm.UpdateConfig{}
	}
	writeJSON(w, NewDetailResponse("/services/"+id+"/rollback-policy", "ServiceRollbackPolicy", map[string]any{
		"rollbackPolicy": resultPolicy,
	}))
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/api/ -run TestHandle.*ServiceRollbackPolicy -v`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat(api): add rollback policy GET + PATCH handlers"
```

---

### Task 7: Log driver handlers (GET + PATCH)

**Files:**
- Modify: `internal/api/write_handlers.go`
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/api/write_handlers_test.go`:

```go
func TestHandleGetServiceLogDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				LogDriver: &swarm.Driver{
					Name:    "json-file",
					Options: map[string]string{"max-size": "10m"},
				},
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/svc1/log-driver", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceLogDriver(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestHandleGetServiceLogDriver_Nil(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("GET", "/services/svc1/log-driver", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceLogDriver(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["logDriver"] != nil {
		t.Errorf("expected null logDriver, got %v", resp["logDriver"])
	}
}

func TestHandlePatchServiceLogDriver_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{LogDriver: &swarm.Driver{Name: "json-file"}}}})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)
	req := httptest.NewRequest("PATCH", "/services/svc1/log-driver", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceLogDriver(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandlePatchServiceLogDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				LogDriver: &swarm.Driver{
					Name:    "json-file",
					Options: map[string]string{"max-size": "10m"},
				},
			},
		},
	})

	mock := &mockWriteClient{
		updateServiceLogDriverFn: func(ctx context.Context, id string, driver *swarm.Driver) (swarm.Service, error) {
			return swarm.Service{
				ID:   "svc1",
				Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{LogDriver: driver}},
			}, nil
		},
	}

	h := NewHandlers(c, nil, nil, nil, mock, closedReady(), nil, config.OpsImpactful)
	body := strings.NewReader(`{"Options":{"max-size":"20m"}}`)
	req := httptest.NewRequest("PATCH", "/services/svc1/log-driver", body)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceLogDriver(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}
```

- [ ] **Step 2: Implement handlers**

Add to `internal/api/write_handlers.go`:

```go
func (h *Handlers) HandleGetServiceLogDriver(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/log-driver", "ServiceLogDriver", map[string]any{
		"logDriver": svc.Spec.TaskTemplate.LogDriver,
	}))
}

func (h *Handlers) HandlePatchServiceLogDriver(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "expected Content-Type: application/merge-patch+json")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.TaskTemplate.LogDriver
	if current == nil {
		current = &swarm.Driver{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current log driver")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current log driver")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged log driver")
		return
	}
	var result swarm.Driver
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid log driver specification")
		return
	}

	slog.Info("updating service log driver", "service", id)

	updated, err := h.writeClient.UpdateServiceLogDriver(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/log-driver", "ServiceLogDriver", map[string]any{
		"logDriver": updated.Spec.TaskTemplate.LogDriver,
	}))
}
```

Note: log driver GET returns `null` for `logDriver` when the field is nil (not an empty struct), since "no log driver configured" is semantically different from "empty log driver".

- [ ] **Step 3: Run tests**

Run: `go test ./internal/api/ -run TestHandle.*ServiceLogDriver -v`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat(api): add log driver GET + PATCH handlers"
```

---

### Task 8: Router registration

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add route registrations**

In `internal/api/router.go`, add after the existing service healthcheck routes (after `PATCH /services/{id}/healthcheck`):

```go
	// Service placement
	mux.HandleFunc("GET /services/{id}/placement", contentNegotiated(h.HandleGetServicePlacement, spa))
	mux.Handle("PUT /services/{id}/placement", tier1(h.HandlePutServicePlacement))

	// Service ports
	mux.HandleFunc("GET /services/{id}/ports", contentNegotiated(h.HandleGetServicePorts, spa))
	mux.Handle("PATCH /services/{id}/ports", tier1(h.HandlePatchServicePorts))

	// Service update policy
	mux.HandleFunc("GET /services/{id}/update-policy", contentNegotiated(h.HandleGetServiceUpdatePolicy, spa))
	mux.Handle("PATCH /services/{id}/update-policy", tier1(h.HandlePatchServiceUpdatePolicy))

	// Service rollback policy
	mux.HandleFunc("GET /services/{id}/rollback-policy", contentNegotiated(h.HandleGetServiceRollbackPolicy, spa))
	mux.Handle("PATCH /services/{id}/rollback-policy", tier1(h.HandlePatchServiceRollbackPolicy))

	// Service log driver
	mux.HandleFunc("GET /services/{id}/log-driver", contentNegotiated(h.HandleGetServiceLogDriver, spa))
	mux.Handle("PATCH /services/{id}/log-driver", tier1(h.HandlePatchServiceLogDriver))
```

- [ ] **Step 2: Verify compilation and run all tests**

Run: `go build ./... && go test ./... -count=1 2>&1 | tail -10`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat(api): register placement, ports, update/rollback policy, log driver routes"
```

---

### Task 9: Update documentation

**Files:**
- Modify: `internal/config/config.go` — update `OpsOperational` godoc
- Modify: `docs/configuration.md` — add new endpoints to tier 1 list
- Modify: `CLAUDE.md` — architecture section updates
- Modify: `api/openapi.yaml` — new endpoint definitions
- Modify: `CHANGELOG.md` — release notes

- [ ] **Step 1: Update `OpsOperational` godoc in `config.go`**

Change the `OpsOperational` comment to include the new sub-resources:

```go
	// OpsOperational allows routine service actions: scale, image update,
	// rollback, restart, and service env/labels/resources/healthcheck/placement/
	// ports/update-policy/rollback-policy/log-driver patches.
	OpsOperational OperationsLevel = 1
```

- [ ] **Step 2: Update `docs/configuration.md` tier 1 endpoint list**

In the "### Level 1 — Operational" section, add after the healthcheck entry:

```markdown
- **Update service placement** — `PUT /services/{id}/placement`
- **Patch service ports** — `PATCH /services/{id}/ports`
- **Patch service update policy** — `PATCH /services/{id}/update-policy`
- **Patch service rollback policy** — `PATCH /services/{id}/rollback-policy`
- **Patch service log driver** — `PATCH /services/{id}/log-driver`
```

- [ ] **Step 3: Update CLAUDE.md**

In the `api/write_handlers.go` architecture description, add the new handlers to the list. In the Key Conventions section, update the write operations summary to mention the new sub-resources.

In the `api/router.go` architecture description, add the new routes to the list of endpoints.

- [ ] **Step 4: Add new endpoints to `api/openapi.yaml`**

Add endpoint definitions for all 10 new routes (5 GET + 5 write). Follow the existing pattern for sub-resource endpoints like `/services/{id}/env` and `/services/{id}/resources`.

- [ ] **Step 5: Update `CHANGELOG.md`**

Add under `[Unreleased]` → `### Added`:

```markdown
- Service placement, ports, update/rollback policy, and log driver read and write endpoints
```

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go docs/configuration.md CLAUDE.md api/openapi.yaml CHANGELOG.md
git commit -m "docs: document service config write endpoints"
```

---

### Task 10: Final verification

- [ ] **Step 1: Run full test suite**

Run: `make test`
Expected: all PASS

- [ ] **Step 2: Check compilation**

Run: `go build ./...`
Expected: compiles cleanly

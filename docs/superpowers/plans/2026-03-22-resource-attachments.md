# Service Resource Attachments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add config, secret, and network attachment editors to the service detail page — GET/PATCH endpoints per resource type + frontend editors following the PortsEditor pattern.

**Architecture:** Backend adds 3 Docker client methods (inspect→mutate→update→re-inspect), 6 HTTP handlers (GET+PATCH per type), and extends the DockerWriteClient interface. Frontend adds 3 editor components (ConfigsEditor, SecretsEditor, NetworksEditor) using the PortsEditor draft-array pattern with combobox pickers.

**Tech Stack:** Go (Docker Engine API), React 19, TypeScript, Combobox/MultiCombobox components

**Spec:** `docs/superpowers/specs/2026-03-22-resource-attachments-design.md`

---

### Task 1: Docker Client — UpdateServiceConfigs, UpdateServiceSecrets, UpdateServiceNetworks

**Files:**
- Modify: `internal/docker/client.go` (after `UpdateServicePorts` at line 696)

- [ ] **Step 1: Add `UpdateServiceConfigs` method**

After `UpdateServicePorts`, add:

```go
func (c *Client) UpdateServiceConfigs(
	ctx context.Context,
	id string,
	configs []*swarm.ConfigReference,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}
	svc.Spec.TaskTemplate.ContainerSpec.Configs = configs
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 2: Add `UpdateServiceSecrets` method**

```go
func (c *Client) UpdateServiceSecrets(
	ctx context.Context,
	id string,
	secrets []*swarm.SecretReference,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}
	svc.Spec.TaskTemplate.ContainerSpec.Secrets = secrets
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 3: Add `UpdateServiceNetworks` method**

```go
func (c *Client) UpdateServiceNetworks(
	ctx context.Context,
	id string,
	networks []swarm.NetworkAttachmentConfig,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.Networks = networks
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/docker/`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add UpdateServiceConfigs, UpdateServiceSecrets, UpdateServiceNetworks"
```

---

### Task 2: DockerWriteClient Interface & Mock

**Files:**
- Modify: `internal/api/handlers.go` (DockerWriteClient interface)
- Modify: `internal/api/write_handlers_test.go` (mockWriteClient)

- [ ] **Step 1: Add methods to DockerWriteClient interface**

In `internal/api/handlers.go`, add to the `DockerWriteClient` interface (after the `UpdateServiceLogDriver` line):

```go
UpdateServiceConfigs(ctx context.Context, id string, configs []*swarm.ConfigReference) (swarm.Service, error)
UpdateServiceSecrets(ctx context.Context, id string, secrets []*swarm.SecretReference) (swarm.Service, error)
UpdateServiceNetworks(ctx context.Context, id string, networks []swarm.NetworkAttachmentConfig) (swarm.Service, error)
```

- [ ] **Step 2: Add fields to mockWriteClient struct**

In `internal/api/write_handlers_test.go`, add to the `mockWriteClient` struct (after `updateServiceLogDriverFn`):

```go
updateServiceConfigsFn  func(ctx context.Context, id string, configs []*swarm.ConfigReference) (swarm.Service, error)
updateServiceSecretsFn  func(ctx context.Context, id string, secrets []*swarm.SecretReference) (swarm.Service, error)
updateServiceNetworksFn func(ctx context.Context, id string, networks []swarm.NetworkAttachmentConfig) (swarm.Service, error)
```

- [ ] **Step 3: Add stub methods**

After the existing `UpdateServiceLogDriver` method on `mockWriteClient`:

```go
func (m *mockWriteClient) UpdateServiceConfigs(
	ctx context.Context,
	id string,
	configs []*swarm.ConfigReference,
) (swarm.Service, error) {
	if m.updateServiceConfigsFn != nil {
		return m.updateServiceConfigsFn(ctx, id, configs)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceSecrets(
	ctx context.Context,
	id string,
	secrets []*swarm.SecretReference,
) (swarm.Service, error) {
	if m.updateServiceSecretsFn != nil {
		return m.updateServiceSecretsFn(ctx, id, secrets)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceNetworks(
	ctx context.Context,
	id string,
	networks []swarm.NetworkAttachmentConfig,
) (swarm.Service, error) {
	if m.updateServiceNetworksFn != nil {
		return m.updateServiceNetworksFn(ctx, id, networks)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/api/`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go internal/api/write_handlers_test.go
git commit -m "feat: extend DockerWriteClient with config, secret, network update methods"
```

---

### Task 3: Backend Handlers — Configs GET/PATCH

**Files:**
- Modify: `internal/api/write_handlers.go`
- Test: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Add response struct**

In `internal/api/write_handlers.go`, add after the existing handler code:

```go
type serviceConfigRef struct {
	ConfigID   string `json:"configID"`
	ConfigName string `json:"configName"`
	FileName   string `json:"fileName"`
}
```

- [ ] **Step 2: Write tests for HandleGetServiceConfigs and HandlePatchServiceConfigs**

In `internal/api/write_handlers_test.go`, add:

```go
func TestHandleGetServiceConfigs_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{
						{
							ConfigID:   "cfg1",
							ConfigName: "app-config",
							File:       &swarm.ConfigReferenceFileTarget{Name: "/etc/app.yaml"},
						},
					},
				},
			},
		},
	})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/svc1/configs", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	configs := resp["configs"].([]any)
	if len(configs) != 1 {
		t.Fatalf("len(configs)=%d, want 1", len(configs))
	}
	cfg := configs[0].(map[string]any)
	if cfg["configID"] != "cfg1" {
		t.Errorf("configID=%v, want cfg1", cfg["configID"])
	}
	if cfg["fileName"] != "/etc/app.yaml" {
		t.Errorf("fileName=%v, want /etc/app.yaml", cfg["fileName"])
	}
}

func TestHandleGetServiceConfigs_Empty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/svc1/configs", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	configs := resp["configs"].([]any)
	if len(configs) != 0 {
		t.Errorf("len(configs)=%d, want 0", len(configs))
	}
}

func TestHandleGetServiceConfigs_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/missing/configs", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceConfigs(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceConfigs_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	updated := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{
						{
							ConfigID:   "cfg1",
							ConfigName: "app-config",
							File:       &swarm.ConfigReferenceFileTarget{Name: "/app.yaml"},
						},
					},
				},
			},
		},
	}
	mock := &mockWriteClient{
		updateServiceConfigsFn: func(_ context.Context, _ string, _ []*swarm.ConfigReference) (swarm.Service, error) {
			return updated, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, mock, closedReady(), nil, config.OpsImpactful)

	body := `{"configs":[{"configID":"cfg1","configName":"app-config","fileName":"/app.yaml"}]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceConfigs_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	body := `{"configs":[]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceConfigs(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(Get|Patch)ServiceConfigs" -v`
Expected: FAIL — methods don't exist yet

- [ ] **Step 4: Implement HandleGetServiceConfigs**

```go
func (h *Handlers) HandleGetServiceConfigs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var refs []serviceConfigRef
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		for _, cfg := range svc.Spec.TaskTemplate.ContainerSpec.Configs {
			var fileName string
			if cfg.File != nil {
				fileName = cfg.File.Name
			}
			refs = append(refs, serviceConfigRef{
				ConfigID:   cfg.ConfigID,
				ConfigName: cfg.ConfigName,
				FileName:   fileName,
			})
		}
	}
	if refs == nil {
		refs = []serviceConfigRef{}
	}

	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/configs", "ServiceConfigs", map[string]any{
		"configs": refs,
	}))
}
```

- [ ] **Step 5: Implement HandlePatchServiceConfigs**

```go
func (h *Handlers) HandlePatchServiceConfigs(w http.ResponseWriter, r *http.Request) {
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
		Configs []serviceConfigRef `json:"configs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	configs := make([]*swarm.ConfigReference, len(patch.Configs))
	for i, ref := range patch.Configs {
		if ref.ConfigID == "" || ref.ConfigName == "" {
			writeProblem(w, r, http.StatusBadRequest, "each config must have configID and configName")
			return
		}
		fileName := ref.FileName
		if fileName == "" {
			fileName = "/" + ref.ConfigName
		}
		configs[i] = &swarm.ConfigReference{
			ConfigID:   ref.ConfigID,
			ConfigName: ref.ConfigName,
			File: &swarm.ConfigReferenceFileTarget{
				Name: fileName,
				UID:  "0",
				GID:  "0",
				Mode: 0444,
			},
		}
	}

	slog.Info("updating service configs", "service", id)

	updated, err := h.writeClient.UpdateServiceConfigs(r.Context(), id, configs)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	var resultRefs []serviceConfigRef
	if updated.Spec.TaskTemplate.ContainerSpec != nil {
		for _, cfg := range updated.Spec.TaskTemplate.ContainerSpec.Configs {
			var fn string
			if cfg.File != nil {
				fn = cfg.File.Name
			}
			resultRefs = append(resultRefs, serviceConfigRef{
				ConfigID:   cfg.ConfigID,
				ConfigName: cfg.ConfigName,
				FileName:   fn,
			})
		}
	}
	if resultRefs == nil {
		resultRefs = []serviceConfigRef{}
	}
	writeJSON(w, NewDetailResponse("/services/"+id+"/configs", "ServiceConfigs", map[string]any{
		"configs": resultRefs,
	}))
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(Get|Patch)ServiceConfigs" -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add GET/PATCH handlers for service config attachments"
```

---

### Task 4: Backend Handlers — Secrets GET/PATCH

**Files:**
- Modify: `internal/api/write_handlers.go`
- Test: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Add response struct**

```go
type serviceSecretRef struct {
	SecretID   string `json:"secretID"`
	SecretName string `json:"secretName"`
	FileName   string `json:"fileName"`
}
```

- [ ] **Step 2: Write tests**

Same pattern as configs tests but for secrets: `TestHandleGetServiceSecrets_OK`, `TestHandleGetServiceSecrets_Empty`, `TestHandleGetServiceSecrets_NotFound`, `TestHandlePatchServiceSecrets_OK`, `TestHandlePatchServiceSecrets_WrongContentType`.

Use `swarm.SecretReference` with `SecretID`, `SecretName`, `File: &swarm.SecretReferenceFileTarget{Name: ...}`. The mock field is `updateServiceSecretsFn`.

- [ ] **Step 3: Implement HandleGetServiceSecrets**

Same pattern as `HandleGetServiceConfigs` but reads `ContainerSpec.Secrets`, maps to `serviceSecretRef`.

- [ ] **Step 4: Implement HandlePatchServiceSecrets**

Same pattern as `HandlePatchServiceConfigs` but:
- Decodes `serviceSecretRef` array
- Default `fileName` is `/run/secrets/<secretName>` (not `/<name>`)
- Builds `[]*swarm.SecretReference` with `&swarm.SecretReferenceFileTarget{...}`
- Calls `writeClient.UpdateServiceSecrets`

- [ ] **Step 5: Run tests and verify**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(Get|Patch)ServiceSecrets" -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add GET/PATCH handlers for service secret attachments"
```

---

### Task 5: Backend Handlers — Networks GET/PATCH

**Files:**
- Modify: `internal/api/write_handlers.go`
- Test: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Add response struct**

```go
type serviceNetworkRef struct {
	Target  string   `json:"target"`
	Aliases []string `json:"aliases,omitempty"`
}
```

- [ ] **Step 2: Write tests**

Same pattern: `TestHandleGetServiceNetworks_OK`, `TestHandleGetServiceNetworks_Empty`, `TestHandleGetServiceNetworks_NotFound`, `TestHandlePatchServiceNetworks_OK`, `TestHandlePatchServiceNetworks_WrongContentType`.

Use `swarm.NetworkAttachmentConfig{Target: "net1", Aliases: []string{"web"}}` in the service's `TaskTemplate.Networks`. The mock field is `updateServiceNetworksFn`.

- [ ] **Step 3: Implement HandleGetServiceNetworks**

Reads from `svc.Spec.TaskTemplate.Networks` (not `ContainerSpec`). Maps to `serviceNetworkRef`.

- [ ] **Step 4: Implement HandlePatchServiceNetworks**

- Decodes `serviceNetworkRef` array
- Validates each entry has `target`
- Builds `[]swarm.NetworkAttachmentConfig`
- Calls `writeClient.UpdateServiceNetworks`

- [ ] **Step 5: Run tests and verify**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(Get|Patch)ServiceNetworks" -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add GET/PATCH handlers for service network attachments"
```

---

### Task 6: Router Registration

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add route registrations**

In `internal/api/router.go`, in the tier 2 (configuration) section after the existing service sub-resource routes, add:

```go
mux.HandleFunc("GET /services/{id}/configs", contentNegotiated(h.HandleGetServiceConfigs, spa))
mux.Handle("PATCH /services/{id}/configs", tier2(h.HandlePatchServiceConfigs))
mux.HandleFunc("GET /services/{id}/secrets", contentNegotiated(h.HandleGetServiceSecrets, spa))
mux.Handle("PATCH /services/{id}/secrets", tier2(h.HandlePatchServiceSecrets))
mux.HandleFunc("GET /services/{id}/networks", contentNegotiated(h.HandleGetServiceNetworks, spa))
mux.Handle("PATCH /services/{id}/networks", tier2(h.HandlePatchServiceNetworks))
```

- [ ] **Step 2: Run full backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -v -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register config, secret, and network attachment routes"
```

---

### Task 7: Frontend Types & API Client

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add types to types.ts**

At the end of `frontend/src/api/types.ts`:

```ts
export interface ServiceConfigRef {
  configID: string;
  configName: string;
  fileName: string;
}

export interface ServiceSecretRef {
  secretID: string;
  secretName: string;
  fileName: string;
}

export interface ServiceNetworkRef {
  target: string;
  aliases?: string[];
}
```

- [ ] **Step 2: Add API methods to client.ts**

In the tier 2 sub-resource GETs section, add:

```ts
serviceConfigs: (id: string, signal?: AbortSignal) =>
  fetchJSON<{ configs: ServiceConfigRef[] }>(`/services/${id}/configs`, signal).then(
    (r) => r.configs,
  ),
serviceSecrets: (id: string, signal?: AbortSignal) =>
  fetchJSON<{ secrets: ServiceSecretRef[] }>(`/services/${id}/secrets`, signal).then(
    (r) => r.secrets,
  ),
serviceNetworks: (id: string, signal?: AbortSignal) =>
  fetchJSON<{ networks: ServiceNetworkRef[] }>(`/services/${id}/networks`, signal).then(
    (r) => r.networks,
  ),
```

In the PATCH mutations section, add:

```ts
patchServiceConfigs: (id: string, configs: ServiceConfigRef[]) =>
  patch<{ configs: ServiceConfigRef[] }>(
    `/services/${id}/configs`,
    { configs },
    "application/merge-patch+json",
  ),
patchServiceSecrets: (id: string, secrets: ServiceSecretRef[]) =>
  patch<{ secrets: ServiceSecretRef[] }>(
    `/services/${id}/secrets`,
    { secrets },
    "application/merge-patch+json",
  ),
patchServiceNetworks: (id: string, networks: ServiceNetworkRef[]) =>
  patch<{ networks: ServiceNetworkRef[] }>(
    `/services/${id}/networks`,
    { networks },
    "application/merge-patch+json",
  ),
```

Import the new types at the top of client.ts.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: add types and API methods for service config, secret, network attachments"
```

---

### Task 8: ConfigsEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/ConfigsEditor.tsx`
- Modify: `frontend/src/components/service-detail/index.ts`

- [ ] **Step 1: Create ConfigsEditor**

Follow the `PortsEditor` pattern exactly. Key structure:

- **Props**: `{ serviceId: string; configs: ServiceConfigRef[]; onSaved: (configs: ServiceConfigRef[]) => void }`
- **State**: `editing`, `saving`, `saveError`, `draft: ServiceConfigRef[]`, `newConfigId/newConfigName/newFileName`, `availableConfigs: ComboboxOption[]`
- **On edit open**: copy props to draft; fetch `api.configs({ limit: 0 })` to populate the combobox options
- **Add row**: Combobox for config selection (value = configID, label = configName). On selection, auto-fill `newFileName` with `/<configName>`. Add button pushes to draft.
- **Remove row**: filter from draft by index
- **Save**: call `api.patchServiceConfigs(serviceId, draft)`, call `onSaved(result.configs)` on success
- **Read-only mode**: `SimpleTable` with columns ["Name", "Target"]. Name links to `/configs/${configID}`. Target shows fileName in monospace.
- **Edit button**: only shown when `canEdit` (tier 2)
- Wrap in `CollapsibleSection` with title "Configs" and `defaultOpen={configs.length > 0}`

- [ ] **Step 2: Add barrel export**

In `frontend/src/components/service-detail/index.ts`, add:

```ts
export { ConfigsEditor } from "./ConfigsEditor";
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/ConfigsEditor.tsx frontend/src/components/service-detail/index.ts
git commit -m "feat: add ConfigsEditor component for service config attachments"
```

---

### Task 9: SecretsEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/SecretsEditor.tsx`
- Modify: `frontend/src/components/service-detail/index.ts`

- [ ] **Step 1: Create SecretsEditor**

Near-identical to `ConfigsEditor`. Key differences:
- Props use `ServiceSecretRef[]` (`secretID`, `secretName`, `fileName`)
- Fetches `api.secrets({ limit: 0 })` for the combobox
- Auto-fill default: `/run/secrets/<secretName>` (not `/<name>`)
- Save: `api.patchServiceSecrets(serviceId, draft)`
- Read-only: Name links to `/secrets/${secretID}`
- Title: "Secrets"

- [ ] **Step 2: Add barrel export**

```ts
export { SecretsEditor } from "./SecretsEditor";
```

- [ ] **Step 3: Verify TypeScript compiles and commit**

```bash
git add frontend/src/components/service-detail/SecretsEditor.tsx frontend/src/components/service-detail/index.ts
git commit -m "feat: add SecretsEditor component for service secret attachments"
```

---

### Task 10: NetworksEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/NetworksEditor.tsx`
- Modify: `frontend/src/components/service-detail/index.ts`

- [ ] **Step 1: Create NetworksEditor**

Same `PortsEditor` pattern with key differences:

- **Props**: `{ serviceId: string; networks: ServiceNetworkRef[]; networkNames: Record<string, string>; onSaved: (networks: ServiceNetworkRef[]) => void }`
- **Combobox**: fetches `api.networks({ limit: 0 })` on edit open. Filters out networks whose ID is already in `draft` (prevents duplicates). Options use `{ value: network.ID, label: network.Name || network.ID }`.
- **Aliases field**: `MultiCombobox` with `values={newAliases}`, `options={[]}` (free-form input — aliases are arbitrary strings). The `MultiCombobox` component degrades to a plain inline input when options is empty.
- **Add row**: Network combobox + aliases MultiCombobox + Add button
- **Read-only mode**: `SimpleTable` with columns ["Network", "Aliases"]. Network links to `/networks/${target}` with `networkNames[target] || target` as label. Aliases shows `aliases?.join(", ") || "—"`.
- **Save**: `api.patchServiceNetworks(serviceId, draft)`
- Title: "Networks"

- [ ] **Step 2: Add barrel export**

```ts
export { NetworksEditor } from "./NetworksEditor";
```

- [ ] **Step 3: Verify TypeScript compiles and commit**

```bash
git add frontend/src/components/service-detail/NetworksEditor.tsx frontend/src/components/service-detail/index.ts
git commit -m "feat: add NetworksEditor component for service network attachments"
```

---

### Task 11: Integrate Editors into ServiceDetail Page

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add imports**

Add to the `service-detail` import:

```ts
import { ConfigsEditor, SecretsEditor, NetworksEditor } from "../components/service-detail";
```

(Or add to the existing destructured import from that barrel.)

- [ ] **Step 2: Replace configs read-only section**

Find the existing configs `CollapsibleSection` (~lines 499-521) and replace with:

```tsx
<ConfigsEditor
  serviceId={serviceId}
  configs={(containerSpec?.Configs ?? []).map((cfg) => ({
    configID: cfg.ConfigID,
    configName: cfg.ConfigName,
    fileName: cfg.File?.Name ?? "",
  }))}
  onSaved={() => fetchData()}
/>
```

- [ ] **Step 3: Replace secrets read-only section**

Find the existing secrets `CollapsibleSection` (~lines 523-545) and replace with:

```tsx
<SecretsEditor
  serviceId={serviceId}
  secrets={(containerSpec?.Secrets ?? []).map((sec) => ({
    secretID: sec.SecretID,
    secretName: sec.SecretName,
    fileName: sec.File?.Name ?? "",
  }))}
  onSaved={() => fetchData()}
/>
```

- [ ] **Step 4: Replace networks read-only section**

Find the existing networks `CollapsibleSection` (~lines 469-497) and replace with:

```tsx
<NetworksEditor
  serviceId={serviceId}
  networks={(taskTemplate?.Networks ?? []).map(({ Target, Aliases }) => ({
    target: Target,
    aliases: Aliases,
  }))}
  networkNames={networkNames}
  onSaved={() => fetchData()}
/>
```

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 6: Verify lint passes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: Success

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: integrate config, secret, and network editors into service detail page"
```

---

### Task 12: Full Test Suite Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`
Expected: All PASS

- [ ] **Step 2: Run frontend type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 3: Run frontend lint and format check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint && npm run fmt:check`
Expected: Success (or run `npm run fmt` to fix)

- [ ] **Step 4: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass

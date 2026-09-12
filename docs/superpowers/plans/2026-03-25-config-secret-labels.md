# Config & Secret Label Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add label editing to config and secret detail pages, cloning the existing node/service label editing pattern.

**Architecture:** Backend adds `GET/PATCH /configs/{id}/labels` and `GET/PATCH /secrets/{id}/labels` endpoints (tier 2) wrapping Docker SDK's `ConfigUpdate`/`SecretUpdate`. Frontend replaces read-only `LabelSection` with `KeyValueEditor` on detail pages.

**Tech Stack:** Go (net/http, Docker SDK), React 19, TypeScript, shadcn/ui

**Spec:** `docs/superpowers/specs/2026-03-25-config-secret-labels-design.md`

---

### Task 1: Add error codes and error helpers

**Files:**
- Modify: `internal/api/errors.go` (after CFG004/SEC004)
- Modify: `internal/api/write_handlers.go` (after `writeNodeError`, around line 76)

- [ ] **Step 1: Add CFG005 and SEC005 error codes**

In `errors.go`, after the CFG004 entry add:

```go
"CFG005": {
    Code:        "CFG005",
    Title:       "Config Version Conflict",
    Status:      http.StatusConflict,
    Description: "The config was modified concurrently.",
    Suggestion:  "Retry the operation with the latest version.",
},
```

After the SEC004 entry add:

```go
"SEC005": {
    Code:        "SEC005",
    Title:       "Secret Version Conflict",
    Status:      http.StatusConflict,
    Description: "The secret was modified concurrently.",
    Suggestion:  "Retry the operation with the latest version.",
},
```

- [ ] **Step 2: Add writeConfigError and writeSecretError helpers**

In `write_handlers.go`, after `writeNodeError` (around line 76), add:

```go
// writeConfigError handles Docker API errors for config mutations,
// mapping version conflicts to CFG005.
func writeConfigError(w http.ResponseWriter, r *http.Request, err error) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "CFG005", err.Error())
		return
	}
	writeDockerError(w, r, err, "config")
}

// writeSecretError handles Docker API errors for secret mutations,
// mapping version conflicts to SEC005.
func writeSecretError(w http.ResponseWriter, r *http.Request, err error) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "SEC005", err.Error())
		return
	}
	writeDockerError(w, r, err, "secret")
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/api/`

- [ ] **Step 4: Commit**

```bash
git add internal/api/errors.go internal/api/write_handlers.go
git commit -m "feat: add config/secret version conflict error codes and helpers"
```

---

### Task 2: Add Docker client UpdateConfigLabels and UpdateSecretLabels

**Files:**
- Modify: `internal/docker/client.go` (after `CreateSecret`, around line 600)

- [ ] **Step 1: Add both methods**

```go
func (c *Client) UpdateConfigLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Config, error) {
	cfg, _, err := c.docker.ConfigInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Config{}, err
	}
	cfg.Spec.Labels = labels
	err = c.docker.ConfigUpdate(ctx, cfg.ID, cfg.Version, cfg.Spec)
	if err != nil {
		return swarm.Config{}, err
	}
	return c.InspectConfig(ctx, id)
}

func (c *Client) UpdateSecretLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Secret, error) {
	sec, _, err := c.docker.SecretInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Secret{}, err
	}
	sec.Spec.Labels = labels
	err = c.docker.SecretUpdate(ctx, sec.ID, sec.Version, sec.Spec)
	if err != nil {
		return swarm.Secret{}, err
	}
	return c.InspectSecret(ctx, id)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/docker/`

- [ ] **Step 3: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add UpdateConfigLabels and UpdateSecretLabels to Docker client"
```

---

### Task 3: Add DockerWriteClient interface methods and mock

**Files:**
- Modify: `internal/api/handlers.go` (DockerWriteClient interface, after `CreateSecret`)
- Modify: `internal/api/write_handlers_test.go` (mockWriteClient struct + methods)

- [ ] **Step 1: Add to DockerWriteClient interface**

After `CreateSecret`:

```go
UpdateConfigLabels(ctx context.Context, id string, labels map[string]string) (swarm.Config, error)
UpdateSecretLabels(ctx context.Context, id string, labels map[string]string) (swarm.Secret, error)
```

- [ ] **Step 2: Add mock fields and methods**

Add to `mockWriteClient` struct:

```go
updateConfigLabelsFn func(ctx context.Context, id string, labels map[string]string) (swarm.Config, error)
updateSecretLabelsFn func(ctx context.Context, id string, labels map[string]string) (swarm.Secret, error)
```

Add mock method implementations (after `CreateSecret` mock):

```go
func (m *mockWriteClient) UpdateConfigLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Config, error) {
	if m.updateConfigLabelsFn != nil {
		return m.updateConfigLabelsFn(ctx, id, labels)
	}
	return swarm.Config{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateSecretLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Secret, error) {
	if m.updateSecretLabelsFn != nil {
		return m.updateSecretLabelsFn(ctx, id, labels)
	}
	return swarm.Secret{}, fmt.Errorf("not implemented")
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/api/`

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add UpdateConfigLabels/UpdateSecretLabels to interface and mock"
```

---

### Task 4: Write tests for config label handlers

**Files:**
- Modify: `internal/api/write_handlers_test.go` (append after the last create test)

- [ ] **Step 1: Write tests**

```go
func TestHandleGetConfigLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "my-config",
				Labels: map[string]string{"env": "prod"},
			},
		},
	})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, nil, closedReady(), nil, config.OpsConfiguration)

	req := httptest.NewRequest("GET", "/configs/cfg1/labels", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleGetConfigLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ConfigLabels" {
		t.Errorf("@type=%v, want ConfigLabels", resp["@type"])
	}
	labels, ok := resp["labels"].(map[string]any)
	if !ok {
		t.Fatal("expected labels key in response")
	}
	if labels["env"] != "prod" {
		t.Errorf("env=%v, want prod", labels["env"])
	}
}

func TestHandleGetConfigLabels_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		&mockWriteClient{},
		nil,
		closedReady(),
		nil,
		config.OpsConfiguration,
	)

	req := httptest.NewRequest("GET", "/configs/missing/labels", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetConfigLabels(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchConfigLabels_JSONPatch(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "my-config",
				Labels: map[string]string{"existing": "value"},
			},
		},
	})

	wc := &mockWriteClient{
		updateConfigLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Config, error) {
			return swarm.Config{
				ID: id,
				Spec: swarm.ConfigSpec{
					Annotations: swarm.Annotations{Labels: labels},
				},
			}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchConfigLabels_MergePatch(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "my-config",
				Labels: map[string]string{"existing": "value", "remove": "me"},
			},
		},
	})

	wc := &mockWriteClient{
		updateConfigLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Config, error) {
			return swarm.Config{
				ID: id,
				Spec: swarm.ConfigSpec{
					Annotations: swarm.Annotations{Labels: labels},
				},
			}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	body := `{"new":"label","remove":null}`
	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchConfigLabels_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "cfg1"})
	h := NewHandlers(
		c,
		nil,
		nil,
		nil,
		&mockWriteClient{},
		nil,
		closedReady(),
		nil,
		config.OpsConfiguration,
	)

	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchConfigLabels_VersionConflict(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}},
	})

	wc := &mockWriteClient{
		updateConfigLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Config, error) {
			return swarm.Config{}, errdefs.Conflict(fmt.Errorf("version conflict"))
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/configs/cfg1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandlePatchConfigLabels(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestHandle.*ConfigLabels" -v -count=1`
Expected: FAIL — handlers don't exist yet

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_handlers_test.go
git commit -m "test: add tests for config label handlers"
```

---

### Task 5: Write tests for secret label handlers

**Files:**
- Modify: `internal/api/write_handlers_test.go` (append after config label tests)

- [ ] **Step 1: Write tests**

```go
func TestHandleGetSecretLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   "my-secret",
				Labels: map[string]string{"env": "prod"},
			},
		},
	})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, nil, closedReady(), nil, config.OpsConfiguration)

	req := httptest.NewRequest("GET", "/secrets/sec1/labels", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleGetSecretLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "SecretLabels" {
		t.Errorf("@type=%v, want SecretLabels", resp["@type"])
	}
}

func TestHandlePatchSecretLabels_JSONPatch(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   "my-secret",
				Labels: map[string]string{"existing": "value"},
			},
		},
	})

	wc := &mockWriteClient{
		updateSecretLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Secret, error) {
			return swarm.Secret{
				ID: id,
				Spec: swarm.SecretSpec{
					Annotations: swarm.Annotations{Labels: labels},
				},
			}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/secrets/sec1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandlePatchSecretLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchSecretLabels_VersionConflict(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}},
	})

	wc := &mockWriteClient{
		updateSecretLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Secret, error) {
			return swarm.Secret{}, errdefs.Conflict(fmt.Errorf("version conflict"))
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/secrets/sec1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandlePatchSecretLabels(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestHandle.*SecretLabels" -v -count=1`
Expected: FAIL

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_handlers_test.go
git commit -m "test: add tests for secret label handlers"
```

---

### Task 6: Implement label handlers

**Files:**
- Modify: `internal/api/write_handlers.go` (after HandleCreateSecret, around line 751)

- [ ] **Step 1: Add four handlers**

```go
func (h *Handlers) HandleGetConfigLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", "config not found")
		return
	}
	labels := cfg.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSONWithETag(w, r, NewDetailResponse("/configs/"+id+"/labels", "ConfigLabels", map[string]any{
		"labels": labels,
	}))
}

func (h *Handlers) HandlePatchConfigLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	isJSONPatch := strings.HasPrefix(ct, "application/json-patch+json")
	isMergePatch := strings.HasPrefix(ct, "application/merge-patch+json")

	if !isJSONPatch && !isMergePatch {
		writeErrorCode(
			w,
			r,
			"API004",
			"Content-Type must be application/json-patch+json or application/merge-patch+json",
		)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", "config not found")
		return
	}

	current := cfg.Spec.Labels
	if current == nil {
		current = map[string]string{}
	}

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeErrorCode(w, r, "API006", "invalid request body")
			return
		}
		updated, err = applyJSONPatch(current, ops)
	} else {
		updated, err = applyMergePatchStringMap(current, body)
	}

	if err != nil {
		writePatchError(w, r, err)
		return
	}

	slog.Info("patching config labels", "config", id)

	result, err := h.writeClient.UpdateConfigLabels(r.Context(), id, updated)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSON(w, labels)
}

func (h *Handlers) HandleGetSecretLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", "secret not found")
		return
	}
	labels := sec.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSONWithETag(w, r, NewDetailResponse("/secrets/"+id+"/labels", "SecretLabels", map[string]any{
		"labels": labels,
	}))
}

func (h *Handlers) HandlePatchSecretLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	isJSONPatch := strings.HasPrefix(ct, "application/json-patch+json")
	isMergePatch := strings.HasPrefix(ct, "application/merge-patch+json")

	if !isJSONPatch && !isMergePatch {
		writeErrorCode(
			w,
			r,
			"API004",
			"Content-Type must be application/json-patch+json or application/merge-patch+json",
		)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}

	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", "secret not found")
		return
	}

	current := sec.Spec.Labels
	if current == nil {
		current = map[string]string{}
	}

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeErrorCode(w, r, "API006", "invalid request body")
			return
		}
		updated, err = applyJSONPatch(current, ops)
	} else {
		updated, err = applyMergePatchStringMap(current, body)
	}

	if err != nil {
		writePatchError(w, r, err)
		return
	}

	slog.Info("patching secret labels", "secret", id)

	result, err := h.writeClient.UpdateSecretLabels(r.Context(), id, updated)
	if err != nil {
		writeSecretError(w, r, err)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSON(w, labels)
}
```

- [ ] **Step 2: Run all label tests**

Run: `go test ./internal/api/ -run "TestHandle.*(Config|Secret)Labels" -v -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_handlers.go
git commit -m "feat: implement config and secret label GET/PATCH handlers"
```

---

### Task 7: Register routes

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add routes**

After `POST /configs` (around line 261), add:

```go
mux.HandleFunc("GET /configs/{id}/labels", contentNegotiated(h.HandleGetConfigLabels, spa))
mux.Handle("PATCH /configs/{id}/labels", tier2(h.HandlePatchConfigLabels))
```

After `POST /secrets` (around line 282), add:

```go
mux.HandleFunc("GET /secrets/{id}/labels", contentNegotiated(h.HandleGetSecretLabels, spa))
mux.Handle("PATCH /secrets/{id}/labels", tier2(h.HandlePatchSecretLabels))
```

- [ ] **Step 2: Run full API test suite**

Run: `go test ./internal/api/ -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register config/secret label GET/PATCH routes at tier 2"
```

---

### Task 8: Add frontend API client methods

**Files:**
- Modify: `frontend/src/api/client.ts` (after `createSecret`)

- [ ] **Step 1: Add methods**

After the existing `createSecret` line, add:

```typescript
patchConfigLabels: (id: string, ops: PatchOp[]) =>
    patch<Record<string, string>>(`/configs/${id}/labels`, ops, "application/json-patch+json"),
patchSecretLabels: (id: string, ops: PatchOp[]) =>
    patch<Record<string, string>>(`/secrets/${id}/labels`, ops, "application/json-patch+json"),
```

Verify `PatchOp` is already imported/defined in the file. Check near `patchNodeLabels` usage.

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: add patchConfigLabels and patchSecretLabels API client methods"
```

---

### Task 9: Replace LabelSection with KeyValueEditor on ConfigDetail

**Files:**
- Modify: `frontend/src/pages/ConfigDetail.tsx`

- [ ] **Step 1: Update imports and add state**

Replace the `LabelSection` import with `KeyValueEditor`:

Change:
```tsx
import {
  LabelSection,
  MetadataGrid,
```
to:
```tsx
import {
  MetadataGrid,
```

Add imports:
```tsx
import { KeyValueEditor } from "../components/KeyValueEditor";
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
```

Add state inside the component, after the `useDetailResource` call:
```tsx
const [configLabels, setConfigLabels] = useState<Record<string, string> | null>(null);
const { level, loading: levelLoading } = useOperationsLevel();
```

You'll need to add `useState` to the React import. Also add an effect or derive labels from `data` when it changes. The simplest approach: derive from `data` directly:

```tsx
const rawLabels = data?.config?.Spec?.Labels ?? {};
```

But for the `KeyValueEditor` `onSave` to update labels without a full refetch, use state initialized from the response. Check how `NodeDetail` handles this (it sets `nodeLabels` from the initial fetch and SSE updates). Since `ConfigDetail` uses `useDetailResource`, you can derive labels from `data` and use a separate state for the editor:

After `const { entries: labelEntries, stack } = parseStackLabels(config.Spec.Labels);` add:

```tsx
const allLabels = config.Spec.Labels ?? {};
```

- [ ] **Step 2: Replace LabelSection with KeyValueEditor**

Replace:
```tsx
<LabelSection entries={labelEntries} />
```

with:
```tsx
<KeyValueEditor
    title="Labels"
    entries={allLabels}
    defaultOpen={Object.keys(allLabels).length > 0}
    keyPlaceholder="com.example.my-label"
    valuePlaceholder="value"
    editDisabled={levelLoading || level < opsLevel.configuration}
    isKeyReadOnly={isReservedLabelKey}
    validateKey={validateLabelKey}
    onSave={async (ops) => {
        const updated = await api.patchConfigLabels(config.ID, ops);
        return updated;
    }}
/>
```

Note: Since `ConfigDetail` uses `useDetailResource` (which refetches on SSE change events), the labels will update automatically from the SSE event triggered by the Docker watcher after the PATCH succeeds. The `onSave` return value is used by `KeyValueEditor` to update its internal state immediately.

- [ ] **Step 3: Remove unused LabelSection import if no longer used**

Check if `LabelSection` is still imported. If the only usage was the one replaced, remove it from the import.

- [ ] **Step 4: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/ConfigDetail.tsx
git commit -m "feat: replace read-only LabelSection with KeyValueEditor on ConfigDetail"
```

---

### Task 10: Replace LabelSection with KeyValueEditor on SecretDetail

**Files:**
- Modify: `frontend/src/pages/SecretDetail.tsx`

- [ ] **Step 1: Same changes as ConfigDetail**

Apply the identical pattern:
- Replace `LabelSection` import with `KeyValueEditor`, `useOperationsLevel`, `isReservedLabelKey`, `validateLabelKey`
- Derive `allLabels` from `secret.Spec.Labels ?? {}`
- Replace `<LabelSection entries={labelEntries} />` with `<KeyValueEditor>` calling `api.patchSecretLabels`

```tsx
<KeyValueEditor
    title="Labels"
    entries={allLabels}
    defaultOpen={Object.keys(allLabels).length > 0}
    keyPlaceholder="com.example.my-label"
    valuePlaceholder="value"
    editDisabled={levelLoading || level < opsLevel.configuration}
    isKeyReadOnly={isReservedLabelKey}
    validateKey={validateLabelKey}
    onSave={async (ops) => {
        const updated = await api.patchSecretLabels(secret.ID, ops);
        return updated;
    }}
/>
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/SecretDetail.tsx
git commit -m "feat: replace read-only LabelSection with KeyValueEditor on SecretDetail"
```

---

### Task 11: Update docs and changelog

**Files:**
- Modify: `api/openapi.yaml` — add GET/PATCH /configs/{id}/labels and /secrets/{id}/labels
- Modify: `docs/api.md` — document new endpoints
- Modify: `CHANGELOG.md` — add entry

- [ ] **Step 1: Add OpenAPI spec entries**

Under `/configs/{id}:` path section, add a new path `/configs/{id}/labels:` with `get:` and `patch:` operations. Follow the existing `/nodes/{id}/labels:` and `/services/{id}/labels:` pattern exactly.

Do the same for `/secrets/{id}/labels:`.

- [ ] **Step 2: Update docs/api.md**

Add `GET /configs/{id}/labels` and `PATCH /configs/{id}/labels` to the Configs section. Same for secrets. Follow existing format.

- [ ] **Step 3: Update CHANGELOG.md**

Under `[Unreleased]`, add:

```markdown
- Add label editing for configs and secrets
```

- [ ] **Step 4: Commit**

```bash
git add api/openapi.yaml docs/api.md CHANGELOG.md
git commit -m "docs: add config/secret label endpoints to OpenAPI spec, API docs, and changelog"
```

---

### Task 12: Full verification

- [ ] **Step 1: Run all backend tests**

Run: `go test ./... -count=1`
Expected: all PASS

- [ ] **Step 2: Run frontend type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: success

- [ ] **Step 3: Run frontend lint**

Run: `cd frontend && npm run lint`
Expected: no errors

- [ ] **Step 4: Run format check**

Run: `make fmt-check`
Expected: no formatting issues

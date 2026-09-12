# Service Mounts Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the read-only mounts SimpleTable on the service detail page with a card-based mounts editor supporting all Docker mount types.

**Architecture:** Backend adds `UpdateServiceMounts` to Docker client + GET/PATCH handlers following the configs/secrets/networks pattern. Frontend adds a `MountsEditor` component following the PortsEditor card-based pattern with conditional type-specific fields.

**Tech Stack:** Go (Docker Engine API, `mount.Mount`), React 19, TypeScript, Tailwind CSS

**Spec:** `docs/superpowers/specs/2026-03-23-service-mounts-editor-design.md`

---

### Task 1: Docker Client — UpdateServiceMounts

**Files:**
- Modify: `internal/docker/client.go`

- [ ] **Step 1: Add `mount` import and `UpdateServiceMounts` method**

Add `"github.com/docker/docker/api/types/mount"` to the import block. Then add after `UpdateServiceNetworks`:

```go
func (c *Client) UpdateServiceMounts(
	ctx context.Context,
	id string,
	mounts []mount.Mount,
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}
	svc.Spec.TaskTemplate.ContainerSpec.Mounts = mounts
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/docker/`

- [ ] **Step 3: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add UpdateServiceMounts to Docker client"
```

---

### Task 2: DockerWriteClient Interface & Mock

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Add `mount` import and interface method to handlers.go**

Add `"github.com/docker/docker/api/types/mount"` to the import block in `handlers.go`. Add to the `DockerWriteClient` interface after `UpdateServiceNetworks`:

```go
UpdateServiceMounts(ctx context.Context, id string, mounts []mount.Mount) (swarm.Service, error)
```

- [ ] **Step 2: Add mock field and stub method**

In `write_handlers_test.go`, add `"github.com/docker/docker/api/types/mount"` to imports. Add to `mockWriteClient` struct:

```go
updateServiceMountsFn func(ctx context.Context, id string, mounts []mount.Mount) (swarm.Service, error)
```

Add stub method:

```go
func (m *mockWriteClient) UpdateServiceMounts(
	ctx context.Context,
	id string,
	mounts []mount.Mount,
) (swarm.Service, error) {
	if m.updateServiceMountsFn != nil {
		return m.updateServiceMountsFn(ctx, id, mounts)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/api/`

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers.go internal/api/write_handlers_test.go
git commit -m "feat: extend DockerWriteClient with UpdateServiceMounts"
```

---

### Task 3: Backend Handlers + Tests

**Files:**
- Modify: `internal/api/write_handlers.go`
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write tests**

Add `"github.com/docker/docker/api/types/mount"` to the test file imports if not already present. Add at the end of the test file:

```go
func TestHandleGetServiceMounts_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Mounts: []mount.Mount{
						{Type: mount.TypeVolume, Source: "data", Target: "/data"},
					},
				},
			},
		},
	})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/svc1/mounts", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceMounts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	mounts, ok := resp["mounts"].([]any)
	if !ok || len(mounts) != 1 {
		t.Fatalf("mounts=%v, want 1 mount", resp["mounts"])
	}
}

func TestHandleGetServiceMounts_Empty(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{},
			},
		},
	})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/svc1/mounts", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceMounts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	mounts, ok := resp["mounts"].([]any)
	if !ok || len(mounts) != 0 {
		t.Fatalf("mounts=%v, want empty array", resp["mounts"])
	}
}

func TestHandleGetServiceMounts_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/missing/mounts", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceMounts(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceMounts_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})

	wc := &mockWriteClient{
		updateServiceMountsFn: func(_ context.Context, _ string, mounts []mount.Mount) (swarm.Service, error) {
			return swarm.Service{
				ID: "svc1",
				Spec: swarm.ServiceSpec{
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: &swarm.ContainerSpec{Mounts: mounts},
					},
				},
			}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	body := `{"mounts":[{"Type":"volume","Source":"data","Target":"/data"}]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/mounts", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceMounts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceMounts_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	body := `{"mounts":[]}`
	req := httptest.NewRequest("PATCH", "/services/missing/mounts", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceMounts(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceMounts_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	body := `{"mounts":[]}`
	req := httptest.NewRequest("PATCH", "/services/svc1/mounts", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceMounts(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(Get|Patch)ServiceMounts" -v`
Expected: FAIL — methods don't exist yet

- [ ] **Step 3: Implement handlers**

Add `"github.com/docker/docker/api/types/mount"` to the import block in `write_handlers.go`. Add after the last handler:

```go
func (h *Handlers) HandleGetServiceMounts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var mounts []mount.Mount
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		mounts = svc.Spec.TaskTemplate.ContainerSpec.Mounts
	}
	if mounts == nil {
		mounts = []mount.Mount{}
	}

	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/mounts", "ServiceMounts", map[string]any{
		"mounts": mounts,
	}))
}

func (h *Handlers) HandlePatchServiceMounts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/merge-patch+json")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var req struct {
		Mounts []mount.Mount `json:"mounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	slog.Info("updating service mounts", "service", id, "count", len(req.Mounts))

	updated, err := h.writeClient.UpdateServiceMounts(r.Context(), id, req.Mounts)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	var mounts []mount.Mount
	if updated.Spec.TaskTemplate.ContainerSpec != nil {
		mounts = updated.Spec.TaskTemplate.ContainerSpec.Mounts
	}
	if mounts == nil {
		mounts = []mount.Mount{}
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/mounts", "ServiceMounts", map[string]any{
		"mounts": mounts,
	}))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(Get|Patch)ServiceMounts" -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add handlers for service mounts GET and PATCH"
```

---

### Task 4: Router Registration

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add route registrations**

After the `PATCH /services/{id}/networks` line (around line 161), add:

```go
mux.HandleFunc("GET /services/{id}/mounts", contentNegotiated(h.HandleGetServiceMounts, spa))
mux.Handle("PATCH /services/{id}/mounts", tier2(h.HandlePatchServiceMounts))
```

- [ ] **Step 2: Run full backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register service mounts routes"
```

---

### Task 5: Frontend Types & API Client

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add `ServiceMount` type**

In `frontend/src/api/types.ts`, add a named `ServiceMount` interface (near the other `Service*Ref` types):

```ts
export interface ServiceMount {
  Type: string;
  Source: string;
  Target: string;
  ReadOnly?: boolean;
  BindOptions?: {
    Propagation?: string;
    NonRecursive?: boolean;
    CreateMountpoint?: boolean;
  };
  VolumeOptions?: {
    NoCopy?: boolean;
    Labels?: Record<string, string>;
    Subpath?: string;
  };
  TmpfsOptions?: {
    SizeBytes?: number;
    Mode?: number;
  };
  ImageOptions?: {
    Subpath?: string;
  };
  ClusterOptions?: Record<string, unknown>;
}
```

Then update the `ContainerSpec.Mounts` inline type to use it:

```ts
Mounts?: ServiceMount[];
```

- [ ] **Step 2: Add API client methods**

In `frontend/src/api/client.ts`, add the GET method in the tier 2 sub-resource GETs section:

```ts
serviceMounts: (id: string, signal?: AbortSignal) =>
  fetchJSON<{ mounts: ServiceMount[] }>(`/services/${id}/mounts`, signal).then(
    (r) => r.mounts ?? [],
  ),
```

Add the PATCH method in the tier 2 mutations section:

```ts
patchServiceMounts: (id: string, mounts: ServiceMount[]) =>
  patch<{ mounts: ServiceMount[] }>(
    `/services/${id}/mounts`,
    { mounts },
    "application/merge-patch+json",
  ),
```

Add `ServiceMount` to the import from `../api/types` if types are imported.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: add ServiceMount type and API client methods"
```

---

### Task 6: MountsEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/MountsEditor.tsx`

This is the largest task. The component follows the PortsEditor pattern: `CollapsibleSection` with edit/read-only toggle, card grid in edit mode, compact chips in read-only mode.

- [ ] **Step 1: Create MountsEditor**

Create `frontend/src/components/service-detail/MountsEditor.tsx`. The component should:

**Structure:**
- Props: `serviceId: string`, `mounts: ServiceMount[]`, `onSaved: (mounts: ServiceMount[]) => void`
- State: `editing`, `saving`, `saveError`, `draft: ServiceMount[]`
- Uses `useEscapeCancel`, `useOperationsLevel` gated at `opsLevel.configuration`

**Read-only view** (when `!editing`):
- Compact chips in a `flex flex-wrap gap-2` container inside a bordered box (same as PortsEditor)
- Each mount: `<MountTypeBadge type={m.Type} />` + source (monospace) + arrow + target (monospace) + optional "(ro)" suffix
- Volume source links to `/volumes/{name}`
- tmpfs shows only target (no source)
- Empty state: "No mounts configured" with edit hint

**Edit mode** (when `editing`):
- Responsive card grid: `grid-cols-1 md:grid-cols-2 lg:grid-cols-3`
- Each card: bordered container with remove button (X) top-right
- Card fields, top to bottom:
  - **Type**: `<select>` with options: bind, volume, tmpfs, npipe, cluster, image
  - **Source**: text input (hidden for tmpfs). Label changes by type: "Host path" (bind), "Volume name" (volume), "Pipe name" (npipe), "CSI volume" (cluster), "Image" (image)
  - **Target**: text input (always shown), label "Container path"
  - **Read-only**: checkbox
  - Conditional type-specific fields:
    - bind: Propagation `<select>` (private, rprivate, shared, rshared, slave, rslave)
    - volume: No-copy checkbox, Subpath text input
    - tmpfs: Size number input (bytes), Mode number input (octal)
    - image: Subpath text input
- Changing type: resets type-specific options (`BindOptions`, `VolumeOptions`, `TmpfsOptions`, `ImageOptions`, `ClusterOptions` all set to `undefined`), clears `Source` when switching to tmpfs
- Default new mount: `{ Type: "volume", Source: "", Target: "", ReadOnly: false }`
- Empty edit: dashed "Add a mount" button (same as PortsEditor)
- Footer: "Add mount" button, Save (with spinner), Cancel

**`MountTypeBadge`** — inline helper or extracted from ServiceDetail.tsx. Extends colors: bind=amber, volume=blue, tmpfs=purple, npipe=slate, cluster=teal, image=indigo.

**Save:** calls `api.patchServiceMounts(serviceId, draft)`, on success calls `onSaved(result.mounts)`.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/MountsEditor.tsx
git commit -m "feat: add MountsEditor component with card-based UI"
```

---

### Task 7: Integrate into ServiceDetail Page

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add import and state**

Import `MountsEditor` from `../components/service-detail/MountsEditor` (or from the barrel if one exists). Add `ServiceMount` to type imports.

Add state:

```ts
const [serviceMounts, setServiceMounts] = useState<ServiceMount[] | null>(null);
```

- [ ] **Step 2: Add fetch in fetchData**

Add alongside other parallel fetches:

```ts
api
  .serviceMounts(id, signal)
  .then(setServiceMounts)
  .catch(() => {});
```

- [ ] **Step 3: Replace read-only mounts section with MountsEditor**

Replace the current mounts block (the `{/* Mounts */}` section with `CollapsibleSection` + `SimpleTable`, around lines 437–470) with:

```tsx
{serviceMounts !== null && (
  <MountsEditor
    serviceId={id!}
    mounts={serviceMounts}
    onSaved={setServiceMounts}
  />
)}
```

- [ ] **Step 4: Remove `MountTypeBadge` from ServiceDetail.tsx**

Delete the `MountTypeBadge` function (lines 681–690) since it's now in `MountsEditor.tsx`. Also remove the `SimpleTable` import if it's no longer used elsewhere in the file. Check before removing — search for other `SimpleTable` usages in the same file.

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 6: Verify lint passes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: integrate MountsEditor into service detail page"
```

---

### Task 8: Full Test Suite Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`
Expected: All PASS

- [ ] **Step 2: Run frontend type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Run frontend lint and format**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint && npm run fmt:check`
Expected: Success (or run `npm run fmt` to fix)

- [ ] **Step 4: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass

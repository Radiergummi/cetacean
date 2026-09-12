# Resource Removal for Configs, Secrets, Networks, Volumes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add DELETE endpoints and UI remove buttons for configs, secrets, networks, and volumes.

**Architecture:** Replicate the existing `HandleRemoveTask`/`HandleRemoveService` pattern (cache lookup → Docker client call → 204) four times. Add `RemoveVolume` to the Docker client (only missing method). Frontend: add a shared `RemoveResourceAction` component used by all four detail pages.

**Tech Stack:** Go, React, TypeScript

---

### Task 1: Add `RemoveVolume` to Docker client and interface

**Files:**
- Modify: `internal/api/handlers.go:62-100` (DockerWriteClient interface)
- Modify: `internal/docker/client.go:578` (after RemoveSecret)
- Modify: `internal/api/write_handlers_test.go:24-53` (mockWriteClient struct + method)

- [ ] **Step 1: Add `RemoveVolume` to the `DockerWriteClient` interface**

In `internal/api/handlers.go`, add after line 80 (`RemoveSecret`):

```go
RemoveVolume(ctx context.Context, name string) error
```

- [ ] **Step 2: Add `RemoveVolume` to `docker/client.go`**

After `RemoveSecret` (line 580):

```go
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	return c.docker.VolumeRemove(ctx, name, false)
}
```

- [ ] **Step 3: Add `removeVolumeFn` to mock and method**

In `internal/api/write_handlers_test.go`, add field to `mockWriteClient` struct:

```go
removeVolumeFn func(ctx context.Context, name string) error
```

And add the mock method:

```go
func (m *mockWriteClient) RemoveVolume(ctx context.Context, name string) error {
	if m.removeVolumeFn != nil {
		return m.removeVolumeFn(ctx, name)
	}
	return fmt.Errorf("not implemented")
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go internal/docker/client.go internal/api/write_handlers_test.go
git commit -m "feat: add RemoveVolume to Docker client and interface"
```

---

### Task 2: Add backend handlers and routes for all four resources

**Files:**
- Modify: `internal/api/write_handlers.go:537` (after HandleRemoveStack)
- Modify: `internal/api/router.go:255,273,291,309` (after each resource's GET routes)

- [ ] **Step 1: Add four `HandleRemove*` handlers**

Add after `HandleRemoveStack` in `write_handlers.go`. All follow the identical pattern — cache lookup, log, Docker client call, 204:

```go
func (h *Handlers) HandleRemoveConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetConfig(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "config not found")
		return
	}

	slog.Info("removing config", "config", id)

	err := h.writeClient.RemoveConfig(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "config")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemoveSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetSecret(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "secret not found")
		return
	}

	slog.Info("removing secret", "secret", id)

	err := h.writeClient.RemoveSecret(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "secret")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetNetwork(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "network not found")
		return
	}

	slog.Info("removing network", "network", id)

	err := h.writeClient.RemoveNetwork(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "network")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemoveVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	_, ok := h.cache.GetVolume(name)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "volume not found")
		return
	}

	slog.Info("removing volume", "volume", name)

	err := h.writeClient.RemoveVolume(r.Context(), name)
	if err != nil {
		writeDockerError(w, r, err, "volume")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Register DELETE routes in `router.go`**

Add after each resource section's existing GET routes, all at tier 3:

After configs section (after line 255):
```go
mux.Handle("DELETE /configs/{id}", tier3(h.HandleRemoveConfig))
```

After secrets section (after line 273):
```go
mux.Handle("DELETE /secrets/{id}", tier3(h.HandleRemoveSecret))
```

After networks section (after line 291):
```go
mux.Handle("DELETE /networks/{id}", tier3(h.HandleRemoveNetwork))
```

After volumes section (after line 309):
```go
mux.Handle("DELETE /volumes/{name}", tier3(h.HandleRemoveVolume))
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/api/write_handlers.go internal/api/router.go
git commit -m "feat: add DELETE endpoints for configs, secrets, networks, and volumes"
```

---

### Task 3: Add backend tests

**Files:**
- Modify: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write tests for all four handlers**

Add tests following the exact `TestHandleRemoveService_*` pattern (OK, NotFound, DockerError variants):

```go
func TestHandleRemoveConfig_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "cfg1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}}})
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleRemoveConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleRemoveConfig_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/configs/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleRemoveConfig_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "cfg1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}}})
	wc := &mockWriteClient{
		removeConfigFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("docker error")
		},
	}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleRemoveConfig(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandleRemoveSecret_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{ID: "sec1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}}})
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleRemoveSecret(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleRemoveSecret_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/secrets/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveSecret(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleRemoveSecret_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{ID: "sec1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}}})
	wc := &mockWriteClient{
		removeSecretFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("docker error")
		},
	}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleRemoveSecret(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandleRemoveNetwork_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "my-network"})
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/networks/net1", nil)
	req.SetPathValue("id", "net1")
	w := httptest.NewRecorder()
	h.HandleRemoveNetwork(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleRemoveNetwork_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/networks/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveNetwork(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleRemoveNetwork_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "my-network"})
	wc := &mockWriteClient{
		removeNetworkFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("docker error")
		},
	}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/networks/net1", nil)
	req.SetPathValue("id", "net1")
	w := httptest.NewRecorder()
	h.HandleRemoveNetwork(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandleRemoveVolume_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "my-vol"})
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/volumes/my-vol", nil)
	req.SetPathValue("name", "my-vol")
	w := httptest.NewRecorder()
	h.HandleRemoveVolume(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleRemoveVolume_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/volumes/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveVolume(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleRemoveVolume_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "my-vol"})
	wc := &mockWriteClient{
		removeVolumeFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("docker error")
		},
	}
	h := &Handlers{cache: c, writeClient: wc}

	req := httptest.NewRequest(http.MethodDelete, "/volumes/my-vol", nil)
	req.SetPathValue("name", "my-vol")
	w := httptest.NewRecorder()
	h.HandleRemoveVolume(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleRemove(Config|Secret|Network|Volume)" -v`
Expected: All 12 tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_handlers_test.go
git commit -m "test: add tests for config, secret, network, and volume removal handlers"
```

---

### Task 4: Add frontend API methods and `RemoveResourceAction` component

**Files:**
- Modify: `frontend/src/api/client.ts:346` (after removeStack)
- Create: `frontend/src/components/RemoveResourceAction.tsx`

- [ ] **Step 1: Add API methods to `client.ts`**

After the existing `removeStack` method:

```typescript
removeConfig: (id: string) => del(`/configs/${id}`),
removeSecret: (id: string) => del(`/secrets/${id}`),
removeNetwork: (id: string) => del(`/networks/${id}`),
removeVolume: (name: string) => del(`/volumes/${name}`),
```

- [ ] **Step 2: Create `RemoveResourceAction` component**

This is a simple shared component for resources with no special preconditions (unlike nodes which must be "down", or services which have rollback/restart). Follows the `ConfirmAction` pattern from `ServiceActions.tsx`:

```tsx
import { Spinner } from "@/components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";

interface RemoveResourceActionProps {
  resourceType: string;
  resourceName: string;
  listPath: string;
  onRemove: () => Promise<void>;
}

export function RemoveResourceAction({
  resourceType,
  resourceName,
  listPath,
  onRemove,
}: RemoveResourceActionProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canImpact = !levelLoading && level >= opsLevel.impactful;
  const navigate = useNavigate();
  const remove = useAsyncAction();

  if (!canImpact) {
    return null;
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <AlertDialog>
        <AlertDialogTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              disabled={remove.loading}
            >
              {remove.loading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
              Remove
            </Button>
          }
        />
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove {resourceType}?</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently remove{" "}
              <strong className="text-foreground">{resourceName}</strong>. This action cannot be
              undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() =>
                void remove.execute(async () => {
                  await onRemove();
                  navigate(listPath, { replace: true });
                }, `Failed to remove ${resourceType.toLowerCase()}`)
              }
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {remove.error && <p className="text-xs text-red-600 dark:text-red-400">{remove.error}</p>}
    </div>
  );
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/components/RemoveResourceAction.tsx
git commit -m "feat: add frontend remove API methods and shared RemoveResourceAction component"
```

---

### Task 5: Add remove actions to all four detail pages

**Files:**
- Modify: `frontend/src/pages/ConfigDetail.tsx`
- Modify: `frontend/src/pages/SecretDetail.tsx`
- Modify: `frontend/src/pages/NetworkDetail.tsx`
- Modify: `frontend/src/pages/VolumeDetail.tsx`

- [ ] **Step 1: Add remove action to `ConfigDetail.tsx`**

Add import:
```tsx
import { RemoveResourceAction } from "../components/RemoveResourceAction";
```

Add `actions` prop to the `PageHeader`:
```tsx
actions={
  <RemoveResourceAction
    resourceType="config"
    resourceName={name}
    listPath={stack ? `/stacks/${stack}` : "/configs"}
    onRemove={() => api.removeConfig(config.ID)}
  />
}
```

- [ ] **Step 2: Add remove action to `SecretDetail.tsx`**

Same pattern — add import, add `actions` prop to `PageHeader`:
```tsx
actions={
  <RemoveResourceAction
    resourceType="secret"
    resourceName={name}
    listPath={stack ? `/stacks/${stack}` : "/secrets"}
    onRemove={() => api.removeSecret(secret.ID)}
  />
}
```

- [ ] **Step 3: Add remove action to `NetworkDetail.tsx`**

```tsx
actions={
  <RemoveResourceAction
    resourceType="network"
    resourceName={network.Name}
    listPath={stack ? `/stacks/${stack}` : "/networks"}
    onRemove={() => api.removeNetwork(network.Id)}
  />
}
```

- [ ] **Step 4: Add remove action to `VolumeDetail.tsx`**

```tsx
actions={
  <RemoveResourceAction
    resourceType="volume"
    resourceName={volume.Name}
    listPath={stack ? `/stacks/${stack}` : "/volumes"}
    onRemove={() => api.removeVolume(volume.Name)}
  />
}
```

- [ ] **Step 5: Verify TypeScript compiles and lint passes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npm run lint`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/ConfigDetail.tsx frontend/src/pages/SecretDetail.tsx frontend/src/pages/NetworkDetail.tsx frontend/src/pages/VolumeDetail.tsx
git commit -m "feat: add remove actions to config, secret, network, and volume detail pages"
```

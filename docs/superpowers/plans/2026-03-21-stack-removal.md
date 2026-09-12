# Stack Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `DELETE /stacks/{name}` endpoint that removes all services, networks, configs, and secrets in a stack (matching `docker stack rm`), with a type-to-confirm dialog on the stack detail page.

**Architecture:** Backend adds three new Docker client removal methods (network, config, secret), a `HandleRemoveStack` handler that orchestrates best-effort removal of all stack resources, and a new route. Frontend adds a `StackActions` component with type-to-confirm dialog, integrated into the stack detail page via the `PageHeader` actions prop.

**Tech Stack:** Go (Docker Engine API), React 19, TypeScript, shadcn/ui (AlertDialog, Button, Input)

**Spec:** `docs/superpowers/specs/2026-03-21-stack-removal-design.md`

---

### Task 1: Docker Client — RemoveNetwork, RemoveConfig, RemoveSecret

**Files:**
- Modify: `internal/docker/client.go` (after `RemoveService` at line 462)

- [ ] **Step 1: Add three removal methods**

```go
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.docker.NetworkRemove(ctx, id)
}

func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	return c.docker.ConfigRemove(ctx, id)
}

func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	return c.docker.SecretRemove(ctx, id)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/docker/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add RemoveNetwork, RemoveConfig, and RemoveSecret to Docker client"
```

---

### Task 2: DockerWriteClient Interface & Mock

**Files:**
- Modify: `internal/api/handlers.go` (DockerWriteClient interface)
- Modify: `internal/api/write_handlers_test.go` (mockWriteClient struct + methods)

- [ ] **Step 1: Add methods to DockerWriteClient interface**

In `internal/api/handlers.go`, add to the `DockerWriteClient` interface (after the `RemoveNode` line):

```go
RemoveNetwork(ctx context.Context, id string) error
RemoveConfig(ctx context.Context, id string) error
RemoveSecret(ctx context.Context, id string) error
```

- [ ] **Step 2: Add fields to mockWriteClient struct**

In `internal/api/write_handlers_test.go`, add to the `mockWriteClient` struct (after `removeNodeFn`):

```go
removeNetworkFn func(ctx context.Context, id string) error
removeConfigFn  func(ctx context.Context, id string) error
removeSecretFn  func(ctx context.Context, id string) error
```

- [ ] **Step 3: Add stub methods to mockWriteClient**

```go
func (m *mockWriteClient) RemoveNetwork(ctx context.Context, id string) error {
	if m.removeNetworkFn != nil {
		return m.removeNetworkFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockWriteClient) RemoveConfig(ctx context.Context, id string) error {
	if m.removeConfigFn != nil {
		return m.removeConfigFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockWriteClient) RemoveSecret(ctx context.Context, id string) error {
	if m.removeSecretFn != nil {
		return m.removeSecretFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/api/`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go internal/api/write_handlers_test.go
git commit -m "feat: extend DockerWriteClient with RemoveNetwork, RemoveConfig, RemoveSecret"
```

---

### Task 3: HandleRemoveStack Handler + Tests

**Files:**
- Modify: `internal/api/write_handlers.go` (add handler)
- Test: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write tests**

To seed a stack in the cache, add resources with the `com.docker.stack.namespace` label. The cache auto-builds the stack from labelled resources.

In `internal/api/write_handlers_test.go`, add:

```go
func seedStack(c *cache.Cache, name string) {
	label := map[string]string{"com.docker.stack.namespace": name}
	c.SetService(swarm.Service{
		ID:   name + "_svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name + "_svc1", Labels: label}},
	})
	c.SetService(swarm.Service{
		ID:   name + "_svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name + "_svc2", Labels: label}},
	})
	c.SetNetwork(network.Summary{ID: name + "_net1", Name: name + "_net1", Labels: label})
	c.SetConfig(swarm.Config{
		ID:   name + "_cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name + "_cfg1", Labels: label}},
	})
	c.SetSecret(swarm.Secret{
		ID:   name + "_sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name + "_sec1", Labels: label}},
	})
}

func TestHandleRemoveStack_OK(t *testing.T) {
	c := cache.New(nil)
	seedStack(c, "myapp")

	wc := &mockWriteClient{
		removeServiceFn: func(_ context.Context, _ string) error { return nil },
		removeNetworkFn: func(_ context.Context, _ string) error { return nil },
		removeConfigFn:  func(_ context.Context, _ string) error { return nil },
		removeSecretFn:  func(_ context.Context, _ string) error { return nil },
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/stacks/myapp", nil)
	req.SetPathValue("name", "myapp")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	removed := resp["removed"].(map[string]any)
	if removed["services"] != float64(2) {
		t.Errorf("services=%v, want 2", removed["services"])
	}
	if removed["networks"] != float64(1) {
		t.Errorf("networks=%v, want 1", removed["networks"])
	}
	if resp["errors"] != nil {
		t.Errorf("errors=%v, want nil", resp["errors"])
	}
}

func TestHandleRemoveStack_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/stacks/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveStack_PartialFailure(t *testing.T) {
	c := cache.New(nil)
	seedStack(c, "myapp")

	wc := &mockWriteClient{
		removeServiceFn: func(_ context.Context, _ string) error { return nil },
		removeNetworkFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("network is in use")
		},
		removeConfigFn: func(_ context.Context, _ string) error { return nil },
		removeSecretFn: func(_ context.Context, _ string) error { return nil },
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/stacks/myapp", nil)
	req.SetPathValue("name", "myapp")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["errors"] == nil {
		t.Fatal("expected errors array")
	}
	errs := resp["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("errors length=%d, want 1", len(errs))
	}
}

func TestHandleRemoveStack_AlreadyGone(t *testing.T) {
	c := cache.New(nil)
	seedStack(c, "myapp")

	wc := &mockWriteClient{
		removeServiceFn: func(_ context.Context, _ string) error {
			return errdefs.NotFound(fmt.Errorf("not found"))
		},
		removeNetworkFn: func(_ context.Context, _ string) error {
			return errdefs.NotFound(fmt.Errorf("not found"))
		},
		removeConfigFn: func(_ context.Context, _ string) error {
			return errdefs.NotFound(fmt.Errorf("not found"))
		},
		removeSecretFn: func(_ context.Context, _ string) error {
			return errdefs.NotFound(fmt.Errorf("not found"))
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/stacks/myapp", nil)
	req.SetPathValue("name", "myapp")
	w := httptest.NewRecorder()
	h.HandleRemoveStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	removed := resp["removed"].(map[string]any)
	if removed["services"] != float64(0) {
		t.Errorf("services=%v, want 0 (all were already gone)", removed["services"])
	}
	if resp["errors"] != nil {
		t.Errorf("errors=%v, want nil (404s are skipped)", resp["errors"])
	}
}
```

Note: The test file already imports `"github.com/docker/docker/errdefs"` (Docker errdefs, used to construct typed errors in mocks) and `"github.com/docker/docker/api/types/network"` may need to be added if not already present. Check imports before adding.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleRemoveStack" -v`
Expected: FAIL — `HandleRemoveStack` doesn't exist yet

- [ ] **Step 3: Implement HandleRemoveStack**

In `internal/api/write_handlers.go`, add after `HandleGetNodeRole`:

```go
type removeError struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Error string `json:"error"`
}

type removeStackResponse struct {
	Removed struct {
		Services int `json:"services"`
		Networks int `json:"networks"`
		Configs  int `json:"configs"`
		Secrets  int `json:"secrets"`
	} `json:"removed"`
	Errors []removeError `json:"errors,omitempty"`
}

func (h *Handlers) HandleRemoveStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	stack, ok := h.cache.GetStack(name)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "stack not found")
		return
	}

	slog.Info("removing stack", "stack", name,
		"services", len(stack.Services),
		"networks", len(stack.Networks),
		"configs", len(stack.Configs),
		"secrets", len(stack.Secrets),
	)

	ctx := r.Context()
	var resp removeStackResponse
	var errs []removeError

	for _, id := range stack.Services {
		if err := h.writeClient.RemoveService(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "service", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Services++
	}

	for _, id := range stack.Networks {
		if err := h.writeClient.RemoveNetwork(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "network", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Networks++
	}

	for _, id := range stack.Secrets {
		if err := h.writeClient.RemoveSecret(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "secret", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Secrets++
	}

	for _, id := range stack.Configs {
		if err := h.writeClient.RemoveConfig(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "config", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Configs++
	}

	if len(errs) > 0 {
		resp.Errors = errs
	}

	writeJSON(w, resp)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleRemoveStack" -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add HandleRemoveStack with best-effort multi-resource removal"
```

---

### Task 4: Router Registration

**Files:**
- Modify: `internal/api/router.go` (stacks section, around line 200)

- [ ] **Step 1: Add route registration**

In `internal/api/router.go`, after the `GET /stacks/{name}` block, add:

```go
mux.Handle("DELETE /stacks/{name}", tier3(h.HandleRemoveStack))
```

- [ ] **Step 2: Run full backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register DELETE /stacks/{name} route"
```

---

### Task 5: Frontend API Client

**Files:**
- Modify: `frontend/src/api/client.ts` (after `removeNode` around line 304)

- [ ] **Step 1: Add removeStack method**

After the `removeNode` line in the `api` object, add:

```ts
removeStack: (name: string) =>
  mutationFetch<{
    removed: { services: number; networks: number; configs: number; secrets: number };
    errors?: { type: string; id: string; error: string }[];
  }>(`/stacks/${name}`, "DELETE"),
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: add removeStack API method"
```

---

### Task 6: StackActions Component

**Files:**
- Create: `frontend/src/components/stack-detail/StackActions.tsx`

- [ ] **Step 1: Check if `frontend/src/components/stack-detail/` directory exists**

Run: `ls frontend/src/components/stack-detail/ 2>/dev/null || echo "does not exist"`

If it doesn't exist, the component will be the first file in this directory.

- [ ] **Step 2: Create StackActions component**

Create `frontend/src/components/stack-detail/StackActions.tsx`:

```tsx
import { api } from "@/api/client";
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
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

interface StackActionsProps {
  stackName: string;
  resourceCounts: {
    services: number;
    networks: number;
    configs: number;
    secrets: number;
  };
}

export function StackActions({ stackName, resourceCounts }: StackActionsProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canImpact = !levelLoading && level >= opsLevel.impactful;
  const navigate = useNavigate();
  const remove = useAsyncAction();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");
  const [partialErrors, setPartialErrors] = useState<
    { type: string; id: string; error: string }[] | null
  >(null);

  if (!canImpact) {
    return null;
  }

  const canRemove = confirmText === stackName;

  function handleOpenChange(next: boolean) {
    setDialogOpen(next);

    if (!next) {
      setConfirmText("");
      setPartialErrors(null);
    }
  }

  const total =
    resourceCounts.services +
    resourceCounts.networks +
    resourceCounts.configs +
    resourceCounts.secrets;

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="border-red-500/50 text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/20"
        disabled={remove.loading}
        onClick={() => setDialogOpen(true)}
      >
        {remove.loading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
        Remove
      </Button>

      {remove.error && (
        <p className="text-xs text-red-600 dark:text-red-400">{remove.error}</p>
      )}

      <AlertDialog
        open={dialogOpen}
        onOpenChange={handleOpenChange}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove stack?</AlertDialogTitle>
            <AlertDialogDescription>
              This will remove all services, networks, configs, and secrets in the{" "}
              <strong className="text-foreground">{stackName}</strong> stack. Volumes will not
              be removed.
            </AlertDialogDescription>
          </AlertDialogHeader>

          {total > 0 && (
            <p className="text-sm text-muted-foreground">
              This stack contains {resourceCounts.services} services,{" "}
              {resourceCounts.networks} networks, {resourceCounts.configs} configs,{" "}
              {resourceCounts.secrets} secrets.
            </p>
          )}

          <div className="flex flex-col gap-1.5">
            <label className="text-sm text-muted-foreground">
              Type <strong className="text-foreground">{stackName}</strong> to confirm
            </label>
            <Input
              value={confirmText}
              onChange={(event) => setConfirmText(event.target.value)}
              placeholder={stackName}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
            />
          </div>

          {partialErrors && (
            <div className="rounded-md border border-yellow-500/25 bg-yellow-500/5 px-3 py-2 text-xs leading-relaxed text-yellow-600 dark:text-yellow-500">
              <p className="mb-1 font-medium">Some resources could not be removed:</p>
              <ul className="list-inside list-disc">
                {partialErrors.map(({ type, id, error }, index) => (
                  <li key={index}>
                    {type} {id}: {error}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={!canRemove || remove.loading}
              onClick={() =>
                void remove.execute(async () => {
                  const result = await api.removeStack(stackName);

                  if (result.errors && result.errors.length > 0) {
                    setPartialErrors(result.errors);
                    return;
                  }

                  navigate("/stacks", { replace: true });
                }, "Failed to remove stack")
              }
            >
              {remove.loading ? "Removing…" : "Remove"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/stack-detail/StackActions.tsx
git commit -m "feat: add StackActions component with type-to-confirm removal dialog"
```

---

### Task 7: Integrate into StackDetail Page

**Files:**
- Modify: `frontend/src/pages/StackDetail.tsx`

- [ ] **Step 1: Add import**

Add at the top of `frontend/src/pages/StackDetail.tsx`:

```ts
import { StackActions } from "../components/stack-detail/StackActions";
```

- [ ] **Step 2: Add StackActions to PageHeader**

Find the `<PageHeader>` usage (around line 100) and add the `actions` prop:

```tsx
<PageHeader
  title={stack.name}
  subtitle={subtitle}
  breadcrumbs={[{ label: "Stacks", to: "/stacks" }, { label: stack.name }]}
  actions={
    <StackActions
      stackName={stack.name}
      resourceCounts={{
        services: stack.services?.length ?? 0,
        networks: stack.networks?.length ?? 0,
        configs: stack.configs?.length ?? 0,
        secrets: stack.secrets?.length ?? 0,
      }}
    />
  }
/>
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 4: Verify lint passes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/StackDetail.tsx
git commit -m "feat: integrate StackActions into stack detail page"
```

---

### Task 8: Full Test Suite Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`
Expected: All PASS

- [ ] **Step 2: Run frontend type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 3: Run frontend lint and format**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint && npm run fmt:check`
Expected: Success (run `npm run fmt` first if format check fails)

- [ ] **Step 4: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass

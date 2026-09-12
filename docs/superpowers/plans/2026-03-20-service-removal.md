# Service Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `DELETE /services/{id}` endpoint and a "Remove" button on the service detail page.

**Architecture:** New Docker client method → interface addition → handler (mirrors `HandleRemoveTask`) → route at tier 2 → frontend API method + UI button with post-deletion navigation.

**Tech Stack:** Go stdlib net/http, Docker Engine SDK, React 19, react-router-dom, lucide-react, shadcn/ui AlertDialog.

---

### Task 1: Backend — Docker client + interface + handler + route

**Files:**
- Modify: `internal/docker/client.go` (add `RemoveService` method after `RemoveTask` ~line 390)
- Modify: `internal/api/handlers.go` (add `RemoveService` to `DockerWriteClient` interface ~line 55)
- Modify: `internal/api/write_handlers.go` (add `HandleRemoveService` handler after `HandleRemoveTask` ~line 308)
- Modify: `internal/api/router.go` (add DELETE route ~line 106)

- [ ] **Step 1: Add `RemoveService` to `DockerWriteClient` interface**

In `internal/api/handlers.go`, add after `RemoveTask(ctx context.Context, id string) error` (~line 65):

```go
RemoveService(ctx context.Context, id string) error
```

- [ ] **Step 2: Add `RemoveService` to Docker client**

In `internal/docker/client.go`, add after the `RemoveTask` method (~line 390):

```go
func (c *Client) RemoveService(ctx context.Context, id string) error {
	return c.docker.ServiceRemove(ctx, id)
}
```

- [ ] **Step 3: Add `HandleRemoveService` handler**

In `internal/api/write_handlers.go`, add after `HandleRemoveTask` (~line 308):

```go
func (h *Handlers) HandleRemoveService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("removing service", "service", id)

	err := h.writeClient.RemoveService(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Add route**

In `internal/api/router.go`, add after the `DELETE /tasks/{id}` route (~line 106):

```go
mux.Handle("DELETE /services/{id}", tier2(h.HandleRemoveService))
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./...`
Expected: compiles (will fail until mock is added in Task 2, so this step verifies the non-test code compiles)

### Task 2: Backend tests + mock

**Files:**
- Modify: `internal/api/write_handlers_test.go` (add mock method + 3 tests)

- [ ] **Step 1: Add mock field and method**

In `internal/api/write_handlers_test.go`, add field to `mockWriteClient` struct (~line 27, after `removeTaskFn`):

```go
removeServiceFn func(ctx context.Context, id string) error
```

Add method (after `UpdateServiceLogDriver` mock method):

```go
func (m *mockWriteClient) RemoveService(ctx context.Context, id string) error {
	if m.removeServiceFn != nil {
		return m.removeServiceFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}
```

- [ ] **Step 2: Write test for success case**

Add after the `TestHandleRemoveTask_NoContainer` test (~line 734):

```go
func TestHandleRemoveService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		removeServiceFn: func(_ context.Context, id string) error {
			return nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRemoveService(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 3: Write test for not-found case**

```go
func TestHandleRemoveService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/services/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}
```

- [ ] **Step 4: Write test for Docker error case**

```go
func TestHandleRemoveService_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		removeServiceFn: func(_ context.Context, id string) error {
			return fmt.Errorf("engine error")
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRemoveService(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/api/ -run TestHandleRemoveService -v`
Expected: 3 PASS

- [ ] **Step 6: Commit backend**

```bash
git add internal/docker/client.go internal/api/handlers.go internal/api/write_handlers.go internal/api/write_handlers_test.go internal/api/router.go
git commit -m "feat: add DELETE /services/{id} endpoint for service removal"
```

### Task 3: Frontend — API client + UI button

**Files:**
- Modify: `frontend/src/api/client.ts` (add `removeService` method ~line 300, near `removeTask`)
- Modify: `frontend/src/components/service-detail/ServiceActions.tsx` (add Remove button + navigation)

- [ ] **Step 1: Add `removeService` to API client**

In `frontend/src/api/client.ts`, add after `removeTask`:

```ts
removeService: (id: string) => del(`/services/${id}`),
```

- [ ] **Step 2: Add Remove button to ServiceActions**

In `frontend/src/components/service-detail/ServiceActions.tsx`:

Add imports:

```ts
import { Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
```

Update the component to accept `service` labels for stack navigation, add `useNavigate`, a new `useAsyncAction` for remove, and tier 2 gating:

```tsx
export function ServiceActions({ service, serviceId }: { service: Service; serviceId: string }) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canWrite = !levelLoading && level >= 1;
  const canImpact = !levelLoading && level >= 2;
  const navigate = useNavigate();

  const rollback = useAsyncAction();
  const restart = useAsyncAction();
  const remove = useAsyncAction();

  const canRollback = canWrite && !!service.PreviousSpec;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* ... existing Rollback and Restart buttons unchanged ... */}

      <ConfirmAction
        icon={Trash2}
        label="Remove"
        title="Remove service?"
        description="This will permanently remove the service and all its tasks. This action cannot be undone."
        disabled={!canImpact}
        disabledTitle="Editing disabled by server configuration"
        loading={remove.loading}
        error={remove.error}
        variant="destructive"
        onConfirm={() =>
          void remove.execute(async () => {
            await api.removeService(serviceId);
            const stackName = service.Spec?.Labels?.["com.docker.stack.namespace"];
            navigate(stackName ? `/stacks/${stackName}` : "/services", { replace: true });
          }, "Failed to remove service")
        }
      />
    </div>
  );
}
```

- [ ] **Step 3: Add `variant` prop to ConfirmAction for destructive styling**

The `ConfirmAction` component needs a `variant` prop to pass through to the `AlertDialogAction` button so the Remove button's confirm action renders as destructive (red). Add `variant?: "default" | "destructive"` to the props, defaulting to `"default"`:

```tsx
function ConfirmAction({
  icon: Icon,
  label,
  title,
  description,
  disabled,
  disabledTitle,
  loading,
  error,
  variant = "default",
  onConfirm,
}: {
  icon: LucideIcon;
  label: string;
  title: string;
  description: string;
  disabled?: boolean;
  disabledTitle?: string;
  loading: boolean;
  error: string | null;
  variant?: "default" | "destructive";
  onConfirm: () => void;
}) {
```

Then on `AlertDialogAction`, pass the variant:

```tsx
<AlertDialogAction variant={variant} onClick={onConfirm}>{label}</AlertDialogAction>
```

- [ ] **Step 4: Verify frontend builds**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`
Expected: no errors

- [ ] **Step 5: Commit frontend**

```bash
git add frontend/src/api/client.ts frontend/src/components/service-detail/ServiceActions.tsx
git commit -m "feat(frontend): add service removal button with post-deletion navigation"
```

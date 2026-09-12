# Healthcheck Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the read-only healthcheck KVTable with an interactive editor widget showing stat cards in display mode and inline inputs in edit mode, backed by new GET/PUT/PATCH endpoints.

**Architecture:** Backend adds three endpoints following existing sub-resource patterns (GET with content negotiation, PUT for full replacement, PATCH with RFC 7396 merge patch). Frontend adds a HealthcheckEditor component with display/edit modes and a quote-aware command parser utility. The healthcheck type uses `*container.HealthConfig` from the Docker SDK.

**Tech Stack:** Go (net/http handlers, Docker SDK), React 19 + TypeScript, Tailwind CSS v4

**Spec:** `docs/superpowers/specs/2026-03-20-healthcheck-editor-design.md`

---

### Task 1: Backend — Write Client Method

**Files:**
- Modify: `internal/api/handlers.go` (add method to `DockerWriteClient` interface)
- Modify: `internal/docker/client.go` (implement the method)

- [ ] **Step 1: Add `UpdateServiceHealthcheck` to `DockerWriteClient` interface**

In `internal/api/handlers.go`, add to the interface (after `UpdateServiceEndpointMode`):

```go
UpdateServiceHealthcheck(
    ctx context.Context,
    id string,
    hc *container.HealthConfig,
) (swarm.Service, error)
```

Add `"github.com/docker/docker/api/types/container"` to imports in `handlers.go` if not present.

- [ ] **Step 2: Implement in `docker/client.go`**

```go
func (c *Client) UpdateServiceHealthcheck(ctx context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.ContainerSpec.Healthcheck = hc
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`

- [ ] **Step 4: Commit**

```
feat(docker): add UpdateServiceHealthcheck write client method
```

---

### Task 2: Backend — GET, PUT, PATCH Handlers + Tests

**Files:**
- Modify: `internal/api/write_handlers.go` (add three handlers)
- Modify: `internal/api/write_handlers_test.go` (add tests)
- Modify: `internal/api/router.go` (register routes)

- [ ] **Step 1: Write tests for GET handler**

In `write_handlers_test.go`, add mock method to `mockWriteClient`:

```go
updateServiceHealthcheckFn func(ctx context.Context, id string, hc *container.HealthConfig) (swarm.Service, error)
```

And the interface implementation:

```go
func (m *mockWriteClient) UpdateServiceHealthcheck(ctx context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
	return m.updateServiceHealthcheckFn(ctx, id, hc)
}
```

Add test:

```go
func TestHandleGetServiceHealthcheck(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	svc.Spec.TaskTemplate.ContainerSpec.Healthcheck = &container.HealthConfig{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost/"},
		Interval: 30_000_000_000,
		Timeout:  10_000_000_000,
		Retries:  3,
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/services/svc1/healthcheck", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceHealthcheck" {
		t.Errorf("@type=%v, want ServiceHealthcheck", resp["@type"])
	}
}

func TestHandleGetServiceHealthcheck_Nil(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/services/svc1/healthcheck", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}
```

- [ ] **Step 2: Write tests for PUT handler**

```go
func TestHandlePutServiceHealthcheck(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	c.SetService(svc)

	wc := &mockWriteClient{
		updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
			s := replicatedService(id)
			s.Spec.TaskTemplate.ContainerSpec.Healthcheck = hc
			return s, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"Test":["CMD-SHELL","curl -f http://localhost/"],"Interval":30000000000,"Timeout":10000000000,"Retries":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePutServiceHealthcheck_Disable(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	c.SetService(svc)

	var captured *container.HealthConfig
	wc := &mockWriteClient{
		updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
			captured = hc
			s := replicatedService(id)
			s.Spec.TaskTemplate.ContainerSpec.Healthcheck = hc
			return s, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"Test":["NONE"]}`
	req := httptest.NewRequest("PUT", "/services/svc1/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	if captured == nil || len(captured.Test) != 1 || captured.Test[0] != "NONE" {
		t.Errorf("expected NONE test, got %v", captured)
	}
}
```

- [ ] **Step 3: Write test for PATCH handler**

```go
func TestHandlePatchServiceHealthcheck_Merge(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	svc.Spec.TaskTemplate.ContainerSpec.Healthcheck = &container.HealthConfig{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost/"},
		Interval: 30_000_000_000,
		Timeout:  10_000_000_000,
		Retries:  3,
	}
	c.SetService(svc)

	var captured *container.HealthConfig
	wc := &mockWriteClient{
		updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
			captured = hc
			s := replicatedService(id)
			s.Spec.TaskTemplate.ContainerSpec.Healthcheck = hc
			return s, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"Timeout":5000000000}`
	req := httptest.NewRequest("PATCH", "/services/svc1/healthcheck", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	if captured.Timeout != 5_000_000_000 {
		t.Errorf("timeout=%d, want 5000000000", captured.Timeout)
	}
	if len(captured.Test) != 2 || captured.Test[0] != "CMD-SHELL" {
		t.Errorf("test should be preserved, got %v", captured.Test)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandleGetServiceHealthcheck -v`
Run: `go test ./internal/api/ -run TestHandlePutServiceHealthcheck -v`
Run: `go test ./internal/api/ -run TestHandlePatchServiceHealthcheck -v`

- [ ] **Step 5: Implement GET handler**

In `write_handlers.go`:

```go
func (h *Handlers) HandleGetServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	hc := svc.Spec.TaskTemplate.ContainerSpec.Healthcheck
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/healthcheck", "ServiceHealthcheck", map[string]any{
		"healthcheck": hc,
	}))
}
```

- [ ] **Step 6: Implement PUT handler**

```go
func (h *Handlers) HandlePutServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var hc container.HealthConfig
	if err := json.NewDecoder(r.Body).Decode(&hc); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("updating service healthcheck", "service", id)

	updated, err := h.writeClient.UpdateServiceHealthcheck(r.Context(), id, &hc)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/healthcheck", "ServiceHealthcheck", map[string]any{
		"healthcheck": updated.Spec.TaskTemplate.ContainerSpec.Healthcheck,
	}))
}
```

- [ ] **Step 7: Implement PATCH handler**

```go
func (h *Handlers) HandlePatchServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
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

	current := svc.Spec.TaskTemplate.ContainerSpec.Healthcheck
	if current == nil {
		current = &container.HealthConfig{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current healthcheck")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current healthcheck")
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
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged healthcheck")
		return
	}
	var result container.HealthConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid healthcheck specification")
		return
	}

	slog.Info("updating service healthcheck", "service", id)

	updated, err := h.writeClient.UpdateServiceHealthcheck(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/healthcheck", "ServiceHealthcheck", map[string]any{
		"healthcheck": updated.Spec.TaskTemplate.ContainerSpec.Healthcheck,
	}))
}
```

- [ ] **Step 8: Register routes in router.go**

After the existing service sub-resource routes:

```go
mux.HandleFunc("GET /services/{id}/healthcheck", contentNegotiated(h.HandleGetServiceHealthcheck, spa))
mux.Handle("PUT /services/{id}/healthcheck", requireWrite(h.HandlePutServiceHealthcheck))
mux.Handle("PATCH /services/{id}/healthcheck", requireWrite(h.HandlePatchServiceHealthcheck))
```

- [ ] **Step 9: Add `container` import to `write_handlers.go` if not present**

```go
"github.com/docker/docker/api/types/container"
```

- [ ] **Step 10: Run all tests**

Run: `go test ./internal/api/ -v`
Expected: All new and existing tests pass.

- [ ] **Step 11: Commit**

```
feat(api): add GET/PUT/PATCH /services/{id}/healthcheck endpoints
```

---

### Task 3: Frontend — parseCommand utility + tests

**Files:**
- Create: `frontend/src/lib/parseCommand.ts`
- Create: `frontend/src/lib/parseCommand.test.ts`

- [ ] **Step 1: Write tests**

```typescript
import { parseCommand, joinCommand } from "./parseCommand";
import { describe, it, expect } from "vitest";

describe("parseCommand", () => {
  it("splits simple command", () => {
    expect(parseCommand("curl -f http://localhost/")).toEqual([
      "curl",
      "-f",
      "http://localhost/",
    ]);
  });

  it("handles double quotes", () => {
    expect(parseCommand('/bin/sh -c "echo hello world"')).toEqual([
      "/bin/sh",
      "-c",
      "echo hello world",
    ]);
  });

  it("handles single quotes", () => {
    expect(parseCommand("echo 'hello world'")).toEqual(["echo", "hello world"]);
  });

  it("handles empty string", () => {
    expect(parseCommand("")).toEqual([]);
  });

  it("handles whitespace-only", () => {
    expect(parseCommand("   ")).toEqual([]);
  });

  it("preserves escaped quotes", () => {
    expect(parseCommand('echo "say \\"hello\\""')).toEqual([
      "echo",
      'say "hello"',
    ]);
  });

  it("handles mixed quotes", () => {
    expect(parseCommand(`cmd --flag="value" --other='val'`)).toEqual([
      "cmd",
      "--flag=value",
      "--other=val",
    ]);
  });
});

describe("joinCommand", () => {
  it("joins simple args", () => {
    expect(joinCommand(["curl", "-f", "http://localhost/"])).toBe(
      "curl -f http://localhost/",
    );
  });

  it("quotes args with spaces", () => {
    expect(joinCommand(["/bin/sh", "-c", "echo hello world"])).toBe(
      '/bin/sh -c "echo hello world"',
    );
  });

  it("returns empty string for empty array", () => {
    expect(joinCommand([])).toBe("");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/parseCommand.test.ts`

- [ ] **Step 3: Implement**

```typescript
/**
 * Quote-aware command string parser. Splits a string into an argument
 * array respecting single and double quotes, similar to shell parsing.
 */
export function parseCommand(input: string): string[] {
  const args: string[] = [];
  let current = "";
  let quote: string | null = null;
  let escape = false;

  for (const char of input) {
    if (escape) {
      current += char;
      escape = false;
      continue;
    }

    if (char === "\\" && quote === '"') {
      escape = true;
      continue;
    }

    if (char === quote) {
      quote = null;
      continue;
    }

    if (!quote && (char === '"' || char === "'")) {
      quote = char;
      continue;
    }

    if (!quote && char === " ") {
      if (current) {
        args.push(current);
        current = "";
      }
      continue;
    }

    current += char;
  }

  if (current) {
    args.push(current);
  }

  return args;
}

/**
 * Joins an argument array into a command string, quoting args that
 * contain spaces with double quotes.
 */
export function joinCommand(args: string[]): string {
  return args
    .map((arg) => (arg.includes(" ") ? `"${arg}"` : arg))
    .join(" ");
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/lib/parseCommand.test.ts`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```
feat(frontend): add quote-aware command string parser
```

---

### Task 4: Frontend — API client methods + type update

**Files:**
- Modify: `frontend/src/api/types.ts` (add `StartInterval` to Healthcheck)
- Modify: `frontend/src/api/client.ts` (add healthcheck API methods)

- [ ] **Step 1: Add `StartInterval` to Healthcheck type**

In `types.ts`, inside the `Healthcheck` type on `ContainerSpec`, add after `StartPeriod`:

```typescript
StartInterval?: number;
```

- [ ] **Step 2: Add API methods to `client.ts`**

Add after the existing `serviceResources` method:

```typescript
serviceHealthcheck: (id: string, signal?: AbortSignal) =>
  fetchJSON<{ healthcheck: Healthcheck | null }>(`/services/${id}/healthcheck`, signal).then(
    (r) => r.healthcheck,
  ),
```

Add a `Healthcheck` type alias at the top of the file or import it from types. Since the type is inline on ContainerSpec, extract it:

In `types.ts`, add a standalone type:

```typescript
export type Healthcheck = NonNullable<
  Service["Spec"]["TaskTemplate"]["ContainerSpec"]["Healthcheck"]
>;
```

Add the PUT method after existing mutation helpers:

```typescript
putServiceHealthcheck: (id: string, healthcheck: Healthcheck) =>
  put<{ healthcheck: Healthcheck }>(`/services/${id}/healthcheck`, healthcheck),
```

- [ ] **Step 3: Type check**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 4: Commit**

```
feat(frontend): add healthcheck API methods and StartInterval type
```

---

### Task 5: Frontend — HealthcheckEditor component

**Files:**
- Create: `frontend/src/components/service-detail/HealthcheckEditor.tsx`
- Modify: `frontend/src/components/service-detail/index.ts` (export)
- Modify: `frontend/src/pages/ServiceDetail.tsx` (integrate)

- [ ] **Step 1: Create HealthcheckEditor component**

The component handles both display and edit modes. Display mode shows stat cards (option B from the design). Edit mode shows toggles + inputs.

Key implementation details:
- Extract mode from `Test[0]`: `"CMD-SHELL"` → shell, `"CMD"` → exec, `"NONE"` → disabled
- Strip the mode prefix from command for display: `Test.slice(1).join(" ")` for CMD, `Test[1]` for CMD-SHELL
- Duration display: convert nanoseconds to seconds (`ns / 1e9`), show "default" for 0/undefined
- Edit state: `enabled` boolean, `useShell` boolean, `command` string, duration fields as string inputs (to support empty = default)
- On save: build `HealthConfig` from form state, call `api.putServiceHealthcheck`
- For CMD mode: use `parseCommand(command)` to split into args, prepend `"CMD"`
- For CMD-SHELL mode: `["CMD-SHELL", command]`
- For disabled: `["NONE"]`
- Durations: parse input as float, multiply by `1e9` for nanoseconds. Empty → `0` (Docker default).

The component should be wrapped in a `CollapsibleSection` with an edit button, following the same pattern as `EnvEditor`.

- [ ] **Step 2: Export from index.ts**

Add to `frontend/src/components/service-detail/index.ts`:

```typescript
export { HealthcheckEditor } from "./HealthcheckEditor";
```

- [ ] **Step 3: Integrate into ServiceDetail.tsx**

Replace the existing healthcheck CollapsibleSection (lines ~397-424) with the new component.

Add state:
```typescript
const [healthcheck, setHealthcheck] = useState<Healthcheck | null | undefined>(undefined);
```

Fetch in `fetchData`:
```typescript
api
  .serviceHealthcheck(id, signal)
  .then(setHealthcheck)
  .catch(() => {});
```

Render (replacing the old KVTable section):
```typescript
{healthcheck !== undefined && (
  <HealthcheckEditor
    serviceId={id!}
    healthcheck={healthcheck}
    onSaved={setHealthcheck}
  />
)}
```

Remove the `hasContainerConfig` check for healthcheck fields (Healthcheck is no longer part of the container config section — it has its own section).

Update `hasContainerConfig` to remove the healthcheck-related conditions if any were included.

- [ ] **Step 4: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 5: Run all frontend tests**

Run: `cd frontend && npx vitest run`

- [ ] **Step 6: Commit**

```
feat(frontend): add HealthcheckEditor with display and edit modes
```

---

### Task 6: Backend tests — run full suite

- [ ] **Step 1: Run all Go tests**

Run: `go test ./...`
Expected: All tests pass including new healthcheck handler tests.

- [ ] **Step 2: Run all frontend tests**

Run: `cd frontend && npx vitest run`
Expected: All tests pass.

- [ ] **Step 3: Run lint and format checks**

Run: `make check`
Expected: Clean.

- [ ] **Step 4: Final commit if any formatting changes needed**

```
style: apply formatting
```

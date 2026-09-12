# Config & Secret Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add config and secret creation to the dashboard via modal dialogs on list pages.

**Architecture:** Backend adds `POST /configs` and `POST /secrets` endpoints (tier 2) that wrap Docker SDK's `ConfigCreate`/`SecretCreate`. Frontend adds a reusable `CreateResourceDialog` base component with per-resource form components (`CreateConfigForm`, `CreateSecretForm`) rendered on list pages.

**Tech Stack:** Go (net/http, Docker SDK), React 19, TypeScript, shadcn/ui

**Spec:** `docs/superpowers/specs/2026-03-25-config-secret-creation-design.md`

---

### Task 1: Add error codes for config/secret creation

**Files:**
- Modify: `internal/api/errors.go:477-498` (after existing CFG002/SEC002 entries)

- [ ] **Step 1: Add CFG003, CFG004, SEC003, SEC004 error codes**

Add these entries to the `errorRegistry` map in `errors.go`, after the existing CFG002 and SEC002 entries:

```go
"CFG003": {
    Code:        "CFG003",
    Title:       "Config Name Conflict",
    Status:      http.StatusConflict,
    Description: "A config with this name already exists.",
    Suggestion:  "Choose a different name or remove the existing config first.",
},
"CFG004": {
    Code:        "CFG004",
    Title:       "Invalid Config",
    Status:      http.StatusBadRequest,
    Description: "The config creation request is invalid.",
    Suggestion:  "Provide a non-empty name and valid base64-encoded data.",
},
```

And after SEC002:

```go
"SEC003": {
    Code:        "SEC003",
    Title:       "Secret Name Conflict",
    Status:      http.StatusConflict,
    Description: "A secret with this name already exists.",
    Suggestion:  "Choose a different name or remove the existing secret first.",
},
"SEC004": {
    Code:        "SEC004",
    Title:       "Invalid Secret",
    Status:      http.StatusBadRequest,
    Description: "The secret creation request is invalid.",
    Suggestion:  "Provide a non-empty name and valid base64-encoded data.",
},
```

- [ ] **Step 2: Verify tests pass**

Run: `go test ./internal/api/ -run TestError -v -count=1`
Expected: PASS (no existing test breakage)

- [ ] **Step 3: Commit**

```bash
git add internal/api/errors.go
git commit -m "feat: add error codes for config/secret creation (CFG003/004, SEC003/004)"
```

---

### Task 2: Add Docker client CreateConfig and CreateSecret methods

**Files:**
- Modify: `internal/docker/client.go:578-584` (after `RemoveConfig`/`RemoveSecret`)

- [ ] **Step 1: Add CreateConfig method**

Add after the existing `RemoveSecret` method (line ~584):

```go
func (c *Client) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	resp, err := c.docker.ConfigCreate(ctx, spec)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	resp, err := c.docker.SecretCreate(ctx, spec)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/docker/`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add CreateConfig and CreateSecret to Docker client"
```

---

### Task 3: Add DockerWriteClient interface methods and mock

**Files:**
- Modify: `internal/api/handlers.go:63-112` (DockerWriteClient interface)
- Modify: `internal/api/write_handlers_test.go:25-56` (mockWriteClient struct + methods)

- [ ] **Step 1: Add to DockerWriteClient interface**

Add these two methods to the `DockerWriteClient` interface in `handlers.go` (after `RemoveSecret`):

```go
CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error)
CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
```

- [ ] **Step 2: Add mock fields and methods**

In `write_handlers_test.go`, add to the `mockWriteClient` struct:

```go
createConfigFn func(ctx context.Context, spec swarm.ConfigSpec) (string, error)
createSecretFn func(ctx context.Context, spec swarm.SecretSpec) (string, error)
```

Add the mock method implementations (after the existing `RemoveSecret` mock method):

```go
func (m *mockWriteClient) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	if m.createConfigFn != nil {
		return m.createConfigFn(ctx, spec)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockWriteClient) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	if m.createSecretFn != nil {
		return m.createSecretFn(ctx, spec)
	}
	return "", fmt.Errorf("not implemented")
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/api/`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add CreateConfig/CreateSecret to DockerWriteClient interface and mock"
```

---

### Task 4: Write handler tests for config creation

**Files:**
- Modify: `internal/api/write_handlers_test.go` (append after the RemoveSecret tests)

- [ ] **Step 1: Write tests for HandleCreateConfig**

Append these tests after the existing `TestHandleRemoveSecret_*` tests:

```go
func TestHandleCreateConfig_OK(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		createConfigFn: func(_ context.Context, spec swarm.ConfigSpec) (string, error) {
			return "new-cfg-id", nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	// Pre-populate cache so the post-create inspect works.
	c.SetConfig(swarm.Config{
		ID:   "new-cfg-id",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "my-config"}},
	})

	body := `{"name":"my-config","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/configs/new-cfg-id" {
		t.Errorf("Location=%q, want /configs/new-cfg-id", loc)
	}
}

func TestHandleCreateConfig_MissingName(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, nil, nil, &mockWriteClient{}, nil, closedReady(), nil, config.OpsConfiguration)

	body := `{"data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateConfig_InvalidBase64(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, nil, nil, &mockWriteClient{}, nil, closedReady(), nil, config.OpsConfiguration)

	body := `{"name":"my-config","data":"not-valid-base64!!!"}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateConfig_NameConflict(t *testing.T) {
	wc := &mockWriteClient{
		createConfigFn: func(_ context.Context, spec swarm.ConfigSpec) (string, error) {
			return "", errdefs.Conflict(fmt.Errorf("config already exists"))
		},
	}
	h := NewHandlers(cache.New(nil), nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	body := `{"name":"existing","data":"aGVsbG8="}`
	req := httptest.NewRequest("POST", "/configs", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateConfig_InvalidJSON(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, nil, nil, &mockWriteClient{}, nil, closedReady(), nil, config.OpsConfiguration)

	req := httptest.NewRequest("POST", "/configs", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()
	h.HandleCreateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandleCreateConfig -v -count=1`
Expected: FAIL — `HandleCreateConfig` does not exist yet

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_handlers_test.go
git commit -m "test: add tests for HandleCreateConfig"
```

---

### Task 5: Write handler tests for secret creation

**Files:**
- Modify: `internal/api/write_handlers_test.go` (append after config creation tests)

- [ ] **Step 1: Write tests for HandleCreateSecret**

```go
func TestHandleCreateSecret_OK(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
			return "new-sec-id", nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	c.SetSecret(swarm.Secret{
		ID:   "new-sec-id",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "my-secret"}},
	})

	body := `{"name":"my-secret","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/secrets/new-sec-id" {
		t.Errorf("Location=%q, want /secrets/new-sec-id", loc)
	}
}

func TestHandleCreateSecret_MissingName(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, nil, nil, &mockWriteClient{}, nil, closedReady(), nil, config.OpsConfiguration)

	body := `{"data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleCreateSecret_NameConflict(t *testing.T) {
	wc := &mockWriteClient{
		createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
			return "", errdefs.Conflict(fmt.Errorf("secret already exists"))
		},
	}
	h := NewHandlers(cache.New(nil), nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	body := `{"name":"existing","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateSecret_ClearsData(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
			return "new-sec-id", nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, nil, closedReady(), nil, config.OpsConfiguration)

	c.SetSecret(swarm.Secret{
		ID:   "new-sec-id",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "my-secret"},
			Data:        []byte("sensitive"),
		},
	})

	body := `{"name":"my-secret","data":"c2VjcmV0"}`
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreateSecret(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}

	// Verify the response does not contain secret data.
	respBody := w.Body.String()
	if strings.Contains(respBody, "sensitive") {
		t.Error("response contains secret data; expected it to be cleared")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandleCreateSecret -v -count=1`
Expected: FAIL — `HandleCreateSecret` does not exist yet

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_handlers_test.go
git commit -m "test: add tests for HandleCreateSecret"
```

---

### Task 6: Implement HandleCreateConfig and HandleCreateSecret handlers

**Files:**
- Modify: `internal/api/write_handlers.go` (after `HandleRemoveSecret`, around line 632)

- [ ] **Step 1: Add the request struct and both handlers**

Add after `HandleRemoveSecret`:

```go
type createResourceRequest struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

func (h *Handlers) HandleCreateConfig(w http.ResponseWriter, r *http.Request) {
	var req createResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeErrorCode(w, r, "CFG004", "name is required")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeErrorCode(w, r, "CFG004", "data must be valid base64")
		return
	}

	slog.Info("creating config", "name", req.Name)

	id, err := h.writeClient.CreateConfig(r.Context(), swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: req.Name},
		Data:        data,
	})
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, "CFG003", err.Error())
			return
		}
		writeDockerError(w, r, err, "config")
		return
	}

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		// Cache may not have caught up yet; return minimal response.
		w.Header().Set("Location", "/configs/"+id)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse("/configs/"+id, "Config", map[string]any{
			"config":   swarm.Config{ID: id, Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: req.Name}}},
			"services": []ServiceRef{},
		}))
		return
	}

	w.Header().Set("Location", "/configs/"+id)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse("/configs/"+id, "Config", map[string]any{
		"config":   cfg,
		"services": h.cache.ServicesUsingConfig(id),
	}))
}

func (h *Handlers) HandleCreateSecret(w http.ResponseWriter, r *http.Request) {
	var req createResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeErrorCode(w, r, "SEC004", "name is required")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeErrorCode(w, r, "SEC004", "data must be valid base64")
		return
	}

	slog.Info("creating secret", "name", req.Name)

	id, err := h.writeClient.CreateSecret(r.Context(), swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: req.Name},
		Data:        data,
	})
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, "SEC003", err.Error())
			return
		}
		writeDockerError(w, r, err, "secret")
		return
	}

	sec, ok := h.cache.GetSecret(id)
	if !ok {
		w.Header().Set("Location", "/secrets/"+id)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse("/secrets/"+id, "Secret", map[string]any{
			"secret":   swarm.Secret{ID: id, Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: req.Name}}},
			"services": []ServiceRef{},
		}))
		return
	}

	sec.Spec.Data = nil
	w.Header().Set("Location", "/secrets/"+id)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse("/secrets/"+id, "Secret", map[string]any{
		"secret":   sec,
		"services": h.cache.ServicesUsingSecret(id),
	}))
}
```

Ensure these imports are present at the top of the file: `"encoding/base64"`, `"strings"`.

- [ ] **Step 2: Run all creation tests**

Run: `go test ./internal/api/ -run "TestHandleCreate(Config|Secret)" -v -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_handlers.go
git commit -m "feat: implement HandleCreateConfig and HandleCreateSecret handlers"
```

---

### Task 7: Register routes

**Files:**
- Modify: `internal/api/router.go:259` (after `DELETE /configs/{id}`, before Secrets section)
- Modify: `internal/api/router.go:278` (after `DELETE /secrets/{id}`)

- [ ] **Step 1: Add POST routes**

After the `DELETE /configs/{id}` line (line 259), add:

```go
mux.Handle("POST /configs", tier2(h.HandleCreateConfig))
```

After the `DELETE /secrets/{id}` line (line 278), add:

```go
mux.Handle("POST /secrets", tier2(h.HandleCreateSecret))
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./internal/api/ -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register POST /configs and POST /secrets routes at tier 2"
```

---

### Task 8: Add frontend API client methods

**Files:**
- Modify: `frontend/src/api/client.ts:383-384` (after `removeConfig`/`removeSecret`)

- [ ] **Step 1: Add createConfig and createSecret methods**

After the existing `removeConfig` and `removeSecret` lines, add:

```typescript
createConfig: (name: string, data: string) =>
    mutationFetch<ConfigDetail>("/configs", "POST", { name, data }, "application/json"),
createSecret: (name: string, data: string) =>
    mutationFetch<SecretDetail>("/secrets", "POST", { name, data }, "application/json"),
```

Ensure `ConfigDetail` and `SecretDetail` are imported from `types.ts`. Check the existing types — if `ConfigDetail` / `SecretDetail` don't exist, use the existing detail response types (check `types.ts` for the correct names).

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: add createConfig and createSecret API client methods"
```

---

### Task 9: Create the CreateResourceDialog base component

**Files:**
- Create: `frontend/src/components/CreateResourceDialog.tsx`

This is the reusable dialog shell. It handles:
- Dialog open/close state
- Operations level gating
- `useAsyncAction` for loading/error
- Success toast + close
- Render prop for form content

- [ ] **Step 1: Create the component**

```tsx
import { type ReactNode, useState } from "react";
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
} from "./ui/alert-dialog";
import { Button } from "./ui/button";
import { Plus } from "lucide-react";
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useNavigate } from "react-router-dom";

interface CreateResourceDialogProps {
  /** Display label for the resource type, e.g. "Config" or "Secret" */
  resourceType: string;
  /** Called with form data on submit; should return the created resource path for navigation */
  onSubmit: () => Promise<string>;
  /** Render prop for form fields. Receives a ref setter for the form reset callback. */
  children: ReactNode;
  /** Whether the form is valid and ready to submit */
  canSubmit: boolean;
  /** Reset the form state (called after successful creation or dialog close) */
  onReset: () => void;
}

/**
 * Reusable dialog shell for creating swarm resources.
 * Handles dialog state, operations level gating, loading/error, and post-create navigation.
 */
export default function CreateResourceDialog({
  resourceType,
  onSubmit,
  children,
  canSubmit,
  onReset,
}: CreateResourceDialogProps) {
  const [open, setOpen] = useState(false);
  const { level, loading: levelLoading } = useOperationsLevel();
  const canCreate = !levelLoading && level >= opsLevel.configuration;
  const action = useAsyncAction({ toast: true });
  const navigate = useNavigate();

  function handleOpenChange(next: boolean) {
    if (!next) {
      onReset();
    }
    setOpen(next);
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={handleOpenChange}
    >
      <AlertDialogTrigger render={
        <Button
          size="sm"
          disabled={!canCreate}
          title={canCreate ? `Create ${resourceType.toLowerCase()}` : "Operations level too low"}
        >
          <Plus className="size-4" />
          Create
        </Button>
      } />

      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Create {resourceType}</AlertDialogTitle>
          <AlertDialogDescription>
            Create a new {resourceType.toLowerCase()} in the swarm.
          </AlertDialogDescription>
        </AlertDialogHeader>

        {children}

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={!canSubmit || action.loading}
            onClick={(event) => {
              event.preventDefault();
              void action.execute(async () => {
                const path = await onSubmit();
                setOpen(false);
                onReset();
                navigate(path);
              }, `Failed to create ${resourceType.toLowerCase()}`);
            }}
          >
            {action.loading ? "Creating…" : "Create"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
```

Note: Check the actual shadcn/ui AlertDialog API in the codebase — the `render` prop on `AlertDialogTrigger` may use `asChild` instead. Match the pattern used by `RemoveResourceAction.tsx`.

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: success (may have unused import warnings, fix as needed)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/CreateResourceDialog.tsx
git commit -m "feat: add CreateResourceDialog base component"
```

---

### Task 10: Create the CreateConfigForm component

**Files:**
- Create: `frontend/src/components/CreateConfigForm.tsx`

- [ ] **Step 1: Create the component**

This component renders name input + data input (text/file toggle) and manages its own form state. It exposes `canSubmit` and an `onSubmit` callback that base64-encodes and calls the API.

```tsx
import { type ChangeEvent, useCallback, useState } from "react";
import { api } from "../api/client";
import CreateResourceDialog from "./CreateResourceDialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Textarea } from "./ui/textarea";

type InputMode = "text" | "file";

export default function CreateConfigForm() {
  const [name, setName] = useState("");
  const [text, setText] = useState("");
  const [fileData, setFileData] = useState<string | null>(null);
  const [inputMode, setInputMode] = useState<InputMode>("text");

  const data = inputMode === "text" ? text : fileData;
  const canSubmit = name.trim().length > 0 && data != null && data.length > 0;

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) {
      setFileData(null);
      return;
    }

    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      // result is "data:...;base64,<data>" — extract the base64 part
      const base64 = result.includes(",") ? result.split(",")[1] : result;
      setFileData(base64);
    };
    reader.readAsDataURL(file);
  }

  const handleSubmit = useCallback(async () => {
    const encoded = inputMode === "text" ? btoa(text) : fileData!;
    await api.createConfig(name.trim(), encoded);
    // Return value not needed for navigation since we don't have the ID yet.
    // We'll need to extract it from the response.
    return "/configs";
  }, [name, text, fileData, inputMode]);

  function reset() {
    setName("");
    setText("");
    setFileData(null);
    setInputMode("text");
  }

  return (
    <CreateResourceDialog
      resourceType="Config"
      onSubmit={handleSubmit}
      canSubmit={canSubmit}
      onReset={reset}
    >
      <div className="flex flex-col gap-4 py-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor="config-name">Name</Label>
          <Input
            id="config-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="my-config"
            autoFocus
          />
        </div>

        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <Label>Data</Label>
            <div className="flex gap-1 text-xs">
              <button
                type="button"
                className={`rounded px-2 py-0.5 ${inputMode === "text" ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setInputMode("text")}
              >
                Text
              </button>
              <button
                type="button"
                className={`rounded px-2 py-0.5 ${inputMode === "file" ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setInputMode("file")}
              >
                File
              </button>
            </div>
          </div>

          {inputMode === "text" ? (
            <Textarea
              value={text}
              onChange={(event) => setText(event.target.value)}
              placeholder="Paste config content…"
              rows={8}
              className="font-mono text-sm"
            />
          ) : (
            <Input
              type="file"
              onChange={handleFileChange}
            />
          )}
        </div>
      </div>
    </CreateResourceDialog>
  );
}
```

**Important:** The `handleSubmit` needs to return the path for navigation. Update to extract the ID from the API response. This requires `createConfig` to return the detail response. Adjust `handleSubmit`:

```tsx
const handleSubmit = useCallback(async () => {
    const encoded = inputMode === "text" ? btoa(text) : fileData!;
    const response = await api.createConfig(name.trim(), encoded);
    return `/configs/${response.config.ID}`;
}, [name, text, fileData, inputMode]);
```

Verify the response shape matches the types in `types.ts`. If `ConfigDetail` has a different structure (e.g., `config` nested differently), adjust accordingly.

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/CreateConfigForm.tsx
git commit -m "feat: add CreateConfigForm component"
```

---

### Task 11: Create the CreateSecretForm component

**Files:**
- Create: `frontend/src/components/CreateSecretForm.tsx`

- [ ] **Step 1: Create the component**

Same structure as `CreateConfigForm` but calls `api.createSecret` and navigates to `/secrets/{id}`:

```tsx
import { type ChangeEvent, useCallback, useState } from "react";
import { api } from "../api/client";
import CreateResourceDialog from "./CreateResourceDialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Textarea } from "./ui/textarea";

type InputMode = "text" | "file";

export default function CreateSecretForm() {
  const [name, setName] = useState("");
  const [text, setText] = useState("");
  const [fileData, setFileData] = useState<string | null>(null);
  const [inputMode, setInputMode] = useState<InputMode>("text");

  const data = inputMode === "text" ? text : fileData;
  const canSubmit = name.trim().length > 0 && data != null && data.length > 0;

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) {
      setFileData(null);
      return;
    }

    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      const base64 = result.includes(",") ? result.split(",")[1] : result;
      setFileData(base64);
    };
    reader.readAsDataURL(file);
  }

  const handleSubmit = useCallback(async () => {
    const encoded = inputMode === "text" ? btoa(text) : fileData!;
    const response = await api.createSecret(name.trim(), encoded);
    return `/secrets/${response.secret.ID}`;
  }, [name, text, fileData, inputMode]);

  function reset() {
    setName("");
    setText("");
    setFileData(null);
    setInputMode("text");
  }

  return (
    <CreateResourceDialog
      resourceType="Secret"
      onSubmit={handleSubmit}
      canSubmit={canSubmit}
      onReset={reset}
    >
      <div className="flex flex-col gap-4 py-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor="secret-name">Name</Label>
          <Input
            id="secret-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="my-secret"
            autoFocus
          />
        </div>

        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <Label>Data</Label>
            <div className="flex gap-1 text-xs">
              <button
                type="button"
                className={`rounded px-2 py-0.5 ${inputMode === "text" ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setInputMode("text")}
              >
                Text
              </button>
              <button
                type="button"
                className={`rounded px-2 py-0.5 ${inputMode === "file" ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setInputMode("file")}
              >
                File
              </button>
            </div>
          </div>

          {inputMode === "text" ? (
            <Textarea
              value={text}
              onChange={(event) => setText(event.target.value)}
              placeholder="Paste secret content…"
              rows={8}
              className="font-mono text-sm"
            />
          ) : (
            <Input
              type="file"
              onChange={handleFileChange}
            />
          )}
        </div>
      </div>
    </CreateResourceDialog>
  );
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/CreateSecretForm.tsx
git commit -m "feat: add CreateSecretForm component"
```

---

### Task 12: Integrate CreateConfigForm into ConfigList page

**Files:**
- Modify: `frontend/src/pages/ConfigList.tsx`

- [ ] **Step 1: Add CreateConfigForm to the page header**

Import `CreateConfigForm` and add it as an action in the `PageHeader`:

```tsx
import CreateConfigForm from "../components/CreateConfigForm";
```

Change the three `<PageHeader title="Configs" />` occurrences (loading state, error state, and main render) to include the create form in the main render. Only the main render (not loading/error) needs the action:

```tsx
<PageHeader
  title="Configs"
  actions={<CreateConfigForm />}
/>
```

The loading and error states keep the plain `<PageHeader title="Configs" />`.

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/ConfigList.tsx
git commit -m "feat: add config creation dialog to ConfigList page"
```

---

### Task 13: Integrate CreateSecretForm into SecretList page

**Files:**
- Modify: `frontend/src/pages/SecretList.tsx`

- [ ] **Step 1: Add CreateSecretForm to the page header**

Same pattern as ConfigList:

```tsx
import CreateSecretForm from "../components/CreateSecretForm";
```

Update the main render's `PageHeader`:

```tsx
<PageHeader
  title="Secrets"
  actions={<CreateSecretForm />}
/>
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/SecretList.tsx
git commit -m "feat: add secret creation dialog to SecretList page"
```

---

### Task 14: Update OpenAPI spec and API docs

**Files:**
- Modify: `api/openapi.yaml:2503` (add `post:` under `/configs` path)
- Modify: `api/openapi.yaml` (add `post:` under `/secrets` path)
- Modify: `docs/api.md`

- [ ] **Step 1: Add POST /configs to OpenAPI spec**

Under the existing `/configs:` path (which has `get:`), add a `post:` operation:

```yaml
    post:
      operationId: createConfig
      tags: [Configs]
      summary: Create a config
      description: Creates a new config in the swarm. Requires operations level 2.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name, data]
              properties:
                name:
                  type: string
                  description: Config name
                data:
                  type: string
                  format: byte
                  description: Config data (base64-encoded)
      responses:
        "201":
          description: Config created
          headers:
            Location:
              schema:
                type: string
              description: Path to the created config
          content:
            application/json:
              schema:
                allOf:
                  - $ref: "#/components/schemas/DetailEnvelope"
                  - type: object
                    properties:
                      config:
                        type: object
                      services:
                        type: array
                        items:
                          $ref: "#/components/schemas/ServiceRef"
        "400":
          $ref: "#/components/responses/BadRequest"
        "403":
          description: Operations level too low
        "409":
          description: Config name conflict
```

- [ ] **Step 2: Add POST /secrets to OpenAPI spec**

Same pattern under the `/secrets:` path.

- [ ] **Step 3: Add to docs/api.md**

Add `POST /configs` and `POST /secrets` to the appropriate section in `docs/api.md`, following the existing format.

- [ ] **Step 4: Commit**

```bash
git add api/openapi.yaml docs/api.md
git commit -m "docs: add POST /configs and POST /secrets to OpenAPI spec and API docs"
```

---

### Task 15: Update CHANGELOG.md

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add changelog entry**

Under `[Unreleased]`, add:

```markdown
- Add config and secret creation from the dashboard
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: add changelog entry for config/secret creation"
```

---

### Task 16: Full verification

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

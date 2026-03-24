# Plugin Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Full Docker plugin management — enable, disable, install, remove, upgrade, and configure — with a dedicated plugin detail page and two-step install/upgrade privilege flow.

**Architecture:** New `DockerPluginClient` interface (separate from `DockerWriteClient`) with live Docker API calls (no cache/SSE). Backend handlers follow existing write handler patterns. Frontend adds a plugin list page (simple table, same as SwarmPage), a plugin detail page with actions and settings editor, and install/upgrade dialogs with privilege review.

**Tech Stack:** Go (stdlib `net/http`, Docker SDK v28), React 19, TypeScript, Tailwind CSS v4, shadcn/ui

**Spec:** `docs/superpowers/specs/2026-03-24-plugin-management-design.md`

---

## File Structure

### Backend (create)
- `internal/api/plugin_handlers.go` — all plugin read/write handlers
- `internal/api/plugin_handlers_test.go` — handler tests with mock plugin client

### Backend (modify)
- `internal/api/handlers.go` — add `DockerPluginClient` interface, add `pluginClient` field to `Handlers`, update `NewHandlers`
- `internal/api/router.go` — register plugin routes
- `internal/docker/client.go` — add plugin wrapper methods
- `main.go` — pass `dockerClient` as `DockerPluginClient` to `NewHandlers`

### Frontend (create)
- `frontend/src/pages/PluginList.tsx` — simple plugin table (mirrors SwarmPage plugin section)
- `frontend/src/pages/PluginDetail.tsx` — plugin detail with actions and settings
- `frontend/src/components/InstallPluginDialog.tsx` — two-step install/upgrade dialog

### Frontend (modify)
- `frontend/src/api/types.ts` — expand `Plugin` interface with full SDK fields
- `frontend/src/api/client.ts` — add plugin API methods
- `frontend/src/pages/SwarmPage.tsx` — add links to plugin detail pages, install action, "View All" link
- `frontend/src/App.tsx` — add plugin routes

---

### Task 1: DockerPluginClient Interface and Handlers Struct

**Files:**
- Modify: `internal/api/handlers.go:49-190`

- [ ] **Step 1: Add `DockerPluginClient` interface**

Add after `DockerWriteClient` (line 153). Move `PluginList` out of `DockerSystemClient`:

```go
type DockerPluginClient interface {
	PluginList(ctx context.Context) (types.PluginsListResponse, error)
	PluginInspect(ctx context.Context, name string) (*types.Plugin, error)
	PluginEnable(ctx context.Context, name string) error
	PluginDisable(ctx context.Context, name string) error
	PluginRemove(ctx context.Context, name string, force bool) error
	PluginInstall(ctx context.Context, remote string) (*types.Plugin, error)
	PluginUpgrade(ctx context.Context, name string, remote string) error
	PluginPrivileges(ctx context.Context, remote string) (types.PluginPrivileges, error)
	PluginConfigure(ctx context.Context, name string, args []string) error
}
```

Note: `PluginInstall` and `PluginUpgrade` signatures are simplified vs the spec — privileges are not passed as parameters because the Docker SDK's `AcceptAllPermissions: true` option handles privilege acceptance server-side. The two-step UI flow (privileges check → install) is a frontend concern; the backend just auto-accepts.

Remove `PluginList` from `DockerSystemClient` (line 52).

- [ ] **Step 2: Update `Handlers` struct and `NewHandlers`**

Add `pluginClient DockerPluginClient` field to `Handlers` struct. Add `pc DockerPluginClient` parameter to `NewHandlers`. Update `HandlePlugins` to use `h.pluginClient.PluginList` instead of `h.systemClient.PluginList`.

- [ ] **Step 3: Update `main.go`**

Pass `dockerClient` as the new `DockerPluginClient` argument to `NewHandlers`.

- [ ] **Step 4: Update all existing `NewHandlers` call sites**

Adding a new parameter to `NewHandlers` breaks every existing caller (~230 call sites across `main.go`, `write_handlers_test.go`, `handlers_test.go`, `swarm_handlers_test.go`, `loghandler_test.go`, `topology_test.go`, `middleware_test.go`, `write_middleware_test.go`, `handlers_bench_test.go`, `openapi_test.go`, `integration_test.go`). Add `nil` for the `pc DockerPluginClient` argument in all test helpers (it's unused in those tests). In `main.go`, pass `dockerClient`.

Run: `grep -rn "NewHandlers(" internal/api/ main.go` to find all call sites.

- [ ] **Step 5: Verify compilation**

Run: `go build ./...`
Expected: PASS (no new handlers yet, just interface + wiring)

- [ ] **Step 5: Commit**

```
feat: add DockerPluginClient interface and wire into Handlers
```

---

### Task 2: Docker Client Plugin Methods

**Files:**
- Modify: `internal/docker/client.go`

- [ ] **Step 1: Add `PluginInspect`**

```go
func (c *Client) PluginInspect(ctx context.Context, name string) (*types.Plugin, error) {
	plugin, _, err := c.docker.PluginInspectWithRaw(ctx, name)
	return plugin, err
}
```

- [ ] **Step 2: Add `PluginEnable` and `PluginDisable`**

```go
func (c *Client) PluginEnable(ctx context.Context, name string) error {
	return c.docker.PluginEnable(ctx, name, types.PluginEnableOptions{Timeout: 30})
}

func (c *Client) PluginDisable(ctx context.Context, name string) error {
	return c.docker.PluginDisable(ctx, name, types.PluginDisableOptions{})
}
```

- [ ] **Step 3: Add `PluginRemove`**

```go
func (c *Client) PluginRemove(ctx context.Context, name string, force bool) error {
	return c.docker.PluginRemove(ctx, name, types.PluginRemoveOptions{Force: force})
}
```

- [ ] **Step 4: Add `PluginPrivileges`**

The Docker SDK has no public method for this. Use the SDK client's HTTP transport to call the Docker Engine API directly. The SDK's `HTTPClient()` has a custom transport that dials the unix socket regardless of the URL host, so we use `http://localhost` as a placeholder host:

```go
func (c *Client) PluginPrivileges(ctx context.Context, remote string) (types.PluginPrivileges, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://localhost/v1.46/plugins/privileges?remote="+url.QueryEscape(remote),
		nil,
	)
	if err != nil {
		return nil, err
	}

	resp, err := c.docker.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plugin privileges: %s: %s", resp.Status, string(body))
	}

	var privileges types.PluginPrivileges
	if err := json.NewDecoder(resp.Body).Decode(&privileges); err != nil {
		return nil, err
	}
	return privileges, nil
}
```

- [ ] **Step 5: Add `PluginInstall`**

```go
func (c *Client) PluginInstall(ctx context.Context, remote string) (*types.Plugin, error) {
	rc, err := c.docker.PluginInstall(ctx, remote, types.PluginInstallOptions{
		RemoteRef:            remote,
		AcceptAllPermissions: true,
	})
	if err != nil {
		return nil, err
	}
	// Drain the pull progress stream.
	_, _ = io.Copy(io.Discard, rc)
	rc.Close()

	return c.PluginInspect(ctx, remote)
}
```

- [ ] **Step 6: Add `PluginUpgrade`**

```go
func (c *Client) PluginUpgrade(ctx context.Context, name string, remote string) error {
	rc, err := c.docker.PluginUpgrade(ctx, name, types.PluginInstallOptions{
		RemoteRef:            remote,
		AcceptAllPermissions: true,
	})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, rc)
	rc.Close()
	return nil
}
```

- [ ] **Step 7: Add `PluginConfigure`**

```go
func (c *Client) PluginConfigure(ctx context.Context, name string, args []string) error {
	return c.docker.PluginSet(ctx, name, args)
}
```

- [ ] **Step 8: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 9: Commit**

```
feat: add Docker client plugin wrapper methods
```

---

### Task 3: Plugin Read Handlers

**Files:**
- Create: `internal/api/plugin_handlers.go`

- [ ] **Step 1: Write tests for `HandlePlugin` (detail) and `HandlePlugins` (list)**

Create `internal/api/plugin_handlers_test.go`:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types"
	json "github.com/goccy/go-json"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

type mockPluginClient struct {
	pluginListFn       func(ctx context.Context) (types.PluginsListResponse, error)
	pluginInspectFn    func(ctx context.Context, name string) (*types.Plugin, error)
	pluginEnableFn     func(ctx context.Context, name string) error
	pluginDisableFn    func(ctx context.Context, name string) error
	pluginRemoveFn     func(ctx context.Context, name string, force bool) error
	pluginInstallFn    func(ctx context.Context, remote string) (*types.Plugin, error)
	pluginUpgradeFn    func(ctx context.Context, name string, remote string) error
	pluginPrivilegesFn func(ctx context.Context, remote string) (types.PluginPrivileges, error)
	pluginConfigureFn  func(ctx context.Context, name string, args []string) error
}

func (m *mockPluginClient) PluginList(ctx context.Context) (types.PluginsListResponse, error) {
	if m.pluginListFn != nil {
		return m.pluginListFn(ctx)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginInspect(ctx context.Context, name string) (*types.Plugin, error) {
	if m.pluginInspectFn != nil {
		return m.pluginInspectFn(ctx, name)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginEnable(ctx context.Context, name string) error {
	if m.pluginEnableFn != nil {
		return m.pluginEnableFn(ctx, name)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginDisable(ctx context.Context, name string) error {
	if m.pluginDisableFn != nil {
		return m.pluginDisableFn(ctx, name)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginRemove(ctx context.Context, name string, force bool) error {
	if m.pluginRemoveFn != nil {
		return m.pluginRemoveFn(ctx, name, force)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginInstall(ctx context.Context, remote string) (*types.Plugin, error) {
	if m.pluginInstallFn != nil {
		return m.pluginInstallFn(ctx, remote)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginUpgrade(ctx context.Context, name string, remote string) error {
	if m.pluginUpgradeFn != nil {
		return m.pluginUpgradeFn(ctx, name, remote)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginPrivileges(ctx context.Context, remote string) (types.PluginPrivileges, error) {
	if m.pluginPrivilegesFn != nil {
		return m.pluginPrivilegesFn(ctx, remote)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginConfigure(ctx context.Context, name string, args []string) error {
	if m.pluginConfigureFn != nil {
		return m.pluginConfigureFn(ctx, name, args)
	}
	return fmt.Errorf("not implemented")
}

var _ DockerPluginClient = (*mockPluginClient)(nil)

func newPluginHandlers(pc *mockPluginClient) *Handlers {
	ready := make(chan struct{})
	close(ready)
	return NewHandlers(
		cache.New(nil),
		NewBroadcaster(0),
		nil,
		nil,
		nil,
		pc,
		ready,
		nil,
		config.OpsImpactful,
	)
}

func TestHandlePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			return &types.Plugin{
				ID:   "abc123",
				Name: name,
			}, nil
		},
	}
	h := newPluginHandlers(pc)

	req := httptest.NewRequest(http.MethodGet, "/plugins/my-plugin", nil)
	req.SetPathValue("name", "my-plugin")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	h.HandlePlugin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Plugin" {
		t.Errorf("expected @type Plugin, got %v", resp["@type"])
	}
}

func TestHandlePlugin_NotFound(t *testing.T) {
	pc := &mockPluginClient{
		pluginInspectFn: func(_ context.Context, _ string) (*types.Plugin, error) {
			return nil, errdefs.NotFound(fmt.Errorf("plugin not found"))
		},
	}
	h := newPluginHandlers(pc)

	req := httptest.NewRequest(http.MethodGet, "/plugins/missing", nil)
	req.SetPathValue("name", "missing")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	h.HandlePlugin(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

Note: You'll need to add `errdefs` import (`"github.com/docker/docker/errdefs"`) and `"strings"` (used by Task 5 tests). Also `newPluginHandlers` needs to match `NewHandlers`'s actual signature after Task 1 — adjust the argument order to match.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandlePlugin -v`
Expected: FAIL (HandlePlugin not defined)

- [ ] **Step 3: Implement `HandlePlugin` and move `HandlePlugins`**

Create `internal/api/plugin_handlers.go`:

```go
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"
)

func (h *Handlers) HandlePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	plugin, err := h.pluginClient.PluginInspect(ctx, name)
	if err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	writeJSONWithETag(w, r, NewDetailResponse("/plugins/"+name, "Plugin", map[string]any{
		"plugin": plugin,
	}))
}
```

Move `HandlePlugins` from `handlers.go` to `plugin_handlers.go` and update it to use `h.pluginClient.PluginList`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run TestHandlePlugin -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: add plugin read handlers (list and detail)
```

---

### Task 4: Plugin Write Handlers

**Files:**
- Modify: `internal/api/plugin_handlers.go`
- Modify: `internal/api/plugin_handlers_test.go`

- [ ] **Step 1: Write tests for enable, disable, remove**

Add to `plugin_handlers_test.go`:

```go
func TestHandleEnablePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginEnableFn: func(_ context.Context, _ string) error { return nil },
	}
	h := newPluginHandlers(pc)

	req := httptest.NewRequest(http.MethodPost, "/plugins/my-plugin/enable", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()

	h.HandleEnablePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDisablePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginDisableFn: func(_ context.Context, _ string) error { return nil },
	}
	h := newPluginHandlers(pc)

	req := httptest.NewRequest(http.MethodPost, "/plugins/my-plugin/disable", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()

	h.HandleDisablePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemovePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginRemoveFn: func(_ context.Context, _ string, _ bool) error { return nil },
	}
	h := newPluginHandlers(pc)

	req := httptest.NewRequest(http.MethodDelete, "/plugins/my-plugin", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()

	h.HandleRemovePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemovePlugin_Force(t *testing.T) {
	var gotForce bool
	pc := &mockPluginClient{
		pluginRemoveFn: func(_ context.Context, _ string, force bool) error {
			gotForce = force
			return nil
		},
	}
	h := newPluginHandlers(pc)

	req := httptest.NewRequest(http.MethodDelete, "/plugins/my-plugin?force=true", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()

	h.HandleRemovePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if !gotForce {
		t.Error("expected force=true to be passed through")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestHandle(Enable|Disable|Remove)Plugin" -v`
Expected: FAIL

- [ ] **Step 3: Implement enable, disable, remove handlers**

Add to `plugin_handlers.go`:

```go
func (h *Handlers) HandleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("enabling plugin", "plugin", name)

	if err := h.pluginClient.PluginEnable(r.Context(), name); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("disabling plugin", "plugin", name)

	if err := h.pluginClient.PluginDisable(r.Context(), name); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	force := r.URL.Query().Get("force") == "true"
	slog.Info("removing plugin", "plugin", name, "force", force)

	if err := h.pluginClient.PluginRemove(r.Context(), name, force); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run "TestHandle(Enable|Disable|Remove)Plugin" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: add plugin enable, disable, and remove handlers
```

---

### Task 5: Plugin Install, Upgrade, Privileges, and Configure Handlers

**Files:**
- Modify: `internal/api/plugin_handlers.go`
- Modify: `internal/api/plugin_handlers_test.go`

- [ ] **Step 1: Write tests for privileges, install, upgrade, configure**

Add to `plugin_handlers_test.go`:

```go
func TestHandlePluginPrivileges_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginPrivilegesFn: func(_ context.Context, remote string) (types.PluginPrivileges, error) {
			return types.PluginPrivileges{
				{Name: "network", Description: "", Value: []string{"host"}},
			}, nil
		},
	}
	h := newPluginHandlers(pc)

	body := strings.NewReader(`{"remote":"vieux/sshfs"}`)
	req := httptest.NewRequest(http.MethodPost, "/plugins/privileges", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandlePluginPrivileges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePluginPrivileges_MissingRemote(t *testing.T) {
	h := newPluginHandlers(&mockPluginClient{})

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/plugins/privileges", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandlePluginPrivileges(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleInstallPlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginInstallFn: func(_ context.Context, remote string) (*types.Plugin, error) {
			return &types.Plugin{
				ID:   "new-plugin-id",
				Name: remote,
			}, nil
		},
	}
	h := newPluginHandlers(pc)

	body := strings.NewReader(`{"remote":"vieux/sshfs"}`)
	req := httptest.NewRequest(http.MethodPost, "/plugins", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleInstallPlugin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpgradePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginUpgradeFn: func(_ context.Context, _ string, _ string) error { return nil },
	}
	h := newPluginHandlers(pc)

	body := strings.NewReader(`{"remote":"vieux/sshfs:next"}`)
	req := httptest.NewRequest(http.MethodPost, "/plugins/my-plugin/upgrade", body)
	req.SetPathValue("name", "my-plugin")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleUpgradePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleConfigurePlugin_OK(t *testing.T) {
	var gotArgs []string
	pc := &mockPluginClient{
		pluginConfigureFn: func(_ context.Context, _ string, args []string) error {
			gotArgs = args
			return nil
		},
	}
	h := newPluginHandlers(pc)

	body := strings.NewReader(`{"args":["DEBUG=1"]}`)
	req := httptest.NewRequest(http.MethodPatch, "/plugins/my-plugin/settings", body)
	req.SetPathValue("name", "my-plugin")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleConfigurePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(gotArgs) != 1 || gotArgs[0] != "DEBUG=1" {
		t.Errorf("expected args [DEBUG=1], got %v", gotArgs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestHandle(PluginPrivileges|InstallPlugin|UpgradePlugin|ConfigurePlugin)" -v`
Expected: FAIL

- [ ] **Step 3: Implement handlers**

Add to `plugin_handlers.go`:

```go
type pluginRemoteRequest struct {
	Remote string `json:"remote"`
}

type pluginConfigureRequest struct {
	Args []string `json:"args"`
}

func (h *Handlers) HandlePluginPrivileges(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Remote == "" {
		writeProblem(w, r, http.StatusBadRequest, "remote is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	privileges, err := h.pluginClient.PluginPrivileges(ctx, req.Remote)
	if err != nil {
		slog.Error("plugin privileges check failed", "remote", req.Remote, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to check plugin privileges")
		return
	}

	writeJSON(w, privileges)
}

func (h *Handlers) HandleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Remote == "" {
		writeProblem(w, r, http.StatusBadRequest, "remote is required")
		return
	}

	slog.Info("installing plugin", "remote", req.Remote)

	plugin, err := h.pluginClient.PluginInstall(r.Context(), req.Remote)
	if err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	writeJSON(w, NewDetailResponse("/plugins/"+plugin.Name, "Plugin", map[string]any{
		"plugin": plugin,
	}))
}

func (h *Handlers) HandleUpgradePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Remote == "" {
		writeProblem(w, r, http.StatusBadRequest, "remote is required")
		return
	}

	slog.Info("upgrading plugin", "plugin", name, "remote", req.Remote)

	if err := h.pluginClient.PluginUpgrade(r.Context(), name, req.Remote); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleConfigurePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginConfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	slog.Info("configuring plugin", "plugin", name, "args", req.Args)

	if err := h.pluginClient.PluginConfigure(r.Context(), name, req.Args); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run "TestHandle(PluginPrivileges|InstallPlugin|UpgradePlugin|ConfigurePlugin)" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: add plugin install, upgrade, privileges, and configure handlers
```

---

### Task 6: Plugin Route Registration

**Files:**
- Modify: `internal/api/router.go:66`

- [ ] **Step 1: Register all plugin routes**

Replace the existing `GET /plugins` line and add the full block after it (near the other swarm/cluster routes):

```go
// Plugins
mux.HandleFunc("GET /plugins", contentNegotiated(h.HandlePlugins, spa))
mux.HandleFunc("GET /plugins/{name}", contentNegotiated(h.HandlePlugin, spa))
mux.HandleFunc("GET /swarm/plugins", contentNegotiated(h.HandlePlugins, spa))
mux.HandleFunc("POST /plugins/privileges", h.HandlePluginPrivileges)
mux.Handle("POST /plugins", tier3(h.HandleInstallPlugin))
mux.Handle("POST /plugins/{name}/enable", tier2(h.HandleEnablePlugin))
mux.Handle("POST /plugins/{name}/disable", tier2(h.HandleDisablePlugin))
mux.Handle("DELETE /plugins/{name}", tier3(h.HandleRemovePlugin))
mux.Handle("POST /plugins/{name}/upgrade", tier3(h.HandleUpgradePlugin))
mux.Handle("PATCH /plugins/{name}/settings", tier2(h.HandleConfigurePlugin))
```

Note: `POST /plugins/privileges` is untiered (uses `HandleFunc`, not `tier*`) per the spec — it's a read-only registry query needed for the two-step install UI. Users at any operations level can preview what a plugin requires; the actual install is still tier 3.

- [ ] **Step 2: Run all tests**

Run: `go test ./internal/api/ -v`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat: register plugin management routes
```

---

### Task 7: Expand Frontend Plugin Type and API Client

**Files:**
- Modify: `frontend/src/api/types.ts:438-447`
- Modify: `frontend/src/api/client.ts:217`

- [ ] **Step 1: Expand `Plugin` interface**

Replace the existing `Plugin` interface in `types.ts` with the full Docker SDK fields needed for the detail page:

```typescript
export interface PluginPrivilege {
  Name: string;
  Description: string;
  Value: string[];
}

export interface PluginMount {
  Name: string;
  Description: string;
  Settable: string[];
  Source: string;
  Destination: string;
  Type: string;
  Options: string[];
}

export interface PluginDevice {
  Name: string;
  Description: string;
  Settable: string[];
  Path: string;
}

export interface PluginEnv {
  Name: string;
  Description: string;
  Settable: string[];
  Value: string;
}

export interface Plugin {
  Id?: string;
  Name: string;
  Enabled: boolean;
  PluginReference?: string;
  Settings: {
    Mounts: PluginMount[];
    Env: string[];
    Args: string[];
    Devices: PluginDevice[];
  };
  Config: {
    DockerVersion?: string;
    Description: string;
    Documentation?: string;
    Entrypoint: string[];
    WorkDir: string;
    User?: { UID: number; GID: number };
    Interface: {
      Types: Array<{ Prefix: string; Capability: string; Description: string }>;
      Socket: string;
    };
    Network: { Type: string };
    Linux: {
      Capabilities: string[];
      AllowAllDevices: boolean;
      Devices: PluginDevice[];
    };
    Mounts: PluginMount[];
    Env: PluginEnv[];
    Args: { Name: string; Description: string; Settable: string[]; Value: string[] };
  };
}
```

- [ ] **Step 2: Add plugin API methods to `client.ts`**

Add to the `api` object:

```typescript
plugin: (name: string, signal?: AbortSignal) =>
  fetchJSON<{ plugin: Plugin }>(`/plugins/${encodeURIComponent(name)}`, signal).then(
    (r) => r.plugin,
  ),
pluginPrivileges: (remote: string) =>
  mutationFetch<PluginPrivilege[]>("/plugins/privileges", "POST", { remote }, "application/json"),
installPlugin: (remote: string) =>
  mutationFetch<{ plugin: Plugin }>("/plugins", "POST", { remote }, "application/json"),
enablePlugin: (name: string) =>
  post<void>(`/plugins/${encodeURIComponent(name)}/enable`),
disablePlugin: (name: string) =>
  post<void>(`/plugins/${encodeURIComponent(name)}/disable`),
removePlugin: (name: string, force?: boolean) =>
  del(`/plugins/${encodeURIComponent(name)}${force ? "?force=true" : ""}`),
upgradePlugin: (name: string, remote: string) =>
  mutationFetch<void>(
    `/plugins/${encodeURIComponent(name)}/upgrade`,
    "POST",
    { remote },
    "application/json",
  ),
configurePlugin: (name: string, args: string[]) =>
  patch<void>(
    `/plugins/${encodeURIComponent(name)}/settings`,
    { args },
    "application/json",
  ),
```

Also add the `PluginPrivilege` type import/export to `types.ts`.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat: expand Plugin type and add plugin API client methods
```

---

### Task 8: Plugin List Page

**Files:**
- Create: `frontend/src/pages/PluginList.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Create `PluginList.tsx`**

This is a simple table matching the SwarmPage plugin section, with an "Install Plugin" header action. No DataTable — just a plain HTML table.

```tsx
import { api } from "../api/client";
import type { Plugin } from "../api/types";
import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { Button } from "../components/ui/button";
import { useOperationsLevel } from "../hooks/useOperationsLevel";
import { Plus } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import InstallPluginDialog from "../components/InstallPluginDialog";

export default function PluginList() {
  const [plugins, setPlugins] = useState<Plugin[] | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [installOpen, setInstallOpen] = useState(false);
  const opsLevel = useOperationsLevel();

  const fetchPlugins = useCallback(() => {
    api
      .plugins()
      .then(setPlugins)
      .catch(setError);
  }, []);

  useEffect(() => {
    fetchPlugins();
  }, [fetchPlugins]);

  if (error) {
    return (
      <FetchError
        message={error.message || "Failed to load plugins"}
        onRetry={fetchPlugins}
      />
    );
  }

  if (!plugins) {
    return <LoadingDetail />;
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Plugins"
        breadcrumbs={[
          { label: "Swarm", to: "/swarm" },
          { label: "Plugins" },
        ]}
        actions={
          opsLevel >= 3 ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setInstallOpen(true)}
            >
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              Install Plugin
            </Button>
          ) : undefined
        }
      />

      {plugins.length === 0 ? (
        <p className="text-sm text-muted-foreground">No plugins installed.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-b text-left text-xs font-medium tracking-wider text-muted-foreground uppercase">
                <th className="p-3">Name</th>
                <th className="p-3">Type</th>
                <th className="p-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {plugins.map(({ Config: { Interface }, Enabled, Id, Name }) => (
                <tr
                  key={Id ?? Name}
                  className="border-b last:border-b-0"
                >
                  <td className="p-3 font-mono text-xs">
                    <Link
                      to={`/plugins/${encodeURIComponent(Name)}`}
                      className="text-primary hover:underline"
                    >
                      {Name}
                    </Link>
                  </td>
                  <td className="p-3 text-sm text-muted-foreground">
                    {Interface.Types.map(({ Capability }) => Capability).join(", ") || "—"}
                  </td>
                  <td className="p-3">
                    <span
                      data-enabled={Enabled || undefined}
                      className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground data-enabled:bg-green-500/10 data-enabled:text-green-500"
                    >
                      {Enabled ? "Enabled" : "Disabled"}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <InstallPluginDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        onInstalled={fetchPlugins}
      />
    </div>
  );
}
```

- [ ] **Step 2: Add routes to `App.tsx`**

Add lazy import:
```tsx
const PluginList = lazy(() => import("./pages/PluginList"));
const PluginDetail = lazy(() => import("./pages/PluginDetail"));
```

Add routes (before the `*` catch-all):
```tsx
<Route path="/plugins" element={<PluginList />} />
<Route path="/plugins/:name" element={<PluginDetail />} />
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: FAIL (PluginDetail and InstallPluginDialog don't exist yet — create stubs)

Create minimal stubs so the app compiles:

`frontend/src/pages/PluginDetail.tsx`:
```tsx
export default function PluginDetail() {
  return <div>Plugin detail placeholder</div>;
}
```

`frontend/src/components/InstallPluginDialog.tsx`:
```tsx
export default function InstallPluginDialog(_props: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onInstalled: () => void;
}) {
  return null;
}
```

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat: add plugin list page and route stubs
```

---

### Task 9: Plugin Detail Page

**Files:**
- Modify: `frontend/src/pages/PluginDetail.tsx`

- [ ] **Step 1: Implement full plugin detail page**

Replace the stub with the full implementation. The page uses `useState` + `useEffect` (no `useDetailResource` since plugins aren't cached/SSE-streamed). Sections: header with actions, overview metadata, settings editor, privileges, and configuration.

Key components:
- `PageHeader` with breadcrumbs to Swarm → Plugins
- Enable/disable toggle button (tier 2)
- Remove button with confirmation dialog (tier 3)
- Upgrade button opening the upgrade dialog (tier 3)
- `MetadataGrid` for overview fields (ID, description, reference, docker version)
- Settings args displayed in a simple list with inline editing
- Privileges shown as a read-only table
- Config details (entrypoint, workdir, mounts, env, devices, capabilities) in collapsible sections

Use `useAsyncAction` for the enable/disable/remove actions. Re-fetch plugin data after any mutation succeeds.

The page should handle URL-encoded plugin names via `decodeURIComponent(useParams().name)`.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat: add plugin detail page with actions and settings
```

---

### Task 10: Install Plugin Dialog

**Files:**
- Modify: `frontend/src/components/InstallPluginDialog.tsx`

- [ ] **Step 1: Implement two-step install dialog**

Replace the stub. Two-step flow using shadcn `Dialog`:

**Step 1 (input):** Text input for remote reference, "Check Privileges" button. Calls `api.pluginPrivileges(remote)`. On success, transitions to step 2.

**Step 2 (review):** Displays the privilege list in a table (Name, Description, Values). "Install" button calls `api.installPlugin(remote)`. On success, calls `onInstalled()` and closes.

Use `useAsyncAction` for both the privileges check and the install action. Show loading spinners and error states.

The same component is reusable for upgrades — accept an optional `mode: "install" | "upgrade"` and `pluginName` prop. When upgrading, step 2 calls `api.upgradePlugin(name, remote)` instead.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat: add install/upgrade plugin dialog with privilege review
```

---

### Task 11: SwarmPage Plugin Section Updates

**Files:**
- Modify: `frontend/src/pages/SwarmPage.tsx:611-647`

- [ ] **Step 1: Update plugin section**

Changes to the existing plugin section:
1. Make plugin names clickable links to `/plugins/<name>`
2. Add "Install Plugin" button as a section action (same as PluginList)
3. Add "View All →" link to `/plugins` at the bottom of the table

Update the `CollapsibleSection` to include the install button:
```tsx
<CollapsibleSection
  title="Plugins"
  actions={
    opsLevel >= 3 ? (
      <Button variant="outline" size="sm" onClick={() => setInstallOpen(true)}>
        <Plus className="mr-1.5 h-3.5 w-3.5" />
        Install Plugin
      </Button>
    ) : undefined
  }
>
```

Add the install dialog state and component (same pattern as PluginList).

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat: add plugin links and install action to SwarmPage
```

---

### Task 12: Lint, Format, and Full Verification

**Files:** All modified files

- [ ] **Step 1: Run backend linting and formatting**

Run: `make lint && make fmt-check`
Expected: PASS (fix any issues)

- [ ] **Step 2: Run backend tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 3: Run frontend linting and formatting**

Run: `cd frontend && npm run lint && npm run fmt:check`
Expected: PASS (fix any issues)

- [ ] **Step 4: Run frontend type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 5: Run frontend tests**

Run: `cd frontend && npx vitest run`
Expected: PASS

- [ ] **Step 6: Build the full project**

Run: `make build`
Expected: PASS

- [ ] **Step 7: Commit any lint/format fixes**

```
chore: fix lint and formatting
```

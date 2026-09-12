# Node Role Change & Node Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add node role change (promote/demote) and node removal write operations to the node detail page.

**Architecture:** Backend adds two new Docker client methods, three new HTTP handlers (GET/PUT role, DELETE node), and extends the DockerWriteClient interface. Frontend adds a RoleEditor InfoCard component (popover with radio cards + quorum warnings) and a NodeActions component (remove button with type-to-confirm dialog).

**Tech Stack:** Go (Docker Engine API), React 19, TypeScript, shadcn/ui components (RadioCard, AlertDialog, Popover, Button)

**Spec:** `docs/superpowers/specs/2026-03-21-node-role-and-removal-design.md`

---

### Task 1: Docker Client — UpdateNodeRole & RemoveNode

**Files:**
- Modify: `internal/docker/client.go` (after `UpdateNodeAvailability` at line 422)

- [ ] **Step 1: Add `UpdateNodeRole` method**

```go
func (c *Client) UpdateNodeRole(
	ctx context.Context,
	id string,
	role swarm.NodeRole,
) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Node{}, err
	}
	node.Spec.Role = role
	err = c.docker.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
	if err != nil {
		return swarm.Node{}, err
	}
	return c.InspectNode(ctx, id)
}
```

- [ ] **Step 2: Add `RemoveNode` method**

```go
func (c *Client) RemoveNode(ctx context.Context, id string) error {
	return c.docker.NodeRemove(ctx, id, swarm.NodeRemoveOptions{Force: false})
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/docker/`
Expected: Success (no callers yet, just verifying syntax)

- [ ] **Step 4: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add UpdateNodeRole and RemoveNode to Docker client"
```

---

### Task 2: DockerWriteClient Interface & Mock

**Files:**
- Modify: `internal/api/handlers.go` (DockerWriteClient interface, around line 75)
- Modify: `internal/api/write_handlers_test.go` (mockWriteClient struct + methods)

- [ ] **Step 1: Add methods to DockerWriteClient interface**

In `internal/api/handlers.go`, add to the `DockerWriteClient` interface (after the `UpdateNodeLabels` line):

```go
UpdateNodeRole(ctx context.Context, id string, role swarm.NodeRole) (swarm.Node, error)
RemoveNode(ctx context.Context, id string) error
```

- [ ] **Step 2: Add fields to mockWriteClient struct**

In `internal/api/write_handlers_test.go`, add to the `mockWriteClient` struct (after `updateNodeLabelsFn`):

```go
updateNodeRoleFn func(ctx context.Context, id string, role swarm.NodeRole) (swarm.Node, error)
removeNodeFn     func(ctx context.Context, id string) error
```

- [ ] **Step 3: Add stub methods to mockWriteClient**

In `internal/api/write_handlers_test.go`, after the existing `UpdateNodeLabels` method:

```go
func (m *mockWriteClient) UpdateNodeRole(
	ctx context.Context,
	id string,
	role swarm.NodeRole,
) (swarm.Node, error) {
	if m.updateNodeRoleFn != nil {
		return m.updateNodeRoleFn(ctx, id, role)
	}
	return swarm.Node{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) RemoveNode(ctx context.Context, id string) error {
	if m.removeNodeFn != nil {
		return m.removeNodeFn(ctx, id)
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
git commit -m "feat: extend DockerWriteClient interface with UpdateNodeRole and RemoveNode"
```

---

### Task 3: Backend Handlers — HandleUpdateNodeRole, HandleRemoveNode, HandleGetNodeRole

**Files:**
- Modify: `internal/api/write_handlers.go` (add handlers after `HandleRemoveService`)
- Test: `internal/api/write_handlers_test.go`

- [ ] **Step 1: Write tests for HandleUpdateNodeRole**

In `internal/api/write_handlers_test.go`, add:

```go
func TestHandleUpdateNodeRole_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}})

	wc := &mockWriteClient{
		updateNodeRoleFn: func(_ context.Context, id string, role swarm.NodeRole) (swarm.Node, error) {
			return swarm.Node{ID: id, Spec: swarm.NodeSpec{Role: role}}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	body := `{"role":"manager"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/role", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Node" {
		t.Errorf("@type=%v, want Node", resp["@type"])
	}
}

func TestHandleUpdateNodeRole_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	body := `{"role":"manager"}`
	req := httptest.NewRequest("PUT", "/nodes/missing/role", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateNodeRole_InvalidRole(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	body := `{"role":"invalid"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/role", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateNodeRole_Conflict(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		updateNodeRoleFn: func(_ context.Context, _ string, _ swarm.NodeRole) (swarm.Node, error) {
			return swarm.Node{}, errdefs.Conflict(fmt.Errorf("conflict"))
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	body := `{"role":"manager"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/role", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeRole(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}
}
```

- [ ] **Step 2: Write tests for HandleRemoveNode**

```go
func TestHandleRemoveNode_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		removeNodeFn: func(_ context.Context, _ string) error {
			return nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/nodes/node1", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleRemoveNode(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveNode_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/nodes/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveNode(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveNode_DockerError(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		removeNodeFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("node is not down")
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("DELETE", "/nodes/node1", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleRemoveNode(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}
}
```

- [ ] **Step 3: Write tests for HandleGetNodeRole**

```go
func TestHandleGetNodeRole_Manager(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:   "node1",
		Spec: swarm.NodeSpec{Role: swarm.NodeRoleManager},
		ManagerStatus: &swarm.ManagerStatus{Leader: true},
	})
	c.SetNode(swarm.Node{
		ID:   "node2",
		Spec: swarm.NodeSpec{Role: swarm.NodeRoleManager},
	})
	c.SetNode(swarm.Node{
		ID:   "node3",
		Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker},
	})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/nodes/node1/role", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleGetNodeRole(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["role"] != "manager" {
		t.Errorf("role=%v, want manager", resp["role"])
	}
	if resp["isLeader"] != true {
		t.Errorf("isLeader=%v, want true", resp["isLeader"])
	}
	if resp["managerCount"] != float64(2) {
		t.Errorf("managerCount=%v, want 2", resp["managerCount"])
	}
}

func TestHandleGetNodeRole_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/nodes/missing/role", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetNodeRole(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(UpdateNodeRole|RemoveNode|GetNodeRole)" -v`
Expected: FAIL — methods don't exist yet

- [ ] **Step 5: Implement HandleUpdateNodeRole**

In `internal/api/write_handlers.go`, add after `HandleRemoveService`:

```go
type updateRoleRequest struct {
	Role string `json:"role"`
}

func (h *Handlers) HandleUpdateNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	var role swarm.NodeRole
	switch req.Role {
	case "worker":
		role = swarm.NodeRoleWorker
	case "manager":
		role = swarm.NodeRoleManager
	default:
		writeProblem(w, r, http.StatusBadRequest, "role must be one of: worker, manager")
		return
	}

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	slog.Info("updating node role", "node", id, "role", req.Role)

	updated, err := h.writeClient.UpdateNodeRole(r.Context(), id, role)
	if err != nil {
		writeDockerError(w, r, err, "node")
		return
	}

	writeJSON(w, NewDetailResponse("/nodes/"+id, "Node", map[string]any{
		"node": updated,
	}))
}
```

- [ ] **Step 6: Implement HandleRemoveNode**

```go
func (h *Handlers) HandleRemoveNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	slog.Info("removing node", "node", id)

	err := h.writeClient.RemoveNode(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "node")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 7: Implement HandleGetNodeRole**

```go
func (h *Handlers) HandleGetNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	managerCount := 0
	for _, n := range h.cache.ListNodes() {
		if n.Spec.Role == swarm.NodeRoleManager {
			managerCount++
		}
	}

	writeJSON(w, map[string]any{
		"role":         string(node.Spec.Role),
		"isLeader":     node.ManagerStatus != nil && node.ManagerStatus.Leader,
		"managerCount": managerCount,
	})
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(UpdateNodeRole|RemoveNode|GetNodeRole)" -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go
git commit -m "feat: add handlers for node role change, removal, and role info"
```

---

### Task 4: Router Registration

**Files:**
- Modify: `internal/api/router.go` (node routes section, around line 104)

- [ ] **Step 1: Add route registrations**

In `internal/api/router.go`, after the `PATCH /nodes/{id}/labels` line, add:

```go
mux.HandleFunc("GET /nodes/{id}/role", contentNegotiated(h.HandleGetNodeRole, spa))
mux.Handle("PUT /nodes/{id}/role", tier3(h.HandleUpdateNodeRole))
mux.Handle("DELETE /nodes/{id}", tier3(h.HandleRemoveNode))
```

- [ ] **Step 2: Run full backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -v -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register node role and removal routes"
```

---

### Task 5: Frontend API Client

**Files:**
- Modify: `frontend/src/api/client.ts` (after `removeService` around line 301)

- [ ] **Step 1: Add API methods**

After the `removeService` line, add:

```ts
updateNodeRole: (id: string, role: "worker" | "manager") =>
  put<{ node: Node }>(`/nodes/${id}/role`, { role }),
removeNode: (id: string) => del(`/nodes/${id}`),
```

In the tier 2 sub-resource GETs section (after `nodeLabels`), add:

```ts
nodeRole: (id: string, signal?: AbortSignal) =>
  fetchJSON<{ role: string; isLeader: boolean; managerCount: number }>(
    `/nodes/${id}/role`,
    signal,
  ),
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: add nodeRole, updateNodeRole, and removeNode API methods"
```

---

### Task 6: RoleEditor Component

**Files:**
- Create: `frontend/src/components/node-detail/RoleEditor.tsx`
- Modify: `frontend/src/components/node-detail/index.ts`

- [ ] **Step 1: Create RoleEditor component**

Create `frontend/src/components/node-detail/RoleEditor.tsx`:

```tsx
import { api } from "@/api/client";
import InfoCard from "@/components/InfoCard";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { RadioCard } from "@/components/ui/radio-card";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { Pencil } from "lucide-react";
import { useState } from "react";

interface RoleEditorProps {
  nodeId: string;
  currentRole: string;
  isLeader: boolean;
  managerCount: number;
}

const roles = [
  {
    value: "worker",
    title: "Worker",
    description: "Runs tasks. Cannot participate in Raft consensus or manage the cluster.",
  },
  {
    value: "manager",
    title: "Manager",
    description:
      "Participates in Raft consensus. Can manage nodes, services, and other cluster resources.",
  },
] as const;

export function RoleEditor({ nodeId, currentRole, isLeader, managerCount }: RoleEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.impactful;
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(currentRole);
  const action = useAsyncAction();

  function handleOpenChange(next: boolean) {
    if (next) {
      setValue(currentRole);
    }

    setOpen(next);
  }

  async function save() {
    if (value === currentRole) {
      setOpen(false);
      return;
    }

    await action.execute(async () => {
      await api.updateNodeRole(nodeId, value as "worker" | "manager");
      setOpen(false);
    }, "Failed to update role");
  }

  const isDemoting = currentRole === "manager" && value === "worker";
  const quorum = Math.floor(managerCount / 2) + 1;
  const remainingManagers = managerCount - 1;

  return (
    <InfoCard
      label="Role"
      value={
        <>
          <span className="capitalize">{currentRole}</span>
          {currentRole === "manager" && isLeader && (
            <span className="rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
              Leader
            </span>
          )}
          {canEdit && (
            <Popover
              open={open}
              onOpenChange={handleOpenChange}
              modal
            >
              <PopoverTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    title="Edit role"
                  >
                    <Pencil className="size-3.5" />
                  </Button>
                }
              />

              <PopoverContent className="w-80">
            <div className="flex flex-col gap-3">
              <p className="text-sm font-medium">Change Role</p>

              {roles.map((role) => (
                <RadioCard
                  key={role.value}
                  selected={value === role.value}
                  onClick={() => setValue(role.value)}
                  disabled={role.value === currentRole}
                  title={
                    role.value === currentRole ? `${role.title} (current)` : role.title
                  }
                  description={role.description}
                />
              ))}

              {isDemoting && (
                <div className="rounded-md border border-yellow-500/25 bg-yellow-500/5 px-3 py-2 text-xs leading-relaxed text-yellow-600 dark:text-yellow-500">
                  {isLeader && (
                    <p className="mb-2 font-medium">
                      This node is the Raft leader. Demoting it will trigger a leader
                      re-election.
                    </p>
                  )}
                  <p>
                    This cluster has {managerCount} managers. Demoting this node leaves{" "}
                    {remainingManagers} managers (quorum requires {quorum}).
                    {remainingManagers === quorum &&
                      " Losing one more manager will make the cluster unrecoverable."}
                  </p>
                </div>
              )}

              {action.error && (
                <p className="text-xs text-red-600 dark:text-red-400">{action.error}</p>
              )}

              <div className="flex justify-end gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  size="sm"
                  disabled={value === currentRole || action.loading}
                  onClick={() => void save()}
                >
                  {action.loading ? "Applying…" : "Apply"}
                </Button>
              </div>
            </div>
          </PopoverContent>
            </Popover>
          )}
        </>
      }
    />
  );
}
```

- [ ] **Step 2: Add barrel export**

In `frontend/src/components/node-detail/index.ts`, add:

```ts
export { RoleEditor } from "./RoleEditor";
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/node-detail/RoleEditor.tsx frontend/src/components/node-detail/index.ts
git commit -m "feat: add RoleEditor component with radio cards and quorum warnings"
```

---

### Task 7: NodeActions Component

**Files:**
- Create: `frontend/src/components/node-detail/NodeActions.tsx`
- Modify: `frontend/src/components/node-detail/index.ts`

- [ ] **Step 1: Create NodeActions component**

Create `frontend/src/components/node-detail/NodeActions.tsx`:

```tsx
import { api } from "@/api/client";
import type { Node } from "@/api/types";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

export function NodeActions({ node, nodeId }: { node: Node; nodeId: string }) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canImpact = !levelLoading && level >= opsLevel.impactful;
  const navigate = useNavigate();
  const remove = useAsyncAction();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");

  if (!canImpact) {
    return null;
  }

  const hostname = node.Description?.Hostname || node.ID;
  const isDown = node.Status?.State === "down";
  const canRemove = isDown && confirmText === hostname;

  function handleOpenChange(next: boolean) {
    setDialogOpen(next);

    if (!next) {
      setConfirmText("");
    }
  }

  const trigger = (
    <Button
      variant="outline"
      size="sm"
      disabled={!isDown || remove.loading}
      className={isDown ? "border-red-500/50 text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/20" : ""}
      onClick={() => setDialogOpen(true)}
    >
      {remove.loading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
      Remove
    </Button>
  );

  return (
    <div className="flex flex-col items-start gap-1">
      <div className="flex flex-wrap items-center gap-2">
        {!isDown ? (
          <Tooltip>
            <TooltipTrigger asChild>{trigger}</TooltipTrigger>
            <TooltipContent>Node must be in down state to remove</TooltipContent>
          </Tooltip>
        ) : (
          trigger
        )}
      </div>

      {remove.error && (
        <p className="text-xs text-red-600 dark:text-red-400">{remove.error}</p>
      )}

      <AlertDialog
        open={dialogOpen}
        onOpenChange={handleOpenChange}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove node?</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently remove{" "}
              <strong className="text-foreground">{hostname}</strong> from the swarm. The
              node will no longer appear in the cluster and cannot rejoin without being
              re-initialized. Any node-specific labels and configuration will be lost.
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="flex flex-col gap-1.5">
            <label className="text-sm text-muted-foreground">
              Type <strong className="text-foreground">{hostname}</strong> to confirm
            </label>
            <Input
              value={confirmText}
              onChange={(event) => setConfirmText(event.target.value)}
              placeholder={hostname}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
            />
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={!canRemove || remove.loading}
              onClick={() =>
                void remove.execute(async () => {
                  await api.removeNode(nodeId);
                  navigate("/nodes", { replace: true });
                }, "Failed to remove node")
              }
            >
              {remove.loading ? "Removing…" : "Remove"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
```

- [ ] **Step 2: Add barrel export**

In `frontend/src/components/node-detail/index.ts`, add:

```ts
export { NodeActions } from "./NodeActions";
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/node-detail/NodeActions.tsx frontend/src/components/node-detail/index.ts
git commit -m "feat: add NodeActions component with type-to-confirm remove dialog"
```

---

### Task 8: Integrate into NodeDetail Page

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx`

- [ ] **Step 1: Add imports and state**

In `frontend/src/pages/NodeDetail.tsx`, update the node-detail import:

```ts
import { AvailabilityEditor, EngineCard, NodeActions, OsCard, RoleEditor, StatusCard } from "../components/node-detail";
```

Add state for role info (after the `nodeLabels` state):

```ts
const [nodeRole, setNodeRole] = useState<{
  role: string;
  isLeader: boolean;
  managerCount: number;
} | null>(null);
```

- [ ] **Step 2: Fetch role info in fetchData**

In the `fetchData` callback, add alongside the other parallel fetches:

```ts
api
  .nodeRole(id, signal)
  .then(setNodeRole)
  .catch(() => {});
```

- [ ] **Step 3: Add NodeActions after PageHeader**

After the `</PageHeader>` closing tag and before `<MetadataGrid>`, add:

```tsx
<NodeActions
  node={node}
  nodeId={node.ID}
/>
```

- [ ] **Step 4: Replace static Role InfoCard with RoleEditor**

Replace the existing `<InfoCard label="Role" ...>` block (the one with `node.Spec.Role` and the Leader badge) with:

```tsx
{nodeRole ? (
  <RoleEditor
    nodeId={node.ID}
    currentRole={nodeRole.role}
    isLeader={nodeRole.isLeader}
    managerCount={nodeRole.managerCount}
  />
) : (
  <InfoCard
    label="Role"
    value={<span className="capitalize">{node.Spec.Role}</span>}
  />
)}
```

This shows a static InfoCard while the role endpoint loads, then swaps to the interactive editor.

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 6: Verify lint passes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: Success

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/NodeDetail.tsx
git commit -m "feat: integrate RoleEditor and NodeActions into node detail page"
```

---

### Task 9: Full Test Suite Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`
Expected: All PASS

- [ ] **Step 2: Run frontend type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 3: Run frontend lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: Success

- [ ] **Step 4: Run frontend format check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run fmt:check`
Expected: Success (or run `npm run fmt` to fix)

- [ ] **Step 5: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass
